package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/cenvero/cetus/internal/assets"
	"github.com/cenvero/cetus/internal/browser"
	"github.com/cenvero/cetus/internal/compose"
	"github.com/cenvero/cetus/internal/encoder"
	"github.com/cenvero/cetus/internal/preview"
	"github.com/cenvero/cetus/internal/updater"
	"github.com/cenvero/cetus/internal/version"
	"github.com/spf13/cobra"
)

func main() {
	if err := newRootCommand().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func newRootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:           "cetus",
		Short:         "Render HTML compositions into video files",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(newRenderCommand())
	root.AddCommand(newValidateCommand())
	root.AddCommand(newPreviewCommand())
	root.AddCommand(newUpdateCommand())
	root.AddCommand(newVersionCommand())
	return root
}

func newRenderCommand() *cobra.Command {
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
	var resume bool
	var noGPU bool

	cmd := &cobra.Command{
		Use:   "render cetus.html -o out.mp4",
		Short: "Render an HTML composition to MP4 or WebM",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
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
			framesDir = strings.TrimSpace(framesDir)
			if resume && framesDir == "" {
				framesDir = ".cetus-frames"
			}
			if framesDir == "" && cmd.Flags().Changed("concurrency") && concurrency > 1 {
				return fmt.Errorf("parallel frame capture requires --frames-dir or --resume")
			}

			input := args[0]
			if _, err := os.Stat(input); err != nil {
				return fmt.Errorf("stat input composition: %w", err)
			}
			audioPath = strings.TrimSpace(audioPath)
			if audioPath == "" && audioControlFlagsChanged(cmd) {
				return fmt.Errorf("audio controls require --audio")
			}
			if audioPath != "" {
				info, err := os.Stat(audioPath)
				if err != nil {
					return fmt.Errorf("stat audio file: %w", err)
				}
				if info.IsDir() {
					return fmt.Errorf("audio path %q is a directory", audioPath)
				}
			}

			start := time.Now()
			progress := newRenderProgressLogger(cmd.ErrOrStderr())

			chromePath, ffmpegPath, err := assets.EnsureAssetsWithProgress(version.Version, func(event assets.ProgressEvent) {
				progress.Stage("%s...", event.Message)
			})
			if err != nil {
				return err
			}

			progress.Stage("Parsing composition...")
			composition, err := compose.Parse(input)
			if err != nil {
				return err
			}

			fpsOverride := 0
			if cmd.Flags().Changed("fps") {
				fpsOverride = fps
			}
			if err := composition.ApplyOverrides(fpsOverride, width, height); err != nil {
				return err
			}
			renderDuration := float64(composition.TotalFrames) / float64(composition.FPS)
			if audioPath != "" && audioStartSeconds >= renderDuration {
				return fmt.Errorf("audio start %.3fs must be before render duration %.3fs", audioStartSeconds, renderDuration)
			}
			progress.Stage(
				"Composition %q: %dx%d, %.2fs at %d fps (%d frames, %d clips)",
				composition.ID,
				composition.Width,
				composition.Height,
				composition.Duration,
				composition.FPS,
				composition.TotalFrames,
				len(composition.Clips),
			)

			resolvedFormat, err := encoder.ResolveFormat(output, format)
			if err != nil {
				return err
			}

			ctx, cancel := renderContext(cmd.Context(), timeoutSeconds)
			defer cancel()

			encoderOpts := encoder.Options{
				AudioPath:           audioPath,
				AudioVolume:         audioVolume,
				AudioVolumeSet:      cmd.Flags().Changed("audio-volume"),
				AudioLoop:           audioLoop,
				AudioStartSeconds:   audioStartSeconds,
				AudioFadeInSeconds:  audioFadeInSeconds,
				AudioFadeOutSeconds: audioFadeOutSeconds,
				DurationSeconds:     renderDuration,
			}
			browserOpts := browser.Options{
				ChromePath: chromePath,
				NoGPU:      noGPU,
			}

			if framesDir != "" {
				if resume {
					progress.Stage("Using resumable frame cache at %s with %d worker(s)...", framesDir, concurrency)
				} else {
					progress.Stage("Saving rendered frames to %s with %d worker(s)...", framesDir, concurrency)
				}
				progress.ResetFrames()
				if err := browser.CaptureFramesToCache(ctx, composition, browser.WorkerOptions{
					HTMLPath:       input,
					BrowserOptions: browserOpts,
					CaptureOptions: browser.CaptureOptions{
						FramesDir: framesDir,
						Resume:    resume,
					},
					Workers: concurrency,
				}, progress.Frames); err != nil {
					return err
				}

				progress.Stage("Starting %s encoder...", resolvedFormat)
				enc, err := encoder.New(ffmpegPath, output, composition.FPS, resolvedFormat, encoderOpts)
				if err != nil {
					return err
				}
				progress.Stage("Encoding cached frames...")
				progress.ResetFrames()
				if err := browser.EncodeCachedFrames(composition, enc, framesDir, progress.Frames); err != nil {
					_ = enc.Close()
					return err
				}
				progress.Stage("Finalizing %s output...", resolvedFormat)
				if err := enc.Close(); err != nil {
					return err
				}

				fmt.Fprintf(cmd.OutOrStdout(), "Rendered %s (%d frames, %s) in %s\n", output, composition.TotalFrames, resolvedFormat, time.Since(start).Round(time.Millisecond))
				return nil
			}

			progress.Stage("Starting %s encoder...", resolvedFormat)
			enc, err := encoder.New(ffmpegPath, output, composition.FPS, resolvedFormat, encoderOpts)
			if err != nil {
				return err
			}

			progress.Stage("Opening headless browser...")
			b, err := browser.New(ctx, input, composition, browserOpts)
			if err != nil {
				_ = enc.Close()
				return err
			}
			defer b.Close()

			progress.Stage("Starting frame rendering...")
			progress.ResetFrames()
			if err := b.CaptureWithOptions(ctx, composition, enc, progress.Frames, browser.CaptureOptions{
				FramesDir: framesDir,
				Resume:    resume,
			}); err != nil {
				_ = enc.Close()
				return err
			}
			progress.Stage("Finalizing %s output...", resolvedFormat)
			if err := enc.Close(); err != nil {
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Rendered %s (%d frames, %s) in %s\n", output, composition.TotalFrames, resolvedFormat, time.Since(start).Round(time.Millisecond))
			return nil
		},
	}

	cmd.Flags().StringVarP(&output, "output", "o", "", "output file path")
	cmd.Flags().IntVar(&fps, "fps", 30, "frames per second")
	cmd.Flags().IntVar(&width, "width", 0, "output width in pixels")
	cmd.Flags().IntVar(&height, "height", 0, "output height in pixels")
	cmd.Flags().StringVar(&format, "format", "", "output format: mp4 or webm")
	cmd.Flags().IntVar(&concurrency, "concurrency", 1, "parallel frame capture workers when --frames-dir or --resume is used")
	cmd.Flags().IntVar(&timeoutSeconds, "timeout", 0, "max render time in seconds; 0 disables the total render deadline")
	cmd.Flags().StringVar(&audioPath, "audio", "", "audio file to mux into the output")
	cmd.Flags().Float64Var(&audioVolume, "audio-volume", 1, "audio volume from 0.0 to 1.0")
	cmd.Flags().BoolVar(&audioLoop, "audio-loop", false, "loop audio until the render duration is reached")
	cmd.Flags().Float64Var(&audioStartSeconds, "audio-start", 0, "audio start time in seconds on the render timeline")
	cmd.Flags().Float64Var(&audioFadeInSeconds, "audio-fade-in", 0, "audio fade-in duration in seconds")
	cmd.Flags().Float64Var(&audioFadeOutSeconds, "audio-fade-out", 0, "audio fade-out duration in seconds")
	cmd.Flags().StringVar(&framesDir, "frames-dir", "", "directory for cached PNG frames")
	cmd.Flags().BoolVar(&resume, "resume", false, "reuse existing frames from --frames-dir; defaults to .cetus-frames when no directory is set")
	cmd.Flags().BoolVar(&noGPU, "no-gpu", false, "disable GPU acceleration")
	_ = cmd.MarkFlagRequired("output")

	return cmd
}

