//go:build darwin && amd64

package assets

import _ "embed"

//go:embed darwin-amd64/headless-shell.br
var headlessShellData []byte

//go:embed darwin-amd64/ffmpeg.br
var ffmpegData []byte
