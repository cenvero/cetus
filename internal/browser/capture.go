package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/cenvero/cetus/internal/compose"
	"github.com/cenvero/cetus/internal/encoder"
	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/page"
	cdpruntime "github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

type CaptureProgress struct {
	CompletedFrames int
	TotalFrames     int
	TimeSeconds     float64
}

type CaptureProgressFunc func(CaptureProgress)

type CaptureOptions struct {
	FramesDir string
	Resume    bool
}

type WorkerOptions struct {
	HTMLPath       string
	BrowserOptions Options
	CaptureOptions CaptureOptions
	Workers        int
}

func (b *Browser) Capture(ctx context.Context, composition *compose.Composition, enc *encoder.Encoder, progress CaptureProgressFunc) error {
	return b.CaptureWithOptions(ctx, composition, enc, progress, CaptureOptions{})
}

func (b *Browser) CaptureWithOptions(ctx context.Context, composition *compose.Composition, enc *encoder.Encoder, progress CaptureProgressFunc, opts CaptureOptions) error {
	if b == nil {
		return fmt.Errorf("browser is nil")
	}
	if composition == nil {
		return fmt.Errorf("composition is required")
	}
	if enc == nil {
		return fmt.Errorf("encoder is required")
	}

	cache, err := newFrameCache(opts, composition)
	if err != nil {
		return err
	}

	reportCaptureProgress(progress, 0, composition.TotalFrames, 0)
	for frame := 0; frame < composition.TotalFrames; frame++ {
		select {
		case <-ctx.Done():
			return fmt.Errorf("capture canceled: %w", ctx.Err())
		default:
		}

		t := float64(frame) / float64(composition.FPS)
		data, cached, err := cachedFrame(cache, frame)
		if err != nil {
			return fmt.Errorf("read cached frame %d: %w", frame, err)
		}
		if !cached {
			if err := chromedp.Run(b.ctx, chromedp.Evaluate(buildSeekScript(frame, composition.FPS), nil, awaitPromise)); err != nil {
				return fmt.Errorf("seek frame %d at %.6fs: %w", frame, t, err)
			}

			data, err = b.captureFrame(t)
			if err != nil {
				return fmt.Errorf("capture frame %d at %.6fs: %w", frame, t, err)
			}
			if cache != nil {
				if err := cache.write(frame, data); err != nil {
					return fmt.Errorf("write cached frame %d: %w", frame, err)
				}
			}
		}
		if err := enc.WriteFrame(data); err != nil {
			return fmt.Errorf("encode frame %d: %w", frame, err)
		}
		reportCaptureProgress(progress, frame+1, composition.TotalFrames, t)
	}

	return nil
}

func cachedFrame(cache *frameCache, frame int) ([]byte, bool, error) {
	if cache == nil || !cache.resume {
		return nil, false, nil
	}
	return cache.read(frame)
}

func CaptureFramesToCache(ctx context.Context, composition *compose.Composition, opts WorkerOptions, progress CaptureProgressFunc) error {
	if composition == nil {
		return fmt.Errorf("composition is required")
	}
	if strings.TrimSpace(opts.HTMLPath) == "" {
		return fmt.Errorf("html path is required")
	}
	if strings.TrimSpace(opts.CaptureOptions.FramesDir) == "" {
		return fmt.Errorf("frames directory is required")
	}

	cache, err := newFrameCache(opts.CaptureOptions, composition)
	if err != nil {
		return err
	}
	if cache == nil {
		return fmt.Errorf("frame cache is required")
	}

	workers := effectiveCaptureWorkers(opts.Workers, composition.TotalFrames)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	frames := make(chan int)
	var completed atomic.Int64
	var firstErr error
	var errMu sync.Mutex
	setErr := func(err error) {
		if err == nil {
			return
		}
		errMu.Lock()
		if firstErr == nil {
			firstErr = err
			cancel()
		}
		errMu.Unlock()
	}

	reportCaptureProgress(progress, 0, composition.TotalFrames, 0)
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var b *Browser
			defer func() {
				if b != nil {
					b.Close()
				}
			}()

			for frame := range frames {
				select {
				case <-ctx.Done():
					return
				default:
				}

				cached, err := frameCached(cache, frame)
				if err != nil {
					setErr(fmt.Errorf("read cached frame %d: %w", frame, err))
					return
				}
				if !cached {
					if b == nil {
						created, err := New(ctx, opts.HTMLPath, composition, opts.BrowserOptions)
						if err != nil {
							setErr(fmt.Errorf("open worker browser: %w", err))
							return
						}
						b = created
					}
					if err := b.captureFrameToCache(frame, composition, cache); err != nil {
						setErr(err)
						return
					}
				}

				done := int(completed.Add(1))
				reportCaptureProgress(progress, done, composition.TotalFrames, float64(frame)/float64(composition.FPS))
			}
		}()
	}

