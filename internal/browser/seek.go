package browser

import (
	"context"
	"fmt"

	"github.com/cenvero/cetus/internal/compose"
	"github.com/chromedp/chromedp"
)

// SeekFrame opens the composition in a headless browser, seeks to the given
// frame index using the Cetus seek engine, captures a lossless PNG screenshot,
// and returns the raw bytes. The caller is responsible for writing to disk.
func SeekFrame(ctx context.Context, htmlPath string, composition *compose.Composition, frameIndex int, opts Options) ([]byte, error) {
	b, err := New(ctx, htmlPath, composition, opts)
	if err != nil {
		return nil, fmt.Errorf("open browser: %w", err)
	}
	defer b.Close()

	t := float64(frameIndex) / float64(composition.FPS)
	if err := chromedp.Run(b.ctx, chromedp.Evaluate(buildSeekScript(frameIndex, composition.FPS), nil, awaitPromise)); err != nil {
		return nil, fmt.Errorf("seek frame %d at %.3fs: %w", frameIndex, t, err)
	}

	data, err := b.captureFrame(t)
	if err != nil {
		return nil, fmt.Errorf("capture frame %d at %.3fs: %w", frameIndex, t, err)
	}
	return data, nil
}
