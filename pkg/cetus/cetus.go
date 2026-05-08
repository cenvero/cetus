package cetus

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/cenvero/cetus/internal/assets"
	"github.com/cenvero/cetus/internal/browser"
	"github.com/cenvero/cetus/internal/compose"
	"github.com/cenvero/cetus/internal/encoder"
	"github.com/cenvero/cetus/internal/preview"
	"github.com/cenvero/cetus/internal/version"
)

type RenderOptions struct {
	FPS     int
	Width   int
	Height  int
	Format  string
	NoGPU   bool
	Timeout time.Duration
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

	enc, err := encoder.New(ffmpegPath, outputPath, composition.FPS, opts.Format)
	if err != nil {
		return err
	}

	b, err := browser.New(ctx, inputPath, composition, browser.Options{
		ChromePath: chromePath,
		NoGPU:      opts.NoGPU,
	})
	if err != nil {
		_ = enc.Close()
		return err
	}
	defer b.Close()

	if err := b.Capture(ctx, composition, enc); err != nil {
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
