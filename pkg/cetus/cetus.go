package cetus

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
	"github.com/cenvero/cetus/internal/version"
)

type RenderOptions struct {
	FPS                 int
	Width               int
	Height              int
	Format              string
	AudioPath           string
	AudioVolume         float64
	AudioVolumeSet      bool
	AudioLoop           bool
	AudioStartSeconds   float64
	AudioFadeInSeconds  float64
	AudioFadeOutSeconds float64
	FramesDir           string
	Resume              bool
	Concurrency         int
	NoGPU               bool
	Timeout             time.Duration
	Quality             int
	Scale               string
	KeepFrames          bool
	SubtitlesPath       string
}

func Parse(path string) (*compose.Composition, error) {
	return compose.Parse(path)
}

func Render(ctx context.Context, inputPath, outputPath string, opts RenderOptions) error {
	if _, err := os.Stat(inputPath); err != nil {
		return fmt.Errorf("stat input composition: %w", err)
	}
	if outputPath == "" {
		return fmt.Errorf("output path is required")
	}
	if err := validateRenderAudioOptions(opts); err != nil {
		return err
	}
	scaleFilter, err := encoder.ParseScale(opts.Scale)
	if err != nil {
		return err
	}
	framesDir := strings.TrimSpace(opts.FramesDir)
	if opts.Resume && framesDir == "" {
		framesDir = ".cetus-frames"
	}
	workers := opts.Concurrency
	if workers <= 0 {
		workers = 1
	}
	audioPath := strings.TrimSpace(opts.AudioPath)
	if audioPath != "" {
		info, err := os.Stat(audioPath)
		if err != nil {
			return fmt.Errorf("stat audio file: %w", err)
		}
		if info.IsDir() {
			return fmt.Errorf("audio path %q is a directory", audioPath)
		}
	}

	if opts.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.Timeout)
		defer cancel()
	}

	chromePath, ffmpegPath, err := assets.EnsureAssets(version.Version)
	if err != nil {
		return err
	}

	composition, err := compose.Parse(inputPath)
	if err != nil {
		return err
	}
	if err := composition.ApplyOverrides(opts.FPS, opts.Width, opts.Height); err != nil {
		return err
	}
	renderDuration := float64(composition.TotalFrames) / float64(composition.FPS)
	if audioPath != "" && opts.AudioStartSeconds >= renderDuration {
		return fmt.Errorf("audio start %.3fs must be before render duration %.3fs", opts.AudioStartSeconds, renderDuration)
	}

	encoderOpts := encoder.Options{
		AudioPath:           audioPath,
		AudioVolume:         opts.AudioVolume,
		AudioVolumeSet:      opts.AudioVolumeSet,
		AudioLoop:           opts.AudioLoop,
		AudioStartSeconds:   opts.AudioStartSeconds,
		AudioFadeInSeconds:  opts.AudioFadeInSeconds,
		AudioFadeOutSeconds: opts.AudioFadeOutSeconds,
		DurationSeconds:     renderDuration,
		Quality:             opts.Quality,
		Scale:               scaleFilter,
		FrameCodec:          "png",
		SubtitlesPath:       strings.TrimSpace(opts.SubtitlesPath),
	}
	browserOpts := browser.Options{
		ChromePath: chromePath,
		NoGPU:      opts.NoGPU,
	}

	if framesDir != "" {
		if err := browser.CaptureFramesToCache(ctx, composition, browser.WorkerOptions{
			HTMLPath:       inputPath,
			BrowserOptions: browserOpts,
			CaptureOptions: browser.CaptureOptions{
				FramesDir: framesDir,
				Resume:    opts.Resume,
			},
			Workers: workers,
		}, nil); err != nil {
			return err
		}
		enc, err := encoder.New(ffmpegPath, outputPath, composition.FPS, opts.Format, encoderOpts)
		if err != nil {
			return err
		}
		if err := browser.EncodeCachedFrames(composition, enc, framesDir, nil); err != nil {
			_ = enc.Close()
			return err
		}
		if err := enc.Close(); err != nil {
			return err
		}
		if !opts.KeepFrames {
			_ = os.RemoveAll(framesDir)
		}
		return nil
	}

	enc, err := encoder.New(ffmpegPath, outputPath, composition.FPS, opts.Format, encoderOpts)
	if err != nil {
		return err
	}

	b, err := browser.New(ctx, inputPath, composition, browserOpts)
	if err != nil {
		_ = enc.Close()
		return err
	}
	defer b.Close()

	if err := b.CaptureWithOptions(ctx, composition, enc, nil, browser.CaptureOptions{
		FramesDir: framesDir,
		Resume:    opts.Resume,
	}); err != nil {
		_ = enc.Close()
		return err
	}

	if err := enc.Close(); err != nil {
		return err
	}
	return nil
}

func Preview(htmlPath string, port int, noOpen bool) error {
	return preview.Serve(htmlPath, port, noOpen)
}

func Version() string {
	return version.String()
}

func validateRenderAudioOptions(opts RenderOptions) error {
	if opts.AudioVolume < 0 || opts.AudioVolume > 1 {
		return fmt.Errorf("audio volume must be between 0.0 and 1.0")
	}
	if opts.AudioStartSeconds < 0 {
		return fmt.Errorf("audio start must be zero or positive")
	}
	if opts.AudioFadeInSeconds < 0 {
		return fmt.Errorf("audio fade-in must be zero or positive")
	}
	if opts.AudioFadeOutSeconds < 0 {
		return fmt.Errorf("audio fade-out must be zero or positive")
	}

	audioPath := strings.TrimSpace(opts.AudioPath)
	if audioPath == "" && (opts.AudioVolumeSet || opts.AudioLoop || opts.AudioStartSeconds > 0 || opts.AudioFadeInSeconds > 0 || opts.AudioFadeOutSeconds > 0) {
		return fmt.Errorf("audio options require AudioPath")
	}
	return nil
}
