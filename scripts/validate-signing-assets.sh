#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

if [[ ! -s "${ROOT_DIR}/public/signing.pub" ]]; then
  echo "public/signing.pub is missing or empty" >&2
  exit 1
fi

if ! grep -q "minisign public key" "${ROOT_DIR}/public/signing.pub"; then
  echo "public/signing.pub does not look like a minisign public key" >&2
  exit 1
fi

echo "signing assets ok"

