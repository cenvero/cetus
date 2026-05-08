//go:build linux && arm64

package assets

import _ "embed"

//go:embed linux-arm64/headless-shell.br
var headlessShellData []byte

//go:embed linux-arm64/ffmpeg.br
var ffmpegData []byte