sendLoop:
	for frame := 0; frame < composition.TotalFrames; frame++ {
		select {
		case <-ctx.Done():
			break sendLoop
		case frames <- frame:
		}
	}
	close(frames)
	wg.Wait()

	if firstErr != nil {
		return firstErr
	}
	if err := ctx.Err(); err != nil && completed.Load() < int64(composition.TotalFrames) {
		return fmt.Errorf("capture canceled: %w", err)
	}
	return nil
}

func EncodeCachedFrames(composition *compose.Composition, enc *encoder.Encoder, framesDir string, progress CaptureProgressFunc) error {
	if composition == nil {
		return fmt.Errorf("composition is required")
	}
	if enc == nil {
		return fmt.Errorf("encoder is required")
	}
	cache, err := newFrameCache(CaptureOptions{FramesDir: framesDir, Resume: true}, composition)
	if err != nil {
		return err
	}
	if cache == nil {
		return fmt.Errorf("frame cache is required")
	}

	reportCaptureProgress(progress, 0, composition.TotalFrames, 0)
	for frame := 0; frame < composition.TotalFrames; frame++ {
		data, ok, err := cache.read(frame)
		if err != nil {
			return fmt.Errorf("read cached frame %d: %w", frame, err)
		}
		if !ok {
			return fmt.Errorf("cached frame %d is missing from %s", frame, framesDir)
		}
		if err := enc.WriteFrame(data); err != nil {
			return fmt.Errorf("encode frame %d: %w", frame, err)
		}
		reportCaptureProgress(progress, frame+1, composition.TotalFrames, float64(frame)/float64(composition.FPS))
	}
	return nil
}

// CompositionFromCache reads config.cetus from dir and returns a minimal Composition for encoding.
func CompositionFromCache(dir string) (*compose.Composition, error) {
	manifest, ok, err := readFrameCacheManifest(dir)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("config.cetus not found in %s; cannot encode without it", dir)
	}
	return &compose.Composition{
		ID:          manifest.CompositionID,
		Width:       manifest.Width,
		Height:      manifest.Height,
		FPS:         manifest.FPS,
		Duration:    manifest.Duration,
		TotalFrames: manifest.TotalFrames,
	}, nil
}

// FrameCodecFromCache returns the frame codec stored in config.cetus, defaulting to "webp".
func FrameCodecFromCache(dir string) string {
	manifest, ok, _ := readFrameCacheManifest(dir)
	if !ok || manifest.FrameFormat == "" {
		return "webp"
	}
	return manifest.FrameFormat
}

// ReadCachedFrame returns the raw frame bytes for the given frame index from a frames directory.
func ReadCachedFrame(dir string, frame int) ([]byte, bool, error) {
	root, err := openFrameCacheRoot(dir)
	if err != nil {
		return nil, false, err
	}
	defer root.Close()

	data, err := root.ReadFile(frameFileName(frame))
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return data, len(data) > 0, nil
}

func frameCached(cache *frameCache, frame int) (bool, error) {
	_, ok, err := cache.read(frame)
	return ok, err
}

