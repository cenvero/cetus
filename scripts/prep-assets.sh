#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 2 ]]; then
  echo "usage: $0 <goos> <goarch>" >&2
  exit 2
fi

GOOS_TARGET="$1"
GOARCH_TARGET="$2"
CHROME_VERSION="${CHROME_VERSION:-133.0.6943.98}"
# macOS arm64 uses the pinned GitHub release asset from eugeneware/ffmpeg-static.
# Keep this tag in sync with ffmpeg_sha256().
FFMPEG_STATIC_TAG="b6.1.1"
# Linux amd64 uses a pinned GitHub release asset from BtbN/FFmpeg-Builds,
# which is linked from FFmpeg's official download page.
BTBN_FFMPEG_TAG="autobuild-2026-05-07-13-30"
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PLATFORM_DIR="${ROOT_DIR}/internal/assets/${GOOS_TARGET}-${GOARCH_TARGET}"
WORK_DIR="$(mktemp -d)"

cleanup() {
  rm -rf "${WORK_DIR}"
}
trap cleanup EXIT

need() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing required command: $1" >&2
    exit 1
  fi
}

need curl
need unzip

log() {
  printf '[prep-assets %s/%s] %s\n' "${GOOS_TARGET}" "${GOARCH_TARGET}" "$*"
}

if ! command -v brotli >/dev/null 2>&1; then
  echo "missing required command: brotli" >&2
  exit 1
fi

chrome_platform() {
  case "$GOOS_TARGET/$GOARCH_TARGET" in
    linux/amd64) echo "linux64" ;;
    linux/arm64) echo "unsupported target: linux/arm64; Chrome for Testing does not publish chrome-headless-shell for this target" >&2; exit 1 ;;
    darwin/amd64) echo "mac-x64" ;;
    darwin/arm64) echo "mac-arm64" ;;
    windows/amd64) echo "win64" ;;
    *) echo "unsupported target: $GOOS_TARGET/$GOARCH_TARGET" >&2; exit 1 ;;
  esac
}

ffmpeg_url() {
  case "$GOOS_TARGET/$GOARCH_TARGET" in
    linux/amd64) echo "https://github.com/BtbN/FFmpeg-Builds/releases/download/${BTBN_FFMPEG_TAG}/ffmpeg-n8.1.1-linux64-gpl-8.1.tar.xz" ;;
    linux/arm64) echo "unsupported target: linux/arm64; Chrome for Testing does not publish chrome-headless-shell for this target" >&2; exit 1 ;;
    darwin/amd64) echo "https://evermeet.cx/ffmpeg/getrelease/zip" ;;
    darwin/arm64) echo "https://github.com/eugeneware/ffmpeg-static/releases/download/${FFMPEG_STATIC_TAG}/ffmpeg-darwin-arm64" ;;
    windows/amd64) echo "https://www.gyan.dev/ffmpeg/builds/ffmpeg-release-essentials.zip" ;;
    *) echo "unsupported target: $GOOS_TARGET/$GOARCH_TARGET" >&2; exit 1 ;;
  esac
}

ffmpeg_sha256() {
  case "$GOOS_TARGET/$GOARCH_TARGET" in
    linux/amd64) echo "09522a2b57e4881ddef6ca6a30803ed9e4ae8de93480f8690f98c6df858c09d6" ;;
    darwin/arm64) echo "a90e3db6a3fd35f6074b013f948b1aa45b31c6375489d39e572bea3f18336584" ;;
    *) echo "" ;;
  esac
}

mkdir -p "${PLATFORM_DIR}"

download() {
  local url="$1"
  local output="$2"
  curl --fail --show-error --location \
    --retry 3 --retry-delay 2 --connect-timeout 30 \
    "${url}" -o "${output}"
}

verify_sha256() {
  local file="$1"
  local expected="$2"
  local actual

  if [[ -z "${expected}" ]]; then
    return 0
  fi

  if command -v sha256sum >/dev/null 2>&1; then
    actual="$(sha256sum "${file}")"
  elif command -v shasum >/dev/null 2>&1; then
    actual="$(shasum -a 256 "${file}")"
  else
    echo "missing required command: sha256sum or shasum" >&2
    exit 1
  fi
  actual="${actual%% *}"

  if [[ "${actual}" != "${expected}" ]]; then
    echo "sha256 mismatch for ${file}: got ${actual}, want ${expected}" >&2
    exit 1
  fi
}

