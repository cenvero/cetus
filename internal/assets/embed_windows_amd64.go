//go:build windows && amd64

package assets

import _ "embed"

//go:embed windows-amd64/headless-shell.br
var headlessShellData []byte

//go:embed windows-amd64/ffmpeg.exe.br
var ffmpegData []byte
