#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
GOOS_TARGET="${GOOS:-$(go env GOOS)}"
GOARCH_TARGET="${GOARCH:-$(go env GOARCH)}"

"${ROOT_DIR}/scripts/prep-assets.sh" "${GOOS_TARGET}" "${GOARCH_TARGET}"

mkdir -p "${ROOT_DIR}/dist"
CGO_ENABLED=0 GOOS="${GOOS_TARGET}" GOARCH="${GOARCH_TARGET}" \
  go build -trimpath \
  -ldflags "-s -w -X github.com/cenvero/cetus/internal/version.Version=dev -X github.com/cenvero/cetus/internal/version.Commit=$(git rev-parse --short HEAD 2>/dev/null || echo unknown) -X github.com/cenvero/cetus/internal/version.Date=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  -o "${ROOT_DIR}/dist/cetus" \
  "${ROOT_DIR}/cmd/cetus"

echo "built ${ROOT_DIR}/dist/cetus"