CHROME_PLATFORM="$(chrome_platform)"
CHROME_ZIP="${WORK_DIR}/chrome-headless-shell.zip"
CHROME_URL="https://storage.googleapis.com/chrome-for-testing-public/${CHROME_VERSION}/${CHROME_PLATFORM}/chrome-headless-shell-${CHROME_PLATFORM}.zip"

log "downloading chrome-headless-shell ${CHROME_VERSION} for ${CHROME_PLATFORM}"
download "${CHROME_URL}" "${CHROME_ZIP}"
log "extracting chrome archive"
unzip -q "${CHROME_ZIP}" -d "${WORK_DIR}/chrome"

if [[ "${GOOS_TARGET}" == "windows" ]]; then
  CHROME_BIN="$(find "${WORK_DIR}/chrome" -type f -name 'chrome-headless-shell.exe' | head -n 1)"
else
  CHROME_BIN="$(find "${WORK_DIR}/chrome" -type f -name 'chrome-headless-shell' | head -n 1)"
fi
if [[ -z "${CHROME_BIN}" ]]; then
  echo "chrome-headless-shell binary not found in archive" >&2
  exit 1
fi
log "compressing embedded browser with brotli -q 11"
brotli -f -q 11 "${CHROME_BIN}" -o "${PLATFORM_DIR}/headless-shell.br"
log "embedded browser ready"

FFMPEG_URL="$(ffmpeg_url)"
FFMPEG_SHA256="$(ffmpeg_sha256)"
FFMPEG_ARCHIVE="${WORK_DIR}/ffmpeg.archive"
log "downloading ffmpeg"
download "${FFMPEG_URL}" "${FFMPEG_ARCHIVE}"
log "verifying ffmpeg checksum"
verify_sha256 "${FFMPEG_ARCHIVE}" "${FFMPEG_SHA256}"
mkdir -p "${WORK_DIR}/ffmpeg"

case "${GOOS_TARGET}" in
  linux)
    log "extracting ffmpeg archive"
    tar -xJf "${FFMPEG_ARCHIVE}" -C "${WORK_DIR}/ffmpeg"
    FFMPEG_BIN="$(find "${WORK_DIR}/ffmpeg" -type f -name ffmpeg | head -n 1)"
    if [[ -z "${FFMPEG_BIN}" ]]; then
      echo "ffmpeg binary not found in archive" >&2
      exit 1
    fi
    log "compressing embedded ffmpeg with brotli -q 11"
    brotli -f -q 11 "${FFMPEG_BIN}" -o "${PLATFORM_DIR}/ffmpeg.br"
    ;;
  darwin)
    if [[ "${GOARCH_TARGET}" == "arm64" ]]; then
      log "copying ffmpeg binary"
      cp "${FFMPEG_ARCHIVE}" "${WORK_DIR}/ffmpeg/ffmpeg"
    else
      log "extracting ffmpeg archive"
      unzip -q "${FFMPEG_ARCHIVE}" -d "${WORK_DIR}/ffmpeg"
    fi
    FFMPEG_BIN="$(find "${WORK_DIR}/ffmpeg" -type f -name ffmpeg | head -n 1)"
    if [[ -z "${FFMPEG_BIN}" ]]; then
      echo "ffmpeg binary not found in archive" >&2
      exit 1
    fi
    log "compressing embedded ffmpeg with brotli -q 11"
    brotli -f -q 11 "${FFMPEG_BIN}" -o "${PLATFORM_DIR}/ffmpeg.br"
    ;;
  windows)
    log "extracting ffmpeg archive"
    unzip -q "${FFMPEG_ARCHIVE}" -d "${WORK_DIR}/ffmpeg"
    FFMPEG_BIN="$(find "${WORK_DIR}/ffmpeg" -type f -name ffmpeg.exe | head -n 1)"
    if [[ -z "${FFMPEG_BIN}" ]]; then
      echo "ffmpeg.exe binary not found in archive" >&2
      exit 1
    fi
    log "compressing embedded ffmpeg with brotli -q 11"
    brotli -f -q 11 "${FFMPEG_BIN}" -o "${PLATFORM_DIR}/ffmpeg.exe.br"
    ;;
esac

log "prepared embedded assets in ${PLATFORM_DIR}"
