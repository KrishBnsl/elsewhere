#!/bin/sh
# Builds the Apple Silicon download that install.sh fetches: elsewhere and dhunn (Melisma) in
# one archive, with its checksum, and publishes them as a GitHub release.
#
#   scripts/release.sh v0.1.0            build and publish
#   scripts/release.sh v0.1.0 --dry-run  build only (into dist/)
#
# MELISMA_DIR is a Melisma checkout (default: ../dhunn next to this repository).

set -eu

tag="${1:?usage: scripts/release.sh <tag> [--dry-run]}"
dry="${2:-}"
here="$(cd "$(dirname "$0")/.." && pwd)"
melisma="${MELISMA_DIR:-$here/../dhunn}"
out="$here/dist"
asset="elsewhere-macos-arm64.tar.gz"

[ "$(uname -s)-$(uname -m)" = "Darwin-arm64" ] || { echo "build this on an Apple Silicon Mac" >&2; exit 1; }
[ -f "$melisma/Cargo.toml" ] || { echo "no Melisma checkout at $melisma (set MELISMA_DIR)" >&2; exit 1; }

rm -rf "$out" && mkdir -p "$out/pkg"
echo "▸ Elsewhere"
(cd "$here" && go test ./... >/dev/null && go build -trimpath -ldflags='-s -w' -o "$out/pkg/elsewhere" .)
echo "▸ Melisma"
(cd "$melisma" && cargo build --release --locked -q -p dhunn-cli)
cp "$melisma/target/release/dhunn" "$out/pkg/dhunn"
strip "$out/pkg/dhunn" 2>/dev/null || true
codesign --force --sign - "$out/pkg/dhunn" "$out/pkg/elsewhere"
tar -czf "$out/$asset" -C "$out/pkg" elsewhere dhunn
(cd "$out" && shasum -a 256 "$asset" >"$asset.sha256")
echo "▸ $out/$asset ($(du -h "$out/$asset" | cut -f1))"

if [ "$dry" = "--dry-run" ]; then
    exit 0
fi
melisma_rev="$(git -C "$melisma" rev-parse --short HEAD)"
gh release create "$tag" "$out/$asset" "$out/$asset.sha256" --repo KrishBnsl/elsewhere \
    --title "Elsewhere $tag" \
    --notes "Elsewhere and Melisma (dhunn engine, Melisma @ $melisma_rev) for Apple Silicon Macs.

Install: \`curl -fsSL https://raw.githubusercontent.com/KrishBnsl/elsewhere/main/install.sh | sh\`"
