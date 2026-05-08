package browser

import (
	"context"
	"fmt"
	"net/url"
	"path/filepath"
	"time"

	"github.com/cenvero/cetus/internal/compose"
	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/chromedp"
)

type Browser struct {
	ctx    context.Context
	cancel context.CancelFunc
}

type Options struct {
	ChromePath string
	NoGPU      bool
}

func New(ctx context.Context, htmlPath string, composition *compose.Composition, opts Options) (*Browser, error) {
	if opts.ChromePath == "" {
		return nil, fmt.Errorf("chrome path is required")
	}
	if composition == nil {
		return nil, fmt.Errorf("composition is required")
	}

	allocOpts := []chromedp.ExecAllocatorOption{
		chromedp.ExecPath(opts.ChromePath),
		chromedp.NoFirstRun,
		chromedp.NoDefaultBrowserCheck,
		chromedp.Flag("headless", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("disable-dev-shm-usage", true),
		chromedp.Flag("remote-debugging-port", "0"),
		chromedp.Flag("hide-scrollbars", true),
		chromedp.Flag("mute-audio", true),
		chromedp.Flag("disable-background-networking", true),
		chromedp.Flag("disable-extensions", true),
		chromedp.Flag("disable-translate", true),
		chromedp.Flag("disable-sync", true),
		chromedp.WindowSize(composition.Width, composition.Height),
	}
	if opts.NoGPU {
		allocOpts = append(allocOpts, chromedp.Flag("disable-gpu", true))
	}

	allocCtx, allocCancel := chromedp.NewExecAllocator(ctx, allocOpts...)
	browserCtx, browserCancel := chromedp.NewContext(allocCtx)
	cancel := func() {
		browserCancel()
		allocCancel()
	}

	fileURL, err := compositionFileURL(htmlPath)
	if err != nil {
		cancel()
		return nil, err
	}

	loadCtx, loadCancel := context.WithTimeout(browserCtx, 60*time.Second)
	defer loadCancel()

	if err := chromedp.Run(loadCtx,
		emulation.SetDeviceMetricsOverride(int64(composition.Width), int64(composition.Height), 1, false),
		chromedp.Navigate(fileURL),
		chromedp.WaitReady("body", chromedp.ByQuery),
		chromedp.Evaluate(`new Promise(function(resolve) {
			if (document.readyState === "complete") {
				resolve(true);
				return;
			}
			window.addEventListener("load", function() { resolve(true); }, { once: true });
		})`, nil, awaitPromise),
	); err != nil {
		cancel()
		return nil, fmt.Errorf("load composition in browser: %w", err)
	}

	return &Browser{ctx: browserCtx, cancel: cancel}, nil
}

func (b *Browser) Close() {
	if b != nil && b.cancel != nil {
		b.cancel()
	}
}

func (b *Browser) Context() context.Context {
	return b.ctx
}

func compositionFileURL(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve composition path: %w", err)
	}
	u := url.URL{Scheme: "file", Path: filepath.ToSlash(abs)}
	return u.String(), nil
}
