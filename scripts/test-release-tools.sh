#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

"${ROOT_DIR}/scripts/validate-release-manifest.sh"
"${ROOT_DIR}/scripts/validate-signing-assets.sh"

WORK_DIR="$(mktemp -d)"
cleanup() {
  rm -rf "${WORK_DIR}"
}
trap cleanup EXIT

write_manifest() {
  printf '{"generated_at":"2026-01-01T00:00:00Z","channels":{},"binaries":{}}\n' > "$1"
}

write_fake_release_assets() {
  local dist_dir="$1"
  mkdir -p "${dist_dir}"
  for platform in linux_amd64 darwin_amd64 darwin_arm64 windows_amd64; do
    local extension="tar.gz"
    if [[ "${platform}" == windows_* ]]; then
      extension="zip"
    fi
    local archive="${dist_dir}/cetus_v9.9.9_${platform}.${extension}"
    printf 'fake archive for %s\n' "${platform}" > "${archive}"
    printf 'fake signature for %s\n' "${platform}" > "${archive}.minisig"
  done
}

MANIFEST_OK="${WORK_DIR}/manifest-ok.json"
DIST_OK="${WORK_DIR}/dist-ok"
write_manifest "${MANIFEST_OK}"
write_fake_release_assets "${DIST_OK}"
CETUS_MANIFEST_PATH="${MANIFEST_OK}" "${ROOT_DIR}/scripts/update-manifest.sh" v9.9.9 "${DIST_OK}" >/dev/null
python3 - "${MANIFEST_OK}" <<'PY'
import json
import pathlib
import sys

manifest = json.loads(pathlib.Path(sys.argv[1]).read_text())
platforms = set(manifest["binaries"]["v9.9.9"])
expected = {"linux-amd64", "darwin-amd64", "darwin-arm64", "windows-amd64"}
if manifest["channels"]["stable"]["version"] != "v9.9.9":
    raise SystemExit("stable channel was not updated")
if platforms != expected:
    raise SystemExit(f"unexpected release platforms: {sorted(platforms)}")
PY

MANIFEST_MISSING="${WORK_DIR}/manifest-missing.json"
DIST_MISSING="${WORK_DIR}/dist-missing"
write_manifest "${MANIFEST_MISSING}"
mkdir -p "${DIST_MISSING}"
printf 'fake archive\n' > "${DIST_MISSING}/cetus_v9.9.9_linux_amd64.tar.gz"
printf 'fake signature\n' > "${DIST_MISSING}/cetus_v9.9.9_linux_amd64.tar.gz.minisig"
if CETUS_MANIFEST_PATH="${MANIFEST_MISSING}" "${ROOT_DIR}/scripts/update-manifest.sh" v9.9.9 "${DIST_MISSING}" >/dev/null 2>&1; then
  echo "update-manifest should fail when release platforms are missing" >&2
  exit 1
fi

echo "release tool checks ok"
