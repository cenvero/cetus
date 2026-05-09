#!/bin/sh
set -eu

BASE_URL="${CETUS_BASE_URL:-https://cetus.cenvero.org}"
MANIFEST_URL="${BASE_URL}/manifest.json"
CHANNEL="${CETUS_CHANNEL:-stable}"
INSTALL_DIR="${CETUS_INSTALL_DIR:-$HOME/.local/bin}"
TMP_DIR="$(mktemp -d)"

cleanup() {
  rm -rf "$TMP_DIR"
}
trap cleanup EXIT

try_install_dep() {
  tool="$1"
  if command -v apt-get >/dev/null 2>&1; then
    echo "Attempting to install $tool via apt-get..." >&2
    sudo apt-get install -y "$tool" >/dev/null 2>&1 && return 0
  fi
  if command -v dnf >/dev/null 2>&1; then
    echo "Attempting to install $tool via dnf..." >&2
    sudo dnf install -y "$tool" >/dev/null 2>&1 && return 0
  fi
  if command -v yum >/dev/null 2>&1; then
    echo "Attempting to install $tool via yum..." >&2
    sudo yum install -y "$tool" >/dev/null 2>&1 && return 0
  fi
  if command -v apk >/dev/null 2>&1; then
    echo "Attempting to install $tool via apk..." >&2
    sudo apk add "$tool" >/dev/null 2>&1 && return 0
  fi
  if command -v brew >/dev/null 2>&1; then
    echo "Attempting to install $tool via brew..." >&2
    brew install "$tool" >/dev/null 2>&1 && return 0
  fi
  return 1
}

need() {
  if command -v "$1" >/dev/null 2>&1; then
    return 0
  fi
  echo "missing required command: $1" >&2
  if try_install_dep "$1"; then
    if command -v "$1" >/dev/null 2>&1; then
      echo "installed $1 successfully" >&2
      return 0
    fi
  fi
  echo "" >&2
  echo "Could not install $1 automatically. Please install it manually:" >&2
  case "$1" in
    curl)
      echo "  Debian/Ubuntu: sudo apt-get install curl" >&2
      echo "  Fedora/RHEL:   sudo dnf install curl" >&2
      echo "  Alpine:        sudo apk add curl" >&2
      echo "  macOS:         brew install curl" >&2
      ;;
    tar)
      echo "  Debian/Ubuntu: sudo apt-get install tar" >&2
      echo "  Fedora/RHEL:   sudo dnf install tar" >&2
      echo "  Alpine:        sudo apk add tar" >&2
      echo "  macOS:         brew install gnu-tar" >&2
      ;;
  esac
  echo "" >&2
  echo "After installing $1, re-run this installer." >&2
  exit 1
}

need curl
need tar

case "$CHANNEL" in
  stable|beta|rc) ;;
  *) echo "unsupported channel: $CHANNEL" >&2; exit 1 ;;
esac

os="$(uname -s | tr '[:upper:]' '[:lower:]')"
arch="$(uname -m)"

case "$os" in
  linux) goos="linux" ;;
  darwin) goos="darwin" ;;
  *) echo "unsupported OS: $os" >&2; exit 1 ;;
esac

case "$arch" in
  x86_64|amd64) goarch="amd64" ;;
  arm64|aarch64) goarch="arm64" ;;
  *) echo "unsupported architecture: $arch" >&2; exit 1 ;;
esac

platform="${goos}-${goarch}"
manifest="$TMP_DIR/manifest.json"
curl -fsSL "$MANIFEST_URL" -o "$manifest"

version="$(awk -v channel="\"$CHANNEL\"" '
  $0 ~ channel "[[:space:]]*:" { in_channel=1 }
  in_channel && /"version"[[:space:]]*:/ {
    line=$0
    sub(/^.*"version"[[:space:]]*:[[:space:]]*"/, "", line)
    sub(/".*$/, "", line)
    print line
    exit
  }
' "$manifest")"
if [ -z "$version" ]; then
  echo "no $CHANNEL Cetus release is published yet" >&2
  exit 1
fi

url="$(awk -v version="\"$version\"" -v platform="\"$platform\"" '
  /"binaries"[[:space:]]*:/ { in_binaries=1 }
  in_binaries && $0 ~ version "[[:space:]]*:" { in_version=1 }
  in_version && $0 ~ platform "[[:space:]]*:" { in_platform=1 }
  in_platform && /"url"/ {
    line=$0
    sub(/^.*"url"[[:space:]]*:[[:space:]]*"/, "", line)
    sub(/".*$/, "", line)
    print line
    exit
  }
' "$manifest")"

sha="$(awk -v version="\"$version\"" -v platform="\"$platform\"" '
  /"binaries"[[:space:]]*:/ { in_binaries=1 }
  in_binaries && $0 ~ version "[[:space:]]*:" { in_version=1 }
  in_version && $0 ~ platform "[[:space:]]*:" { in_platform=1 }
  in_platform && /"sha256"/ {
    line=$0
    sub(/^.*"sha256"[[:space:]]*:[[:space:]]*"/, "", line)
    sub(/".*$/, "", line)
    print line
    exit
  }
' "$manifest")"

if [ -z "$url" ] || [ -z "$sha" ]; then
  echo "manifest does not contain a binary for $platform" >&2
  exit 1
fi

archive="$TMP_DIR/cetus.tar.gz"
curl -fsSL "$url" -o "$archive"

if command -v sha256sum >/dev/null 2>&1; then
  actual="$(sha256sum "$archive" | awk '{print $1}')"
else
  actual="$(shasum -a 256 "$archive" | awk '{print $1}')"
fi

if [ "$actual" != "$sha" ]; then
  echo "checksum mismatch for $url" >&2
  exit 1
fi

mkdir -p "$INSTALL_DIR"
tar -xzf "$archive" -C "$TMP_DIR"
install -m 0755 "$TMP_DIR/cetus" "$INSTALL_DIR/cetus"

echo "installed cetus $version ($CHANNEL) to $INSTALL_DIR/cetus"

# Advise user to add INSTALL_DIR to PATH if not already present
case ":${PATH}:" in
  *":$INSTALL_DIR:"*) ;;
  *)
    echo ""
    echo "NOTE: $INSTALL_DIR is not in your PATH."
    shell_rc=""
    case "${SHELL:-}" in
      */zsh)  shell_rc="$HOME/.zshrc" ;;
      */bash) shell_rc="$HOME/.bashrc" ;;
      */fish) shell_rc="$HOME/.config/fish/config.fish" ;;
    esac
    if [ -n "$shell_rc" ]; then
      echo "Add it by running:"
      echo ""
      if [ "${SHELL:-}" = "*/fish" ]; then
        echo "  fish_add_path $INSTALL_DIR"
      else
        echo "  echo 'export PATH=\"\$PATH:$INSTALL_DIR\"' >> $shell_rc"
        echo "  source $shell_rc"
      fi
    else
      echo "Add $INSTALL_DIR to your PATH to use cetus from any directory."
    fi
    echo ""
    ;;
esac