func (b *Browser) captureFrameToCache(frame int, composition *compose.Composition, cache *frameCache) error {
	t := float64(frame) / float64(composition.FPS)
	if err := chromedp.Run(b.ctx, chromedp.Evaluate(buildSeekScript(frame, composition.FPS), nil, awaitPromise)); err != nil {
		return fmt.Errorf("seek frame %d at %.6fs: %w", frame, t, err)
	}
	data, err := b.captureFrame(t)
	if err != nil {
		return fmt.Errorf("capture frame %d at %.6fs: %w", frame, t, err)
	}
	if err := cache.write(frame, data); err != nil {
		return fmt.Errorf("write cached frame %d: %w", frame, err)
	}
	return nil
}

func effectiveCaptureWorkers(workers, totalFrames int) int {
	if workers <= 0 {
		workers = 1
	}
	if totalFrames > 0 && workers > totalFrames {
		return totalFrames
	}
	return workers
}

func reportCaptureProgress(progress CaptureProgressFunc, completedFrames, totalFrames int, timeSeconds float64) {
	if progress != nil {
		progress(CaptureProgress{
			CompletedFrames: completedFrames,
			TotalFrames:     totalFrames,
			TimeSeconds:     timeSeconds,
		})
	}
}

func (b *Browser) captureFrame(t float64) ([]byte, error) {
	if b.useBeginFrame {
		return beginFrame(b.ctx, t)
	}
	return captureScreenshot(b.ctx)
}

func awaitPromise(p *cdpruntime.EvaluateParams) *cdpruntime.EvaluateParams {
	return p.WithAwaitPromise(true)
}

func beginFrame(ctx context.Context, t float64) ([]byte, error) {
	targetCtx, err := targetExecutorContext(ctx)
	if err != nil {
		return nil, err
	}

	var result struct {
		HasDamage      bool   `json:"hasDamage"`
		ScreenshotData []byte `json:"screenshotData"`
	}
	params := map[string]any{
		"frameTimeTicks": t,
		"screenshot": map[string]any{
			"format":  "webp",
			"quality": 100,
		},
	}
	if err := cdp.Execute(targetCtx, "HeadlessExperimental.beginFrame", params, &result); err != nil {
		return nil, fmt.Errorf("call HeadlessExperimental.beginFrame: %w", err)
	}
	if len(result.ScreenshotData) == 0 {
		return nil, fmt.Errorf("HeadlessExperimental.beginFrame returned no screenshot data")
	}
	return result.ScreenshotData, nil
}

func captureScreenshot(ctx context.Context) ([]byte, error) {
	targetCtx, err := targetExecutorContext(ctx)
	if err != nil {
		return nil, err
	}

	if err := page.BringToFront().Do(targetCtx); err != nil {
		return nil, fmt.Errorf("activate page before screenshot: %w", err)
	}

	data, err := page.CaptureScreenshot().
		WithFormat(page.CaptureScreenshotFormatWebp).
		WithQuality(100).
		WithFromSurface(true).
		Do(targetCtx)
	if err != nil {
		return nil, fmt.Errorf("capture page screenshot: %w", err)
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("Page.captureScreenshot returned no screenshot data")
	}
	return data, nil
}

func targetExecutorContext(ctx context.Context) (context.Context, error) {
	c := chromedp.FromContext(ctx)
	if c == nil || c.Target == nil {
		return nil, fmt.Errorf("browser target is unavailable")
	}
	return cdp.WithExecutor(ctx, c.Target), nil
}

