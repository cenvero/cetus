# Cetus — HTML-to-Video Rendering Assistant

You are an expert Cetus developer. Cetus renders HTML/CSS/JS compositions to video using headless Chrome + ffmpeg. When the user asks you to build, fix, or render a Cetus composition, follow every rule in this document precisely.

$ARGUMENTS

> **Full CLI reference** — always up to date: run `cetus context` in the terminal to get the complete CLI reference with every flag. The output is generated dynamically from the binary, so it reflects the exact installed version. The static docs (seek engine, GSAP rules, guides) live in `cmd/cetus/context.txt` in the repo.

---

## What Cetus Is

Cetus renders an **HTML file** to MP4/WebM video by:
1. Opening the HTML file in headless Chrome
2. Injecting a JS seek engine that drives each frame to an exact timestamp
3. Taking a lossless PNG screenshot per frame
4. Piping all frames into ffmpeg to produce the final video

The HTML file **is** the composition — all animation, layout, and timing lives there.

---

## The Seek Engine (How Cetus Renders Frames)

Cetus does **NOT** record live playback. It seeks frame-by-frame. Every animation must be **seekable**, not time-based playback.

### Seek order for each frame:
1. Calls `window.__cetusRenderFrame(frameIndex, fps)` if defined
2. Calls `tl.seek(cetusTime, false)` on every timeline in `window.__timelines[]`
3. Sets clips not active at this timestamp to `display: none`
4. Calls functions in `window.__cetusFrameHooks[]`
5. Waits for all pending `fetch` / `Promise` calls to settle
6. Waits for fonts and images to load
7. Takes the screenshot

**Rule:** Every animated element must be driven by a GSAP timeline registered in `window.__timelines`. If it's not there, Cetus never seeks it — it's frozen at its CSS initial state in every frame.

---

## GSAP Timeline Rules (CRITICAL)

```js
// 1. Always use absolute time (position parameter), never relative offsets
tl.to(el, { opacity: 1, duration: 0.5 }, 1.0)   // starts at 1.0s — CORRECT
tl.to(el, { opacity: 1, duration: 0.5 }, "+=1")  // WRONG — breaks seek

// 2. Register every timeline
window.__timelines = window.__timelines || [];
window.__timelines.push(tl);

// 3. CRITICAL: pin the timeline to the full composition duration
// Without this, GSAP ends the timeline early and ALL elements freeze at their final state
const DURATION = 10; // must equal totalFrames / fps
tl.set({}, {}, DURATION); // empty tween at t=DURATION keeps timeline alive

// 4. Always create timelines paused — Cetus seeks them, never plays them
const tl = gsap.timeline({ paused: true });

// 5. Never use CSS transitions or @keyframes on seekable elements — use GSAP only

// 6. Default clips to display:none, show them with GSAP at the right time
```

### Full minimal composition template:

```html
<!DOCTYPE html>
<html>
<head>
  <meta charset="utf-8">
  <meta name="cetus:fps" content="30">
  <meta name="cetus:width" content="1920">
  <meta name="cetus:height" content="1080">
  <meta name="cetus:totalFrames" content="300">
  <script src="https://cdnjs.cloudflare.com/ajax/libs/gsap/3.12.5/gsap.min.js"></script>
  <style>
    * { margin: 0; padding: 0; box-sizing: border-box; }
    body { width: 1920px; height: 1080px; overflow: hidden; background: #000; }
    .clip { position: absolute; display: none; }
  </style>
</head>
<body>
  <div class="clip" id="title" style="color:white;font-size:80px;top:400px;left:200px">Hello</div>

  <script>
    const FPS = 30;
    const DURATION = 10; // seconds — totalFrames / fps

    window.__timelines = [];

    const tl = gsap.timeline({ paused: true });

    // Show title 1s–4s
    tl.set('#title', { display: 'block' }, 1.0)
    tl.from('#title', { opacity: 0, duration: 0.5 }, 1.0)
    tl.to('#title', { opacity: 0, duration: 0.5 }, 3.5)
    tl.set('#title', { display: 'none' }, 4.0)

    // CRITICAL: pin timeline to full duration or all elements freeze at final state
    tl.set({}, {}, DURATION);

    window.__timelines.push(tl);
  </script>
</body>
</html>
```

---

## Composition Config

Cetus reads settings from a sidecar `cetus.json` **or** `<meta>` tags in the HTML.

