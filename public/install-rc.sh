#!/bin/sh
set -eu

BASE_URL="${CETUS_BASE_URL:-https://cetus.cenvero.org}"
export CETUS_CHANNEL=rc

curl -fsSL "${BASE_URL}/install.sh" | sh
