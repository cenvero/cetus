#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 1 ]]; then
  echo "usage: $0 <version> [dist-dir]" >&2
  exit 2
fi

VERSION="$1"
DIST_DIR="${2:-dist}"
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
MANIFEST="${ROOT_DIR}/public/manifest.json"
NOW="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

python3 - "$VERSION" "$DIST_DIR" "$MANIFEST" "$NOW" <<'PY'
import hashlib
import json
import pathlib
import sys

version, dist_dir, manifest_path, now = sys.argv[1:]
dist = pathlib.Path(dist_dir)
manifest_file = pathlib.Path(manifest_path)
manifest = json.loads(manifest_file.read_text())
manifest["generated_at"] = now

stable = manifest.setdefault("channels", {}).setdefault("stable", {})
stable["version"] = version
stable["release_date"] = now
stable.setdefault("min_supported", version)
stable["release_notes_url"] = f"https://github.com/cenvero/cetus/releases/tag/{version}"
history = stable.setdefault("history", [])
if version not in history:
    history.insert(0, version)

binaries = manifest.setdefault("binaries", {}).setdefault(version, {})
for archive in dist.glob(f"cetus_{version}_*"):
    if archive.suffix == ".minisig" or archive.name.endswith("checksums.txt"):
        continue
    parts = archive.name.removeprefix(f"cetus_{version}_").split(".")[0].split("_")
    if len(parts) < 2:
        continue
    platform = f"{parts[0]}-{parts[1]}"
    sha = hashlib.sha256(archive.read_bytes()).hexdigest()
    url = f"https://github.com/cenvero/cetus/releases/download/{version}/{archive.name}"
    binaries[platform] = {
        "url": url,
        "sha256": sha,
        "signature_url": url + ".minisig",
        "size": archive.stat().st_size,
    }

manifest_file.write_text(json.dumps(manifest, indent=2) + "\n")
PY

echo "updated ${MANIFEST}"