func buildSeekScript(frame, fps int) string {
	t := float64(frame) / float64(fps)
	timeLiteral := strconv.FormatFloat(t, 'f', -1, 64)
	frameLiteral := strconv.Itoa(frame)
	fpsLiteral := strconv.Itoa(fps)
	return `(async function() {
  const cetusTime = ` + timeLiteral + `;
  const cetusFrame = ` + frameLiteral + `;
  const cetusFPS = ` + fpsLiteral + `;
  document.__cetusTime = cetusTime;
  document.__cetusFrame = cetusFrame;
  document.__cetusFPS = cetusFPS;
  window.__cetusTime = cetusTime;
  window.__cetusFrame = cetusFrame;
  window.__cetusFPS = cetusFPS;

  const frameDetail = Object.freeze({
    time: cetusTime,
    frame: cetusFrame,
    fps: cetusFPS
  });

  function delay(ms) {
    return new Promise(function(resolve) {
      setTimeout(resolve, ms);
    });
  }

  function withTimeout(promise, ms) {
    return Promise.race([
      Promise.resolve(promise).catch(function() {}),
      delay(ms)
    ]);
  }

  async function runFrameHooks() {
    const hooks = [];
    if (typeof window.__cetusSeek === "function") {
      hooks.push(window.__cetusSeek(cetusTime, frameDetail));
    }
    if (typeof window.__cetusRenderFrame === "function") {
      hooks.push(window.__cetusRenderFrame(cetusTime, frameDetail));
    }
    if (Array.isArray(window.__cetusFrameCallbacks)) {
      for (const hook of window.__cetusFrameCallbacks) {
        if (typeof hook === "function") {
          hooks.push(hook(cetusTime, frameDetail));
        }
      }
    }
    if (hooks.length > 0) {
      await Promise.all(hooks.map(function(hookResult) {
        return withTimeout(hookResult, 5000);
      }));
    }
    document.dispatchEvent(new CustomEvent("cetus:seek", { detail: frameDetail }));
  }

  function clipTimingFor(target) {
    const clip = target && typeof target.closest === "function" ? target.closest(".clip") : null;
    if (!clip) {
      return { active: true, localTime: cetusTime };
    }
    const start = Number(clip.dataset.start) || 0;
    return {
      localTime: Math.max(0, cetusTime - start)
    };
  }

  function seekWebAnimations() {
    if (typeof document.getAnimations !== "function") {
      return;
    }
    for (const animation of document.getAnimations({ subtree: true })) {
      try {
        if (!animation || !animation.effect) {
          continue;
        }
        const target = animation.effect.target;
        const timing = clipTimingFor(target);
        const targetTime = Math.max(0, timing.localTime * 1000);
        animation.pause();
        animation.currentTime = targetTime;
      } catch (_) {
      }
    }
  }

  function seekTimelines() {
    const timelines = Array.isArray(window.__timelines) ? window.__timelines : [];
    for (const timeline of timelines) {
      if (timeline && typeof timeline.seek === "function") {
        timeline.seek(cetusTime, false);
      }
    }
  }

  function activeFor(el) {
    const start = Number(el.dataset.start);
    const duration = Number(el.dataset.duration);
    return Number.isFinite(start) && Number.isFinite(duration) &&
      cetusTime >= start && cetusTime < start + duration;
  }

  function waitForEvent(target, eventNames, timeoutMS) {
    return new Promise(function(resolve) {
      const done = function() {
        clearTimeout(timer);
        for (const eventName of eventNames) {
          target.removeEventListener(eventName, done);
        }
        resolve();
      };
      const timer = setTimeout(done, timeoutMS);
      for (const eventName of eventNames) {
        target.addEventListener(eventName, done, { once: true });
      }
    });
  }

  async function waitForMediaMetadata(media) {
    if (media.readyState >= 1) {
      return;
    }
    try {
      media.load();
    } catch (_) {
    }
    await waitForEvent(media, ["loadedmetadata", "error"], 2000);
  }

  async function waitForVideoFrame(video) {
    if (typeof video.requestVideoFrameCallback !== "function") {
      await waitForPaint();
      return;
    }
    await new Promise(function(resolve) {
      const timer = setTimeout(resolve, 500);
      try {
        video.requestVideoFrameCallback(function() {
          clearTimeout(timer);
          resolve();
        });
      } catch (_) {
        clearTimeout(timer);
        resolve();
      }
    });
  }

  async function waitForSeek(media, targetTime) {
    if (!Number.isFinite(targetTime) || targetTime < 0) {
      return;
    }

    await waitForMediaMetadata(media);

    try {
      if (typeof media.pause === "function") {
        media.pause();
      }
      if (Math.abs(media.currentTime - targetTime) >= 0.001) {
        const seek = waitForEvent(media, ["seeked", "error"], 2000);
        if (typeof media.fastSeek === "function") {
          media.fastSeek(targetTime);
        } else {
          media.currentTime = targetTime;
        }
        await seek;
      }
    } catch (_) {
    }

    if (media.tagName.toLowerCase() === "video") {
      await waitForVideoFrame(media);
    }
  }

  async function waitForImage(img) {
    if (!(img instanceof HTMLImageElement)) {
      return;
    }
    if (!img.complete) {
      await waitForEvent(img, ["load", "error"], 2000);
    }
    if (img.complete && img.naturalWidth !== 0 && typeof img.decode === "function") {
      await withTimeout(img.decode(), 2000);
    }
  }

  function waitForPaint() {
    return new Promise(function(resolve) {
      let finished = false;
      const finish = function() {
        if (!finished) {
          finished = true;
          clearTimeout(timer);
          resolve();
        }
      };
      const timer = setTimeout(finish, 32);
      try {
        requestAnimationFrame(function() {
          requestAnimationFrame(finish);
        });
      } catch (_) {
        finish();
      }
    });
  }

  seekTimelines();
  await runFrameHooks();
  seekTimelines();

  const waits = [];
  for (const el of document.querySelectorAll(".clip")) {
    const isActive = activeFor(el);
    el.style.display = isActive ? "" : "none";
    if (el.dataset.trackIndex !== undefined) {
      el.style.zIndex = String(Number(el.dataset.trackIndex) || 0);
    }

    const tagName = el.tagName.toLowerCase();
    if (tagName === "video" || tagName === "audio") {
      const start = Number(el.dataset.start) || 0;
      const localTime = Math.max(0, cetusTime - start);
      if (el.dataset.volume !== undefined) {
        const volume = Number(el.dataset.volume);
        if (Number.isFinite(volume)) {
          el.volume = Math.max(0, Math.min(1, volume));
        }
      }
      if (isActive) {
        waits.push(waitForSeek(el, localTime));
      }
    } else if (tagName === "img" && isActive) {
      waits.push(waitForImage(el));
    }
  }

  seekWebAnimations();
  if (document.fonts && document.fonts.ready) {
    waits.push(withTimeout(document.fonts.ready, 2000));
  }
  await Promise.all(waits);
  seekWebAnimations();
  document.body.getBoundingClientRect();
  await waitForPaint();
  return true;
})()`
}