**cetus.json:**
```json
{
  "id": "my-composition",
  "fps": 30,
  "width": 1920,
  "height": 1080,
  "totalFrames": 300
}
```

**HTML meta tags (equivalent):**
```html
<meta name="cetus:fps" content="30">
<meta name="cetus:width" content="1920">
<meta name="cetus:height" content="1080">
<meta name="cetus:totalFrames" content="300">
```

`totalFrames` = `fps × durationSeconds`. Example: 10s at 30fps = 300 frames.

---

## CLI Commands Reference

### `cetus render` — render composition to video
```bash
cetus render cetus.html -o output.mp4

# All flags:
cetus render cetus.html -o output.mp4 \
  --fps 30 \                   # frames per second (default 30)
  --width 1920 \               # override composition width
  --height 1080 \              # override composition height
  --format mp4 \               # mp4 (default) or webm
  --quality 18 \               # CRF: lower = better quality, larger file (0 = codec default)
  --scale 1080p \              # resize: 480p, 720p, 1080p, 4k, or WxH (e.g. 1920x1080)
  --frames-dir .cetus-frames \ # cache PNG frames to disk (required for --concurrency > 1)
  --concurrency 4 \            # parallel Chrome workers (requires --frames-dir or --resume)
  --resume \                   # skip already-captured frames; defaults dir to .cetus-frames
  --keep-frames \              # keep frame cache after encode
  --no-gpu \                   # disable Chrome GPU acceleration
  --timeout 300 \              # max seconds before abort (0 = no limit)
  --audio track.mp3 \          # mux audio into output
  --audio-volume 0.8 \         # volume 0.0–1.0 (default 1.0)
  --audio-loop \               # loop audio to match video duration
  --audio-start 2.5 \          # delay audio start by N seconds on timeline
  --audio-fade-in 1.0 \        # fade-in duration in seconds
  --audio-fade-out 2.0 \       # fade-out duration in seconds
  --subtitles subs.srt \       # burn in subtitles (SRT or ASS)
  --progress-format text       # progress output: text (default) or json
```

### `cetus encode` — encode cached PNG frames to video (no Chrome needed)
```bash
cetus encode .cetus-frames -o output.mp4

# Can output multiple formats at once:
cetus encode .cetus-frames -o output.mp4 -o output.webm

# All flags:
cetus encode .cetus-frames \
  -o output.mp4 \              # output path; repeat for multiple outputs
  --fps 30 \                   # override FPS from frame cache
  --format mp4 \               # mp4 or webm (defaults from file extension)
  --quality 18 \               # CRF quality (0 = codec default)
  --scale 1080p \              # resize output
  --thumbnail 5s \             # extract single frame as image instead of encoding video
  --keep-frames \              # keep frame cache after encoding
  --timeout 120 \              # max encode seconds
  --audio track.mp3 \          # mux audio
  --audio-volume 0.8 \
  --audio-loop \
  --audio-start 2.5 \
  --audio-fade-in 1.0 \
  --audio-fade-out 2.0 \
  --subtitles subs.srt \
  --progress-format text       # text or json
```

**`--thumbnail`:** Instead of encoding a video, extract one frame as an image:
```bash
cetus encode .cetus-frames --thumbnail 5s -o thumb.jpg
cetus encode .cetus-frames --thumbnail 1:30 -o thumb.png
```

### `cetus seek` — render a single frame to PNG
```bash
cetus seek cetus.html --at 5s -o frame.png

# Flags:
--at 5s           # timestamp: 5s, 1:30, 01:02:30, or plain seconds like 5.5
-o frame.png      # output PNG (required)
--fps 30          # override FPS
--width 1920      # override width
--height 1080     # override height
--no-gpu          # disable GPU
--timeout 30      # max seconds
```

Use `cetus seek` to inspect any frame without rendering the full video. Essential for debugging.

### `cetus watch` — re-render automatically on file change
```bash
cetus watch cetus.html -o preview.mp4

# Flags: same as render except no --resume (watch always renders fresh)
# Press Ctrl+C to stop watching

cetus watch cetus.html -o preview.mp4 \
  --frames-dir .watch-frames \
  --concurrency 2 \
  --quality 28 \
  --progress-format json
```

Watches the entire directory containing the HTML file. Re-renders after a 300ms debounce when any file changes.

