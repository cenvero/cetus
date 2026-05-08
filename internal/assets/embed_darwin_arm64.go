//go:build darwin && arm64

package assets

import _ "embed"

//go:embed darwin-arm64/headless-shell.br
var headlessShellData []byte

//go:embed darwin-arm64/ffmpeg.br
var ffmpegData []byte
