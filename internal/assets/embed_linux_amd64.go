//go:build linux && amd64

package assets

import _ "embed"

//go:embed linux-amd64/headless-shell.br
var headlessShellData []byte

//go:embed linux-amd64/ffmpeg.br
var ffmpegData []byte

