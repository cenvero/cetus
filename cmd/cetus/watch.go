package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/cenvero/cetus/internal/assets"
	"github.com/cenvero/cetus/internal/browser"
	"github.com/cenvero/cetus/internal/compose"
	"github.com/cenvero/cetus/internal/encoder"
	"github.com/cenvero/cetus/internal/version"
	"github.com/fsnotify/fsnotify"
	"github.com/spf13/cobra"
)

func newWatchCommand() *cobra.Command {
	var output string
	var fps int
	var width int
	var height int
	var format string
	var concurrency int
	var timeoutSeconds int
	var audioPath string
	var audioVolume float64
	var audioLoop bool
	var audioStartSeconds float64
	var audioFadeInSeconds float64
	var audioFadeOutSeconds float64
	var framesDir string
	var noGPU bool
	var scale string
	var quality int
	var subtitlesPath string
	var progressFormat string

	cmd := &cobra.Command{
		Use:   "watch cetus.html -o preview.mp4",
		Short: "Re-render a composition whenever the source files change",
		Long: `Watch an HTML composition and its assets for changes. Performs an initial render immediately, then re-renders automatically whenever any watched file is modified.

Press Ctrl+C to stop watching.

Examples:
  cetus watch cetus.html -o preview.mp4
  cetus watch cetus.html -o preview.mp4 --quality 28
  cetus watch cetus.html -o preview.mp4 --frames-dir .watch-frames --concurrency 2`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			input := args[0]
			if output == "" {
				return fmt.Errorf("output is required")
			}
			if concurrency <= 0 {
				return fmt.Errorf("concurrency must be positive")
			}
			if timeoutSeconds < 0 {
				return fmt.Errorf("timeout must be zero or positive")
			}
			if err := validateAudioFlagValues(audioVolume, audioStartSeconds, audioFadeInSeconds, audioFadeOutSeconds); err != nil {
				return err
			}
			if _, err := os.Stat(input); err != nil {
				return fmt.Errorf("stat input composition: %w", err)
			}
			audioPath = strings.TrimSpace(audioPath)
			if audioPath == "" && audioControlFlagsChanged(cmd) {
				return fmt.Errorf("audio controls require --audio")
			}

			absInput, err := filepath.Abs(input)
			if err != nil {
				return fmt.Errorf("resolve input path: %w", err)
			}

			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			fmt.Fprintf(cmd.OutOrStdout(), "Watching %s for changes (Ctrl+C to stop)...\n", absInput)

			watcher, err := fsnotify.NewWatcher()
			if err != nil {
				return fmt.Errorf("create watcher: %w", err)
			}
			defer watcher.Close()

			watchDir := filepath.Dir(absInput)
			if err := watcher.Add(watchDir); err != nil {
				return fmt.Errorf("watch %s: %w", watchDir, err)
			}

			doRender := func() {
				fmt.Fprintf(cmd.OutOrStdout(), "\n[%s] Rendering → %s\n", time.Now().Format("15:04:05"), output)
				start := time.Now()

				renderCtx, cancel := renderContext(ctx, timeoutSeconds)
				defer cancel()

				chromePath, ffmpegPath, err := assets.EnsureAssetsWithProgress(version.Version, func(event assets.ProgressEvent) {})
				if err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "assets: %v\n", err)
					return
				}

				scaleFilter, err := encoder.ParseScale(scale)
				if err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "scale: %v\n", err)
					return
				}

				composition, err := compose.Parse(input)
				if err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "parse: %v\n", err)
					return
				}

				fpsOverride := 0
				if cmd.Flags().Changed("fps") {
					fpsOverride = fps
				}
				if err := composition.ApplyOverrides(fpsOverride, width, height); err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "overrides: %v\n", err)
					return
				}

				renderDuration := float64(composition.TotalFrames) / float64(composition.FPS)
				if audioPath != "" && audioStartSeconds >= renderDuration {
					fmt.Fprintf(cmd.ErrOrStderr(), "audio start %.3fs must be before render duration %.3fs\n", audioStartSeconds, renderDuration)
					return
				}

				resolvedFormat, err := encoder.ResolveFormat(output, format)
				if err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "format: %v\n", err)
					return
				}

				progress := newProgressLogger(cmd.ErrOrStderr(), progressFormat)

				encoderOpts := encoder.Options{
					AudioPath:           audioPath,
					AudioVolume:         audioVolume,
					AudioVolumeSet:      cmd.Flags().Changed("audio-volume"),
					AudioLoop:           audioLoop,
					AudioStartSeconds:   audioStartSeconds,
					AudioFadeInSeconds:  audioFadeInSeconds,
					AudioFadeOutSeconds: audioFadeOutSeconds,
					DurationSeconds:     renderDuration,
					Quality:             quality,
					Scale:               scaleFilter,
					FrameCodec:          "png",
					SubtitlesPath:       strings.TrimSpace(subtitlesPath),
				}
				browserOpts := browser.Options{
					ChromePath: chromePath,
					NoGPU:      noGPU,
				}

				if framesDir != "" {
					// Always fresh render — never resume in watch mode
					progress.Stage("Rendering frames → %s (%d worker(s))...", framesDir, concurrency)
					progress.ResetFrames()
					if err := browser.CaptureFramesToCache(renderCtx, composition, browser.WorkerOptions{
						HTMLPath:       input,
						BrowserOptions: browserOpts,
						CaptureOptions: browser.CaptureOptions{
							FramesDir: framesDir,
							Resume:    false,
						},
						Workers: concurrency,
					}, progress.Frames); err != nil {
						fmt.Fprintf(cmd.ErrOrStderr(), "capture: %v\n", err)
						return
					}
					enc, err := encoder.New(ffmpegPath, output, composition.FPS, resolvedFormat, encoderOpts)
					if err != nil {
						fmt.Fprintf(cmd.ErrOrStderr(), "encoder: %v\n", err)
						return
					}
					progress.Stage("Encoding frames → %s...", output)
					progress.ResetFrames()
					if err := browser.EncodeCachedFrames(composition, enc, framesDir, progress.Frames); err != nil {
						_ = enc.Close()
						fmt.Fprintf(cmd.ErrOrStderr(), "encode: %v\n", err)
						return
					}
					progress.Stage("Finalizing %s...", output)
					if err := enc.Close(); err != nil {
						fmt.Fprintf(cmd.ErrOrStderr(), "finalize: %v\n", err)
						return
					}
					_ = os.RemoveAll(framesDir)
				} else {
					progress.Stage("Starting %s encoder...", resolvedFormat)
					enc, err := encoder.New(ffmpegPath, output, composition.FPS, resolvedFormat, encoderOpts)
					if err != nil {
						fmt.Fprintf(cmd.ErrOrStderr(), "encoder: %v\n", err)
						return
					}
					b, err := browser.New(renderCtx, input, composition, browserOpts)
					if err != nil {
						_ = enc.Close()
						fmt.Fprintf(cmd.ErrOrStderr(), "browser: %v\n", err)
						return
					}
					progress.ResetFrames()
					if err := b.CaptureWithOptions(renderCtx, composition, enc, progress.Frames, browser.CaptureOptions{}); err != nil {
						b.Close()
						_ = enc.Close()
						fmt.Fprintf(cmd.ErrOrStderr(), "capture: %v\n", err)
						return
					}
					b.Close()
					progress.Stage("Finalizing %s...", output)
					if err := enc.Close(); err != nil {
						fmt.Fprintf(cmd.ErrOrStderr(), "finalize: %v\n", err)
						return
					}
				}

				fmt.Fprintf(cmd.OutOrStdout(), "Done in %s → %s\nWatching for changes...\n",
					time.Since(start).Round(time.Millisecond), output)
			}

			// Initial render
			doRender()

			// Watch loop with 300ms debounce
			timer := time.NewTimer(time.Hour)
			if !timer.Stop() {
				<-timer.C
			}
			pending := false

			for {
				select {
				case <-ctx.Done():
					return nil
				case event, ok := <-watcher.Events:
					if !ok {
						return nil
					}
					if event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename|fsnotify.Remove) == 0 {
						continue
					}
					// Ignore the output file being written
					if abs, err := filepath.Abs(event.Name); err == nil {
						if absOut, err := filepath.Abs(output); err == nil && abs == absOut {
							continue
						}
					}
					if !pending {
						fmt.Fprintf(cmd.OutOrStdout(), "\nChanged: %s\n", filepath.Base(event.Name))
					}
					pending = true
					timer.Reset(300 * time.Millisecond)
				case err, ok := <-watcher.Errors:
					if !ok {
						return nil
					}
					fmt.Fprintf(cmd.ErrOrStderr(), "watcher: %v\n", err)
				case <-timer.C:
					if pending {
						pending = false
						doRender()
					}
				}
			}
		},
	}

	cmd.Flags().StringVarP(&output, "output", "o", "", "output file path (overwritten on each re-render)")
	cmd.Flags().IntVar(&fps, "fps", 0, "override composition FPS")
	cmd.Flags().IntVar(&width, "width", 0, "override composition width")
	cmd.Flags().IntVar(&height, "height", 0, "override composition height")
	cmd.Flags().StringVar(&format, "format", "", "output format: mp4 or webm")
	cmd.Flags().IntVar(&concurrency, "concurrency", 1, "parallel frame capture workers (requires --frames-dir)")
	cmd.Flags().IntVar(&timeoutSeconds, "timeout", 0, "per-render time limit in seconds; 0 disables the deadline")
	cmd.Flags().StringVar(&audioPath, "audio", "", "audio file to mux into the output")
	cmd.Flags().Float64Var(&audioVolume, "audio-volume", 1, "audio volume from 0.0 to 1.0")
	cmd.Flags().BoolVar(&audioLoop, "audio-loop", false, "loop audio until the render duration is reached")
	cmd.Flags().Float64Var(&audioStartSeconds, "audio-start", 0, "audio start offset in seconds")
	cmd.Flags().Float64Var(&audioFadeInSeconds, "audio-fade-in", 0, "audio fade-in duration in seconds")
	cmd.Flags().Float64Var(&audioFadeOutSeconds, "audio-fade-out", 0, "audio fade-out duration in seconds")
	cmd.Flags().StringVar(&framesDir, "frames-dir", "", "directory for PNG frames; speeds up re-renders by reusing the Chrome render path")
	cmd.Flags().BoolVar(&noGPU, "no-gpu", false, "disable GPU acceleration")
	cmd.Flags().StringVar(&scale, "scale", "", "scale output: 480p, 720p, 1080p, 4k, or WxH")
	cmd.Flags().IntVar(&quality, "quality", 0, "encoder CRF quality (0 = codec default)")
	cmd.Flags().StringVar(&subtitlesPath, "subtitles", "", "subtitle file to burn into the video (SRT or ASS)")
	cmd.Flags().StringVar(&progressFormat, "progress-format", "text", "progress output format: text or json")
	_ = cmd.MarkFlagRequired("output")

	return cmd
}
