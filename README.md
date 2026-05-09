# Cetus

Cetus is a self-contained CLI tool that renders HTML compositions into video files.
It ships a platform-specific headless browser and static ffmpeg binary inside one
Go executable, extracts them once into `~/.cenvero-cetus`, and renders frames
deterministically through Chrome DevTools Protocol.
Release builds keep the current version cache and `dev`, and remove older
versioned renderer caches during asset preparation.

## Install

```sh
curl -fsSL https://cetus.cenvero.org/install | sh
```

Prerelease testers can install a channel-specific build:

```sh
curl -fsSL https://cetus.cenvero.org/install-beta | sh
curl -fsSL https://cetus.cenvero.org/install-rc | sh
```

## Usage

```sh
cetus validate cetus.html
cetus render cetus.html -o out.mp4
cetus render cetus.html -o out.webm

# Override HTML defaults only when needed
cetus render cetus.html -o out.mp4 --fps 60
cetus render cetus.html -o out.mp4 --audio music.mp3 --audio-volume 0.7 --audio-loop
cetus render cetus.html -o out.mp4 --resume --frames-dir .cetus-frames
cetus preview cetus.html
cetus update check
cetus update apply
cetus version
```

`cetus.html` is the recommended default filename. Any HTML path works.
Self-updates stay on the installed release channel by default. Stable builds
check stable releases, beta builds check beta releases, and RC builds check RC
releases.

During render, Cetus prints stage updates and frame progress to stderr:
asset preparation, composition parsing, browser launch, frame count, elapsed
time, ETA, and final encoding. The final `Rendered ...` line remains on stdout
for scripts. Preview prints the served URL, watched directories, browser launch,
and live-reload events.

If Cetus is installed with Homebrew, updates are handled by Homebrew:

```sh
brew update && brew upgrade cenvero-cetus
```

`--no-gpu` disables GPU acceleration. GPU remains enabled by default so WebGL,
Three.js, and shader-based compositions work on platforms with usable graphics
drivers.

`cetus validate cetus.html` runs a static preflight check for composition
metadata, clip timing, missing local assets, remote URLs, obvious inline
out-of-frame positioning, and GSAP timelines that are not paused and registered.

`--audio path/to/music.mp3` muxes a local audio file into the final output. MP4
outputs encode audio as AAC, and WebM outputs encode audio as Opus. Audio can be
adjusted with `--audio-volume`, `--audio-loop`, `--audio-start`,
`--audio-fade-in`, and `--audio-fade-out`. By default Cetus does not set a total
render deadline; pass `--timeout 600` only when you want a hard cap in seconds.

Long renders can opt into a resumable frame cache with `--resume --frames-dir
.cetus-frames`. If the render fails, rerun the same command and Cetus reuses
completed frame PNGs from that directory. `--resume` without `--frames-dir` uses
`.cetus-frames` in the current directory. Frames are not saved unless
`--frames-dir` or `--resume` is provided.

## Composition Format

```html
<div id="root"
     data-composition-id="intro"
     data-width="1920"
     data-height="1080"
     data-duration="5"
     data-fps="30">
  <h1 class="clip"
      data-start="0.5"
      data-duration="4"
      data-track-index="0">
    Hello World
  </h1>
</div>
```

The root element requires:

- `data-composition-id`
- `data-width`
- `data-height`
- `data-duration`

Timed elements use `class="clip"` plus:

- `data-start`
- `data-duration`
- `data-track-index`
- optional `data-volume`

CSS and Web Animations are seeked automatically for each captured frame. GSAP
timelines should be paused and registered on `window.__timelines`.

```html
<script>
  window.__timelines = window.__timelines || [];
  const tl = gsap.timeline({ paused: true });
  tl.from("#title", { opacity: 0, duration: 0.6 });
  window.__timelines.push(tl);
</script>
```

Canvas, WebGL, Three.js, particle systems, and custom JavaScript animation
should draw from Cetus time instead of wall-clock time:

```html
<script>
  window.__cetusRenderFrame = async function(time, detail) {
    drawSceneAt(time); // detail.frame and detail.fps are also available.
  };
</script>
```

## Development

```sh
go test ./...
go run ./cmd/cetus version
```

Release builds require real embedded assets. Generate them for a target platform:

```sh
./scripts/prep-assets.sh linux amd64
```

The checked-in `.br` files are placeholders so the package structure exists in a
fresh checkout. They must be replaced before any real render build.

## License

Copyright (C) 2026 Cenvero <email@cenvero.org>
Copyright (C) 2026 Shubhdeep Singh <shubhdeep@cenvero.org> <shub@cenvero.org>

AGPL-3.0-or-later. See [LICENSE](LICENSE) and [COPYING](COPYING).
