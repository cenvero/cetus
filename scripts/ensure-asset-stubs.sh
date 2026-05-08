#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

write_stub() {
  local path="$1"
  mkdir -p "$(dirname "${path}")"
  if [[ ! -f "${path}" ]]; then
    printf 'stub\n' > "${path}"
  fi
}

write_stub "${ROOT_DIR}/internal/assets/linux-amd64/headless-shell.br"
write_stub "${ROOT_DIR}/internal/assets/linux-amd64/ffmpeg.br"
write_stub "${ROOT_DIR}/internal/assets/linux-arm64/headless-shell.br"
write_stub "${ROOT_DIR}/internal/assets/linux-arm64/ffmpeg.br"
write_stub "${ROOT_DIR}/internal/assets/darwin-amd64/headless-shell.br"
write_stub "${ROOT_DIR}/internal/assets/darwin-amd64/ffmpeg.br"
write_stub "${ROOT_DIR}/internal/assets/darwin-arm64/headless-shell.br"
write_stub "${ROOT_DIR}/internal/assets/darwin-arm64/ffmpeg.br"
write_stub "${ROOT_DIR}/internal/assets/windows-amd64/headless-shell.br"
write_stub "${ROOT_DIR}/internal/assets/windows-amd64/ffmpeg.exe.br"