type frameCache struct {
	dir    string
	resume bool
}

type frameCacheManifest struct {
	Version       int     `json:"version"`
	CompositionID string  `json:"composition_id"`
	Width         int     `json:"width"`
	Height        int     `json:"height"`
	FPS           int     `json:"fps"`
	Duration      float64 `json:"duration"`
	TotalFrames   int     `json:"total_frames"`
	FrameFormat   string  `json:"frame_format"`
}

const frameCacheManifestName = "config.cetus"

var frameCacheFilePattern = regexp.MustCompile(`^frame-[0-9]+\.webp$`)

func newFrameCache(opts CaptureOptions, composition *compose.Composition) (*frameCache, error) {
	dir := strings.TrimSpace(opts.FramesDir)
	if dir == "" {
		return nil, nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create frame cache directory: %w", err)
	}

	expected := manifestForComposition(composition)
	if opts.Resume {
		if err := validateExistingFrameCache(dir, expected); err != nil {
			return nil, err
		}
	} else {
		if err := clearFrameCache(dir); err != nil {
			return nil, err
		}
	}
	if err := writeFrameCacheManifest(dir, expected); err != nil {
		return nil, err
	}

	return &frameCache{dir: dir, resume: opts.Resume}, nil
}

func manifestForComposition(composition *compose.Composition) frameCacheManifest {
	return frameCacheManifest{
		Version:       1,
		CompositionID: composition.ID,
		Width:         composition.Width,
		Height:        composition.Height,
		FPS:           composition.FPS,
		Duration:      composition.Duration,
		TotalFrames:   composition.TotalFrames,
		FrameFormat:   "webp",
	}
}

