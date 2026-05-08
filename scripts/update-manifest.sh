#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 1 ]]; then
  echo "usage: $0 <version> [dist-dir]" >&2
  exit 2
fi

VERSION="$1"
DIST_DIR="${2:-dist}"
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
MANIFEST="${CETUS_MANIFEST_PATH:-${ROOT_DIR}/public/manifest.json}"
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

def release_channel(version):
    base = version[1:] if version.startswith("v") else version
    if "-" not in base:
        return "stable"
    prerelease = base.split("-", 1)[1].split("+", 1)[0].lower()
    if prerelease == "beta" or prerelease.startswith("beta."):
        return "beta"
    if prerelease == "rc" or prerelease.startswith("rc."):
        return "rc"
    raise SystemExit(f"unsupported prerelease channel in {version}; use beta or rc")

channel_name = release_channel(version)
channel = manifest.setdefault("channels", {}).setdefault(channel_name, {})
channel["version"] = version
channel["release_date"] = now
channel.setdefault("min_supported", version)
channel["release_notes_url"] = f"https://github.com/cenvero/cetus/releases/tag/{version}"
history = channel.setdefault("history", [])
if version not in history:
    history.insert(0, version)

binaries = manifest.setdefault("binaries", {}).setdefault(version, {})
expected_platforms = {"linux-amd64", "darwin-amd64", "darwin-arm64", "windows-amd64"}
found_platforms = set()
for archive in dist.glob(f"cetus_{version}_*"):
    if archive.suffix == ".minisig" or archive.name.endswith("checksums.txt"):
        continue
    parts = archive.name.removeprefix(f"cetus_{version}_").split(".")[0].split("_")
    if len(parts) < 2:
        continue
    platform = f"{parts[0]}-{parts[1]}"
    signature = archive.with_name(archive.name + ".minisig")
    if not signature.exists():
        raise SystemExit(f"missing signature for {archive.name}")
    sha = hashlib.sha256(archive.read_bytes()).hexdigest()
    url = f"https://github.com/cenvero/cetus/releases/download/{version}/{archive.name}"
    binaries[platform] = {
        "url": url,
        "sha256": sha,
        "signature_url": url + ".minisig",
        "size": archive.stat().st_size,
    }
    found_platforms.add(platform)

missing_platforms = sorted(expected_platforms - found_platforms)
if missing_platforms:
    raise SystemExit(f"release assets missing for {version}: {', '.join(missing_platforms)}")

manifest_file.write_text(json.dumps(manifest, indent=2) + "\n")
PY

echo "updated ${MANIFEST}"