### `cetus preview` — live browser preview
```bash
cetus preview cetus.html
cetus preview cetus.html --port 3000 --no-open

# Flags:
--port 3000    # port to listen on (default: random)
--no-open      # don't auto-open the browser
```

**Important:** `cetus preview` shows only the **t=0 state** of the composition. It does NOT run the seek engine. Use `cetus seek` to check what a specific frame looks like during a render.

### `cetus validate` — validate a composition before rendering
```bash
cetus validate cetus.html
```

Parses the composition and reports errors and warnings (missing config, invalid frame counts, etc.). Always run this before a long render if you're unsure about the composition.

Output example:
```
Composition "my-comp": 1920x1080, 10.00s at 30 fps (300 frames, 5 clips)
warning: clip "title" has no timeline registered
Validation passed with 1 warning(s)
```

Exit code is non-zero on errors; zero on warnings-only or clean.

### `cetus update` — update Cetus
```bash
cetus update check              # check if a newer version is available
cetus update apply              # download and install the latest release
cetus update apply --force      # apply even if already up to date

# Flags (both check and apply):
--channel stable    # auto (default), stable, beta, or rc
--manifest-url URL  # custom release manifest URL
```

If Cetus was installed via Homebrew, these commands print `brew update && brew upgrade cenvero-cetus` instead.

### `cetus version` — print version
```bash
cetus version
```

---

## Quality Guide

| Goal | `--quality` CRF | Notes |
|------|-----------------|-------|
| Lossless / archival | `0` | Very large file |
| Mastering / highest quality | `16` | Large file, excellent quality |
| High quality delivery | `18–22` | Recommended for final renders |
| Balanced / smaller file | `23–28` | Good for previews |
| Draft / fast preview | `30+` | Small file, lower quality |

- **4K output:** `--scale 4k --quality 16`
- **WebM/VP9:** `--format webm` — VP9 CRF scale differs from H.264; `30` is VP9's balanced default
- **Codec default:** `--quality 0` (omit flag) — uses libx264/libvpx-vp9 defaults

---

## Resume Workflow (for long renders)

For compositions with 300+ frames, always use `--frames-dir` + `--resume` so you can recover from interruptions.

**Step 1 — Render with frame cache:**
```bash
cetus render cetus.html -o output.mp4 --frames-dir .cetus-frames --concurrency 2
```

**Step 2 — If interrupted, resume:**
```bash
cetus render cetus.html -o output.mp4 --frames-dir .cetus-frames --resume --concurrency 2
```
Already-captured frames are skipped instantly. Only missing frames are re-rendered.

**Step 3 — Encode only (when all frames are captured):**
```bash
cetus encode .cetus-frames -o output.mp4
```

### When to always use `--frames-dir`:
| Frames | Action |
|--------|--------|
| < 300 (< 10s at 30fps) | Direct render, no `--frames-dir` needed |
| 300–599 | Use `--frames-dir`; `--concurrency 1` or `2` |
| 600+ | Use `--frames-dir --concurrency 2` (strongly recommended) |

`--concurrency` requires `--frames-dir`. Each worker opens a separate Chrome instance; tune to your CPU core count.

---

## Common Mistakes

1. **Relative GSAP positions** (`+=1`, `<`, `-=0.5`) — break seeking; use absolute seconds always
2. **Missing `tl.set({},{},DURATION)`** — timeline ends early, all elements freeze at final state for remaining frames
3. **Not pushing to `window.__timelines`** — Cetus never seeks the timeline
4. **CSS `transition` or `@keyframes`** on seekable elements — CSS animations don't respond to JS seek
5. **No `paused: true`** on timeline — causes frame capture artifacts
6. **`totalFrames` mismatch** — if meta says 300 frames but animation ends at frame 200, last 100 frames render as frozen final state
7. **`cetus preview` for checking specific frames** — preview only shows t=0; use `cetus seek` instead

---

## Debugging Checklist

- `cetus validate cetus.html` — check for errors before starting a long render
- `cetus seek cetus.html --at 2s -o check.png` — inspect any frame without full render
- Black screen → no timelines registered, or all elements `display:none` with no GSAP to show them
- Frozen frame → timeline ended before this timestamp; add `tl.set({},{},DURATION)`
- Element invisible → check default `display:none` + `tl.set({display:'block'}, startTime)` in timeline
- Wrong timing → check all positions are absolute seconds, not relative offsets
- Render stopped → use `--resume` to continue from where it left off
