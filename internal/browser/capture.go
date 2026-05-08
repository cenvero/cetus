package browser

import (
	"context"
	"fmt"
	"strconv"

	"github.com/cenvero/cetus/internal/compose"
	"github.com/cenvero/cetus/internal/encoder"
	"github.com/chromedp/cdproto/cdp"
	cdpruntime "github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

func (b *Browser) Capture(ctx context.Context, composition *compose.Composition, enc *encoder.Encoder) error {
	if b == nil {
		return fmt.Errorf("browser is nil")
	}
	if composition == nil {
		return fmt.Errorf("composition is required")
	}
	if enc == nil {
		return fmt.Errorf("encoder is required")
	}

	for frame := 0; frame < composition.TotalFrames; frame++ {
		select {
		case <-ctx.Done():
			return fmt.Errorf("capture canceled: %w", ctx.Err())
		default:
		}

		t := float64(frame) / float64(composition.FPS)
		if err := chromedp.Run(b.ctx, chromedp.Evaluate(buildSeekScript(t), nil, awaitPromise)); err != nil {
			return fmt.Errorf("seek frame %d at %.6fs: %w", frame, t, err)
		}

		png, err := beginFrame(b.ctx, t)
		if err != nil {
			return fmt.Errorf("capture frame %d at %.6fs: %w", frame, t, err)
		}
		if err := enc.WriteFrame(png); err != nil {
			return fmt.Errorf("encode frame %d: %w", frame, err)
		}
	}

	return nil
}

func awaitPromise(p *cdpruntime.EvaluateParams) *cdpruntime.EvaluateParams {
	return p.WithAwaitPromise(true)
}

func beginFrame(ctx context.Context, t float64) ([]byte, error) {
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
	if err := cdp.Execute(ctx, "HeadlessExperimental.beginFrame", params, &result); err != nil {
		return nil, fmt.Errorf("call HeadlessExperimental.beginFrame: %w", err)
	}
	if len(result.ScreenshotData) == 0 {
		return nil, fmt.Errorf("HeadlessExperimental.beginFrame returned no screenshot data")
	}
	return result.ScreenshotData, nil
}

func buildSeekScript(t float64) string {
	timeLiteral := strconv.FormatFloat(t, 'f', -1, 64)
	return `(async function() {
  const cetusTime = ` + timeLiteral + `;
  document.__cetusTime = cetusTime;

  const timelines = Array.isArray(window.__timelines) ? window.__timelines : [];
  for (const timeline of timelines) {
    if (timeline && typeof timeline.seek === "function") {
      timeline.seek(cetusTime, false);
    }
  }

  function activeFor(el) {
    const start = Number(el.dataset.start);
    const duration = Number(el.dataset.duration);
    return Number.isFinite(start) && Number.isFinite(duration) &&
      cetusTime >= start && cetusTime < start + duration;
  }

  function waitForSeek(media, targetTime) {
    return new Promise(function(resolve) {
      if (!Number.isFinite(targetTime) || targetTime < 0) {
        resolve();
        return;
      }

      const done = function() {
        clearTimeout(timer);
        media.removeEventListener("seeked", done);
        media.removeEventListener("error", done);
        resolve();
      };
      const timer = setTimeout(done, 2000);
      media.addEventListener("seeked", done, { once: true });
      media.addEventListener("error", done, { once: true });

      try {
        if (Math.abs(media.currentTime - targetTime) < 0.001) {
          done();
          return;
        }
        media.currentTime = targetTime;
      } catch (_) {
        done();
      }
    });
  }

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
    }
  }

  await Promise.all(waits);
  document.body.getBoundingClientRect();
  return true;
})()`
}
