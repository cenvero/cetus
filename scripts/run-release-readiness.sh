#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

"${ROOT_DIR}/scripts/ensure-asset-stubs.sh"
gofmt -w $(find "${ROOT_DIR}" -name '*.go')
go vet ./...
go test -race -cover ./...
"${ROOT_DIR}/scripts/test-release-tools.sh"

echo "release readiness ok"
