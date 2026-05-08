# Cetus

Cetus is a self-contained CLI tool that renders HTML compositions into video files.
It ships a platform-specific headless browser and static ffmpeg binary inside one
Go executable, extracts them once into the user cache, and renders frames
deterministically through Chrome DevTools Protocol.

## Install

```sh
curl -fsSL https://cetus.cenvero.org/install | sh
```

## Usage

```sh
cetus render cetus.html -o out.mp4
cetus render cetus.html -o out.webm

# Override HTML defaults only when needed
cetus render cetus.html -o out.mp4 --fps 60
cetus preview cetus.html
cetus version
```

`cetus.html` is the recommended default filename. Any HTML path works.

`--no-gpu` disables GPU acceleration. GPU remains enabled by default so WebGL,
Three.js, and shader-based compositions work on platforms with usable graphics
drivers.

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

GSAP timelines should be paused and registered on `window.__timelines`.

```html
<script>
  window.__timelines = window.__timelines || [];
  const tl = gsap.timeline({ paused: true });
  tl.from("#title", { opacity: 0, duration: 0.6 });
  window.__timelines.push(tl);
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
