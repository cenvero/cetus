package browser

import (
	"context"
	"fmt"
	"strconv"

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

func (b *Browser) Capture(ctx context.Context, composition *compose.Composition, enc *encoder.Encoder, progress CaptureProgressFunc) error {
	if b == nil {
		return fmt.Errorf("browser is nil")
	}
	if composition == nil {
		return fmt.Errorf("composition is required")
	}
	if enc == nil {
		return fmt.Errorf("encoder is required")
	}

	reportCaptureProgress(progress, 0, composition.TotalFrames, 0)
	for frame := 0; frame < composition.TotalFrames; frame++ {
		select {
		case <-ctx.Done():
			return fmt.Errorf("capture canceled: %w", ctx.Err())
		default:
		}

		t := float64(frame) / float64(composition.FPS)
		if err := chromedp.Run(b.ctx, chromedp.Evaluate(buildSeekScript(frame, composition.FPS), nil, awaitPromise)); err != nil {
			return fmt.Errorf("seek frame %d at %.6fs: %w", frame, t, err)
		}

		png, err := b.captureFrame(t)
		if err != nil {
			return fmt.Errorf("capture frame %d at %.6fs: %w", frame, t, err)
		}
		if err := enc.WriteFrame(png); err != nil {
			return fmt.Errorf("encode frame %d: %w", frame, err)
		}
		reportCaptureProgress(progress, frame+1, composition.TotalFrames, t)
	}

	return nil
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
