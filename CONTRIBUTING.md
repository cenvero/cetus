# Contributing

## Requirements

- Go 1.24 or newer
- `brotli`, `curl`, and `unzip` when preparing embedded assets
- `ffmpeg` and Chrome assets are bundled through `scripts/prep-assets.sh`

## Local Checks

```sh
gofmt -w $(find . -name '*.go')
go vet ./...
go test ./...
```

## Pull Requests

- Keep changes focused.
- Include tests for parser, CLI, and non-trivial behavior.
- Do not add CGO dependencies.
- Keep the CLI non-interactive and scriptable.
- Wrap errors with context using `%w`.

## Release Assets

The placeholder files under `internal/assets/*` are not usable runtime assets.
Before release builds, run `scripts/prep-assets.sh` for every target platform.