func validateExistingFrameCache(dir string, expected frameCacheManifest) error {
	existing, ok, err := readFrameCacheManifest(dir)
	if err != nil {
		return err
	}
	if !ok {
		hasFrames, err := hasFrameCacheFiles(dir)
		if err != nil {
			return err
		}
		if hasFrames {
			return fmt.Errorf("frame cache manifest is missing; rerun without --resume or use an empty --frames-dir")
		}
		return nil
	}
	if !sameFrameCacheManifest(existing, expected) {
		return fmt.Errorf("frame cache manifest does not match this composition; use a different --frames-dir or rerun without --resume")
	}
	return nil
}

func hasFrameCacheFiles(dir string) (bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false, fmt.Errorf("read frame cache directory: %w", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() && frameCacheFilePattern.MatchString(entry.Name()) {
			return true, nil
		}
	}
	return false, nil
}

func sameFrameCacheManifest(a, b frameCacheManifest) bool {
	return a.Version == b.Version &&
		a.CompositionID == b.CompositionID &&
		a.Width == b.Width &&
		a.Height == b.Height &&
		a.FPS == b.FPS &&
		a.TotalFrames == b.TotalFrames &&
		math.Abs(a.Duration-b.Duration) < 0.000001
}

func readFrameCacheManifest(dir string) (frameCacheManifest, bool, error) {
	root, err := openFrameCacheRoot(dir)
	if err != nil {
		return frameCacheManifest{}, false, err
	}
	defer root.Close()

	data, err := root.ReadFile(frameCacheManifestName)
	if os.IsNotExist(err) {
		return frameCacheManifest{}, false, nil
	}
	if err != nil {
		return frameCacheManifest{}, false, fmt.Errorf("read frame cache manifest: %w", err)
	}
	var manifest frameCacheManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return frameCacheManifest{}, false, fmt.Errorf("parse frame cache manifest: %w", err)
	}
	return manifest, true, nil
}

func writeFrameCacheManifest(dir string, manifest frameCacheManifest) error {
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("encode frame cache manifest: %w", err)
	}
	data = append(data, '\n')

	root, err := openFrameCacheRoot(dir)
	if err != nil {
		return err
	}
	defer root.Close()
	if err := root.WriteFile(frameCacheManifestName, data, 0o600); err != nil {
		return fmt.Errorf("write frame cache manifest: %w", err)
	}
	return nil
}

func clearFrameCache(dir string) error {
	root, err := openFrameCacheRoot(dir)
	if err != nil {
		return err
	}
	defer root.Close()

	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read frame cache directory: %w", err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || (name != frameCacheManifestName && !frameCacheFilePattern.MatchString(name)) {
			continue
		}
		if err := root.Remove(name); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove old cached frame %q: %w", name, err)
		}
	}
	return nil
}

func (c *frameCache) read(frame int) ([]byte, bool, error) {
	root, err := openFrameCacheRoot(c.dir)
	if err != nil {
		return nil, false, err
	}
	defer root.Close()

	data, err := root.ReadFile(frameFileName(frame))
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if !isValidWebPData(data) {
		return nil, false, nil
	}
	return data, true, nil
}

func (c *frameCache) write(frame int, data []byte) error {
	if len(data) == 0 {
		return fmt.Errorf("frame data is empty")
	}
	root, err := openFrameCacheRoot(c.dir)
	if err != nil {
		return err
	}
	defer root.Close()

	name := frameFileName(frame)
	tmpName := name + ".tmp"
	if err := root.WriteFile(tmpName, data, 0o600); err != nil {
		return err
	}
	if err := root.Rename(tmpName, name); err != nil {
		_ = root.Remove(tmpName)
		return err
	}
	return nil
}

func openFrameCacheRoot(dir string) (*os.Root, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, fmt.Errorf("open frame cache root: %w", err)
	}
	return root, nil
}

func frameFileName(frame int) string {
	return fmt.Sprintf("frame-%09d.webp", frame)
}

func isValidWebPData(data []byte) bool {
	return len(data) >= 12 &&
		data[0] == 'R' && data[1] == 'I' && data[2] == 'F' && data[3] == 'F' &&
		data[8] == 'W' && data[9] == 'E' && data[10] == 'B' && data[11] == 'P'
}