func renderContext(parent context.Context, timeoutSeconds int) (context.Context, context.CancelFunc) {
	if timeoutSeconds > 0 {
		return context.WithTimeout(parent, time.Duration(timeoutSeconds)*time.Second)
	}
	return context.WithCancel(parent)
}

func validateAudioFlagValues(volume, start, fadeIn, fadeOut float64) error {
	if volume < 0 || volume > 1 {
		return fmt.Errorf("audio volume must be between 0.0 and 1.0")
	}
	if start < 0 {
		return fmt.Errorf("audio start must be zero or positive")
	}
	if fadeIn < 0 {
		return fmt.Errorf("audio fade-in must be zero or positive")
	}
	if fadeOut < 0 {
		return fmt.Errorf("audio fade-out must be zero or positive")
	}
	return nil
}

func audioControlFlagsChanged(cmd *cobra.Command) bool {
	for _, name := range []string{"audio-volume", "audio-loop", "audio-start", "audio-fade-in", "audio-fade-out"} {
		if cmd.Flags().Changed(name) {
			return true
		}
	}
	return false
}

func newPreviewCommand() *cobra.Command {
	var port int
	var noOpen bool

	cmd := &cobra.Command{
		Use:   "preview cetus.html",
		Short: "Serve an HTML composition locally with live reload",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if port < 0 {
				return fmt.Errorf("port must be zero or positive")
			}
			if _, err := os.Stat(args[0]); err != nil {
				return fmt.Errorf("stat input composition: %w", err)
			}
			return preview.Serve(args[0], port, noOpen)
		},
	}

	cmd.Flags().IntVar(&port, "port", 0, "port to listen on")
	cmd.Flags().BoolVar(&noOpen, "no-open", false, "do not open a browser automatically")
	return cmd
}

