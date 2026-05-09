package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

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
		png, cached, err := cachedFrame(cache, frame)
		if err != nil {
			return fmt.Errorf("read cached frame %d: %w", frame, err)
		}
		if !cached {
			if err := chromedp.Run(b.ctx, chromedp.Evaluate(buildSeekScript(frame, composition.FPS), nil, awaitPromise)); err != nil {
				return fmt.Errorf("seek frame %d at %.6fs: %w", frame, t, err)
			}

			png, err = b.captureFrame(t)
			if err != nil {
				return fmt.Errorf("capture frame %d at %.6fs: %w", frame, t, err)
			}
			if cache != nil {
				if err := cache.write(frame, png); err != nil {
					return fmt.Errorf("write cached frame %d: %w", frame, err)
				}
			}
		}
		if err := enc.WriteFrame(png); err != nil {
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
			"format":  "png",
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

	png, err := page.CaptureScreenshot().
		WithFormat(page.CaptureScreenshotFormatPng).
		WithFromSurface(true).
		Do(targetCtx)
	if err != nil {
		return nil, fmt.Errorf("capture page screenshot: %w", err)
	}
	if len(png) == 0 {
		return nil, fmt.Errorf("Page.captureScreenshot returned no screenshot data")
	}
	return png, nil
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
}

const frameCacheManifestName = "cetus-frames.json"

var frameCacheFilePattern = regexp.MustCompile(`^frame-[0-9]+\.png$`)

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
	data, err := os.ReadFile(filepath.Join(dir, frameCacheManifestName))
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
	if err := os.WriteFile(filepath.Join(dir, frameCacheManifestName), data, 0o600); err != nil {
		return fmt.Errorf("write frame cache manifest: %w", err)
	}
	return nil
}

func clearFrameCache(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read frame cache directory: %w", err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || (name != frameCacheManifestName && !frameCacheFilePattern.MatchString(name)) {
			continue
		}
		if err := os.Remove(filepath.Join(dir, name)); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove old cached frame %q: %w", name, err)
		}
	}
	return nil
}

func (c *frameCache) read(frame int) ([]byte, bool, error) {
	path := c.framePath(frame)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if len(data) == 0 {
		return nil, false, nil
	}
	return data, true, nil
}

func (c *frameCache) write(frame int, png []byte) error {
	if len(png) == 0 {
		return fmt.Errorf("frame PNG is empty")
	}
	path := c.framePath(frame)
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, png, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return nil
}

func (c *frameCache) framePath(frame int) string {
	return filepath.Join(c.dir, fmt.Sprintf("frame-%06d.png", frame))
}
