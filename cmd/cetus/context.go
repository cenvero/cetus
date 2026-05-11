package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newContextCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "context",
		Short: "Print the full Cetus AI context (GSAP rules, CLI reference, seek engine docs)",
		Long: `Prints the complete Cetus reference as markdown — useful for pasting into any AI tool
that doesn't have a Claude Code skill installed, or for piping into a file.

Examples:
  cetus context
  cetus context > context.md`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprint(cmd.OutOrStdout(), cetusAIContext)
			return nil
		},
	}
}

const cetusAIContext = `# Cetus — HTML-to-Video Rendering Reference

Cetus renders HTML/CSS/JS compositions to video using headless Chrome + ffmpeg.

## What Cetus Is

Cetus renders an HTML file to MP4/WebM video by:
1. Opening the HTML file in headless Chrome
2. Injecting a JS seek engine that drives each frame to an exact timestamp
3. Taking a lossless PNG screenshot per frame
4. Piping all frames into ffmpeg to produce the final video

The HTML file IS the composition — all animation, layout, and timing lives there.

---

## The Seek Engine

Cetus does NOT record live playback. It seeks frame-by-frame. Every animation must be seekable.

Seek order for each frame:
1. Calls window.__cetusRenderFrame(frameIndex, fps) if defined
2. Calls tl.seek(cetusTime, false) on every timeline in window.__timelines[]
3. Sets clips not active at this timestamp to display: none
4. Calls functions in window.__cetusFrameHooks[]
5. Waits for pending fetch / Promise calls to settle
6. Waits for fonts and images to load
7. Takes the screenshot

Rule: Every animated element must be driven by a GSAP timeline registered in window.__timelines.
If it is not there, Cetus never seeks it — it is frozen at its CSS initial state in every frame.

---

## GSAP Timeline Rules (CRITICAL)

  // 1. Always use absolute time, never relative offsets
  tl.to(el, { opacity: 1, duration: 0.5 }, 1.0)   // CORRECT
  tl.to(el, { opacity: 1, duration: 0.5 }, "+=1")  // WRONG — breaks seek

  // 2. Register every timeline
  window.__timelines = window.__timelines || [];
  window.__timelines.push(tl);

  // 3. CRITICAL: pin the timeline to the full composition duration
  // Without this, GSAP ends the timeline early and ALL elements freeze at their final state
  const DURATION = 10; // must equal totalFrames / fps
  tl.set({}, {}, DURATION);

  // 4. Always create timelines paused
  const tl = gsap.timeline({ paused: true });

  // 5. Never use CSS transitions or @keyframes on seekable elements — use GSAP only

  // 6. Default clips to display:none, show them with GSAP at the right time

---

## Composition Config

cetus.json:
  { "id": "my-comp", "fps": 30, "width": 1920, "height": 1080, "totalFrames": 300 }

HTML meta tags (equivalent):
  <meta name="cetus:fps" content="30">
  <meta name="cetus:width" content="1920">
  <meta name="cetus:height" content="1080">
  <meta name="cetus:totalFrames" content="300">

totalFrames = fps x durationSeconds. Example: 10s at 30fps = 300 frames.

---

## CLI Commands

cetus render cetus.html -o output.mp4
  --fps 30            frames per second (default 30)
  --width / --height  override composition dimensions
  --format mp4        mp4 (default) or webm
  --quality 18        CRF: lower = better quality, larger file (0 = codec default)
  --scale 1080p       resize: 480p, 720p, 1080p, 4k, or WxH
  --frames-dir DIR    cache PNG frames to disk (required for --concurrency > 1)
  --concurrency N     parallel Chrome workers (requires --frames-dir or --resume)
  --resume            skip already-captured frames
  --keep-frames       keep frame cache after encode
  --no-gpu            disable Chrome GPU
  --timeout N         max seconds (0 = no limit)
  --audio FILE        mux audio into output
  --audio-volume 0.8  volume 0.0-1.0
  --audio-loop        loop audio to match video duration
  --audio-start 2.5   delay audio start by N seconds
  --audio-fade-in N   fade-in duration in seconds
  --audio-fade-out N  fade-out duration in seconds
  --subtitles FILE    burn in subtitles (SRT or ASS)
  --progress-format   text (default) or json

cetus encode .cetus-frames -o output.mp4
  -o can be repeated for multiple outputs: -o out.mp4 -o out.webm
  --fps / --format / --quality / --scale / --keep-frames / --timeout
  --audio / --audio-volume / --audio-loop / --audio-start / --audio-fade-in / --audio-fade-out
  --subtitles / --progress-format
  --thumbnail 5s      extract a single frame as an image instead of encoding video

cetus seek cetus.html --at 5s -o frame.png
  --at 5s             timestamp: 5s, 1:30, 01:02:30
  -o frame.png        output PNG (required)
  --fps / --width / --height / --no-gpu / --timeout

cetus watch cetus.html -o preview.mp4
  Same flags as render except no --resume (always renders fresh)
  Press Ctrl+C to stop

cetus preview cetus.html
  --port 3000   port (default: random)
  --no-open     do not auto-open browser
  NOTE: preview shows t=0 state only. Does NOT run seek engine.

cetus validate cetus.html
  Parses composition and reports errors and warnings before rendering.

cetus update check
cetus update apply [--force]
  --channel stable / beta / rc / auto

cetus version
cetus context     print this reference

---

## Quality Guide

  CRF 0       Lossless / archival (very large file)
  CRF 16      Mastering / highest quality
  CRF 18-22   High quality delivery (recommended)
  CRF 23-28   Balanced / smaller file
  CRF 30+     Draft preview

4K output: --scale 4k --quality 16
WebM/VP9:  --format webm (VP9 default balanced CRF is 30)

---

## Resume Workflow

Step 1 - Render with frame cache:
  cetus render cetus.html -o output.mp4 --frames-dir .cetus-frames --concurrency 2

Step 2 - If interrupted, resume:
  cetus render cetus.html -o output.mp4 --frames-dir .cetus-frames --resume --concurrency 2

Step 3 - Encode only when all frames are captured:
  cetus encode .cetus-frames -o output.mp4

When to use --frames-dir:
  < 300 frames   direct render, no --frames-dir needed
  300-599        use --frames-dir, --concurrency 1 or 2
  600+           use --frames-dir --concurrency 2 (strongly recommended)

---

## Common Mistakes

1. Relative GSAP positions (+=1, <, -=0.5) — use absolute seconds always
2. Missing tl.set({},{},DURATION) — timeline ends early, elements freeze at final state
3. Not pushing to window.__timelines — timeline is never seeked
4. CSS transition or @keyframes on seekable elements — does not respond to JS seek
5. No paused: true on timeline — frame capture artifacts
6. totalFrames mismatch — last N frames render as frozen final state
7. Using cetus preview to check specific frames — preview only shows t=0; use cetus seek

---

## Debugging Checklist

- cetus validate cetus.html — check before long renders
- cetus seek cetus.html --at 2s -o check.png — inspect any frame without full render
- Black screen → no timelines registered, or all elements display:none
- Frozen frame → missing tl.set({},{},DURATION)
- Element invisible → missing tl.set({display:'block'}, startTime)
- Wrong timing → check all positions are absolute seconds
- Render stopped → use --resume to continue
`
