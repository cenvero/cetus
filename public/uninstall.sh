#!/bin/sh
set -eu

INSTALL_DIR="${CETUS_INSTALL_DIR:-$HOME/.local/bin}"
rm -f "$INSTALL_DIR/cetus"
echo "removed $INSTALL_DIR/cetus"