func newUpdateCommand() *cobra.Command {
	var manifestURL string
	var channel string

	cmd := &cobra.Command{
		Use:   "update",
		Short: "Check for and apply Cetus updates",
	}

	cmd.PersistentFlags().StringVar(&manifestURL, "manifest-url", updater.DefaultManifestURL, "release manifest URL")
	cmd.PersistentFlags().StringVar(&channel, "channel", updater.ChannelAuto, "update channel: auto, stable, beta, or rc")
	cmd.AddCommand(newUpdateCheckCommand(&manifestURL, &channel))
	cmd.AddCommand(newUpdateApplyCommand(&manifestURL, &channel))
	return cmd
}

func newUpdateCheckCommand(manifestURL, channel *string) *cobra.Command {
	return &cobra.Command{
		Use:   "check",
		Short: "Check whether a Cetus update is available",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if homebrew, path := updater.IsHomebrewManaged(); homebrew {
				fmt.Fprintf(cmd.OutOrStdout(), "Cetus is managed by Homebrew")
				if path != "" {
					fmt.Fprintf(cmd.OutOrStdout(), " at %s", path)
				}
				fmt.Fprintln(cmd.OutOrStdout(), ".")
				fmt.Fprintln(cmd.OutOrStdout(), "Use: brew update && brew upgrade cenvero-cetus")
				return nil
			}

			result, err := updater.Check(cmd.Context(), version.Version, *manifestURL, *channel)
			if err != nil {
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Current: %s\n", result.CurrentVersion)
			fmt.Fprintf(cmd.OutOrStdout(), "Latest:  %s\n", result.LatestVersion)
			fmt.Fprintf(cmd.OutOrStdout(), "Channel: %s\n", result.Channel)
			fmt.Fprintf(cmd.OutOrStdout(), "Platform: %s\n", result.Platform)
			if result.ReleaseNotesURL != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "Release notes: %s\n", result.ReleaseNotesURL)
			}
			if result.UpdateAvailable {
				if result.CurrentComparable {
					fmt.Fprintln(cmd.OutOrStdout(), "Update available. Use: cetus update apply")
				} else {
					fmt.Fprintln(cmd.OutOrStdout(), "Latest release is available. Current version is not a release version.")
				}
				return nil
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Cetus is up to date.")
			return nil
		},
	}
}

func newUpdateApplyCommand(manifestURL, channel *string) *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Download and install the latest Cetus release",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if homebrew, path := updater.IsHomebrewManaged(); homebrew {
				fmt.Fprintf(cmd.OutOrStdout(), "Cetus is managed by Homebrew")
				if path != "" {
					fmt.Fprintf(cmd.OutOrStdout(), " at %s", path)
				}
				fmt.Fprintln(cmd.OutOrStdout(), ".")
				fmt.Fprintln(cmd.OutOrStdout(), "Use: brew update && brew upgrade cenvero-cetus")
				return nil
			}

			result, err := updater.Apply(cmd.Context(), version.Version, *manifestURL, *channel, force)
			if err != nil {
				return err
			}
			if !result.Applied {
				if result.Check != nil && !result.Check.UpdateAvailable {
					fmt.Fprintln(cmd.OutOrStdout(), "Cetus is already up to date.")
					return nil
				}
				fmt.Fprintln(cmd.OutOrStdout(), "No update applied.")
				return nil
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Updated Cetus from %s to %s.\n", result.Check.CurrentVersion, result.Check.LatestVersion)
			fmt.Fprintf(cmd.OutOrStdout(), "Channel: %s\n", result.Check.Channel)
			fmt.Fprintf(cmd.OutOrStdout(), "Installed: %s\n", result.InstalledPath)
			return nil
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "apply the latest release even if the current version is already current")
	return cmd
}

func newVersionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintln(cmd.OutOrStdout(), version.String())
			return nil
		},
	}
}
