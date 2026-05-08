#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

"${ROOT_DIR}/scripts/validate-release-manifest.sh"
"${ROOT_DIR}/scripts/validate-signing-assets.sh"

echo "release tool checks ok"

