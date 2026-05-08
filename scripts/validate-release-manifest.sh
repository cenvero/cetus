#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
MANIFEST="${ROOT_DIR}/public/manifest.json"
SCHEMA="${ROOT_DIR}/public/manifest_schema.json"

if command -v jq >/dev/null 2>&1; then
  jq empty "${MANIFEST}"
else
  python3 -m json.tool "${MANIFEST}" >/dev/null
fi

if command -v ajv >/dev/null 2>&1; then
  ajv validate -s "${SCHEMA}" -d "${MANIFEST}"
else
  echo "ajv not found; skipped JSON schema validation"
fi

echo "release manifest ok"

