#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 2 ]]; then
  echo "usage: $0 <goos> <goarch>" >&2
  exit 2
fi

GOOS_TARGET="$1"
GOARCH_TARGET="$2"
CHROME_VERSION="${CHROME_VERSION:-133.0.6943.98}"
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

if ! command -v brotli >/dev/null 2>&1; then
  echo "missing required command: brotli" >&2
  exit 1
fi

chrome_platform() {
  case "$GOOS_TARGET/$GOARCH_TARGET" in
    linux/amd64) echo "linux64" ;;
    linux/arm64) echo "linux-arm64" ;;
    darwin/amd64) echo "mac-x64" ;;
    darwin/arm64) echo "mac-arm64" ;;
    windows/amd64) echo "win64" ;;
    *) echo "unsupported target: $GOOS_TARGET/$GOARCH_TARGET" >&2; exit 1 ;;
  esac
}

ffmpeg_url() {
  case "$GOOS_TARGET/$GOARCH_TARGET" in
    linux/amd64) echo "https://johnvansickle.com/ffmpeg/releases/ffmpeg-release-amd64-static.tar.xz" ;;
    linux/arm64) echo "https://johnvansickle.com/ffmpeg/releases/ffmpeg-release-arm64-static.tar.xz" ;;
    darwin/amd64) echo "https://evermeet.cx/ffmpeg/getrelease/zip" ;;
    darwin/arm64) echo "https://www.osxexperts.net/ffmpeg71arm.zip" ;;
    windows/amd64) echo "https://www.gyan.dev/ffmpeg/builds/ffmpeg-release-essentials.zip" ;;
    *) echo "unsupported target: $GOOS_TARGET/$GOARCH_TARGET" >&2; exit 1 ;;
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

CHROME_PLATFORM="$(chrome_platform)"
CHROME_ZIP="${WORK_DIR}/chrome-headless-shell.zip"
CHROME_URL="https://storage.googleapis.com/chrome-for-testing-public/${CHROME_VERSION}/${CHROME_PLATFORM}/chrome-headless-shell-${CHROME_PLATFORM}.zip"

echo "downloading chrome-headless-shell ${CHROME_VERSION} for ${CHROME_PLATFORM}"
download "${CHROME_URL}" "${CHROME_ZIP}"
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
brotli -f -q 11 "${CHROME_BIN}" -o "${PLATFORM_DIR}/headless-shell.br"

FFMPEG_URL="$(ffmpeg_url)"
FFMPEG_ARCHIVE="${WORK_DIR}/ffmpeg.archive"
echo "downloading ffmpeg for ${GOOS_TARGET}/${GOARCH_TARGET}"
download "${FFMPEG_URL}" "${FFMPEG_ARCHIVE}"
mkdir -p "${WORK_DIR}/ffmpeg"

case "${GOOS_TARGET}" in
  linux)
    tar -xJf "${FFMPEG_ARCHIVE}" -C "${WORK_DIR}/ffmpeg"
    FFMPEG_BIN="$(find "${WORK_DIR}/ffmpeg" -type f -name ffmpeg | head -n 1)"
    if [[ -z "${FFMPEG_BIN}" ]]; then
      echo "ffmpeg binary not found in archive" >&2
      exit 1
    fi
    brotli -f -q 11 "${FFMPEG_BIN}" -o "${PLATFORM_DIR}/ffmpeg.br"
    ;;
  darwin)
    unzip -q "${FFMPEG_ARCHIVE}" -d "${WORK_DIR}/ffmpeg"
    FFMPEG_BIN="$(find "${WORK_DIR}/ffmpeg" -type f -name ffmpeg | head -n 1)"
    if [[ -z "${FFMPEG_BIN}" ]]; then
      echo "ffmpeg binary not found in archive" >&2
      exit 1
    fi
    brotli -f -q 11 "${FFMPEG_BIN}" -o "${PLATFORM_DIR}/ffmpeg.br"
    ;;
  windows)
    unzip -q "${FFMPEG_ARCHIVE}" -d "${WORK_DIR}/ffmpeg"
    FFMPEG_BIN="$(find "${WORK_DIR}/ffmpeg" -type f -name ffmpeg.exe | head -n 1)"
    if [[ -z "${FFMPEG_BIN}" ]]; then
      echo "ffmpeg.exe binary not found in archive" >&2
      exit 1
    fi
    brotli -f -q 11 "${FFMPEG_BIN}" -o "${PLATFORM_DIR}/ffmpeg.exe.br"
    ;;
esac

echo "prepared embedded assets in ${PLATFORM_DIR}"
