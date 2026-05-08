#!/usr/bin/env bash
set -euo pipefail

required=(
  GITHUB_TOKEN
  MINISIGN_PRIVATE_KEY
  MINISIGN_PASSWORD
)

for name in "${required[@]}"; do
  if [[ -z "${!name:-}" ]]; then
    echo "missing required environment variable: ${name}" >&2
    exit 1
  fi
done

if ! grep -q "minisign encrypted secret key" <<<"${MINISIGN_PRIVATE_KEY}"; then
  echo "MINISIGN_PRIVATE_KEY does not look like a minisign encrypted secret key" >&2
  exit 1
fi

echo "release environment ok"
