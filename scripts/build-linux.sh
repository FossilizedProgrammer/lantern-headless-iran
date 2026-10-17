#!/usr/bin/env bash
#
# Build the Linux (amd64) binaries locally:
#   dist/lantern-headless-linux-amd64   — the CLI
#   dist/lantern-radiance               — the official v10 radiance engine helper
#
# Requirements: Go (>=1.24), a C toolchain (gcc/g++), and network access the
# first time so the engine's Go 1.26.x toolchain and modules can be fetched.
#
# Usage:  ./scripts/build-linux.sh [output-dir]   (default: dist)
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

GOOS_TARGET="linux"
GOARCH_TARGET="${GOARCH:-amd64}"
OUT_DIR="${1:-dist}"
MODULE="github.com/oldblocker/lantern-headless-iran"
RADIANCE_TAGS="novpn,with_gvisor,with_quic,with_dhcp,with_wireguard,with_utls,with_acme"
RADIANCE_TOOLCHAIN="${RADIANCE_TOOLCHAIN:-go1.26.9}"

asset="lantern-headless-${GOOS_TARGET}-${GOARCH_TARGET}"
engine_asset="lantern-radiance"

# ── version stamp (same scheme as the release CI) ────────────────────────────
if git describe --tags --exact-match >/dev/null 2>&1; then
  VERSION="$(git describe --tags --exact-match | sed 's/^v//')"
else
  VERSION="0.0.0-dev+$(git rev-parse --short HEAD)"
fi
REVISION_DATE="$(git log -1 --pretty=format:%ad --date=format:%Y%m%d.%H%M%S 2>/dev/null || date -u +%Y%m%d.%H%M%S)"
BUILD_DATE="$(date -u +%Y%m%d.%H%M%S)"

command -v go >/dev/null || { echo "error: go not found in PATH" >&2; exit 1; }
command -v gcc >/dev/null || { echo "error: gcc not found (needed for CGO)" >&2; exit 1; }

mkdir -p "$OUT_DIR"
OUT_DIR_ABS="$(cd "$OUT_DIR" && pwd)"
echo "Building $asset (version $VERSION) ..."

# ── CLI ──────────────────────────────────────────────────────────────────────
GOOS="$GOOS_TARGET" GOARCH="$GOARCH_TARGET" CGO_ENABLED=1 CC="${CC:-gcc}" CXX="${CXX:-g++}" \
  go build -buildvcs=false -trimpath \
  -ldflags="-s -w \
    -X ${MODULE}/shared.ApplicationVersion=${VERSION} \
    -X ${MODULE}/shared.RevisionDate=${REVISION_DATE} \
    -X ${MODULE}/shared.BuildDate=${BUILD_DATE}" \
  -o "${OUT_DIR}/${asset}" .

# ── radiance engine (separate module; needs CGO on Linux) ────────────────────
echo "Building $engine_asset (tags: $RADIANCE_TAGS, toolchain: $RADIANCE_TOOLCHAIN) ..."
(
  cd radiance-engine
  GOOS="$GOOS_TARGET" GOARCH="$GOARCH_TARGET" CGO_ENABLED=1 CC="${CC:-gcc}" CXX="${CXX:-g++}" \
    GOTOOLCHAIN="$RADIANCE_TOOLCHAIN" \
    go build -buildvcs=false -trimpath -tags "$RADIANCE_TAGS" \
    -o "${OUT_DIR_ABS}/${engine_asset}" .
)

echo
echo "Done. Output in ${OUT_DIR}/:"
ls -la "${OUT_DIR}/${asset}" "${OUT_DIR}/${engine_asset}"
echo
echo "Run:"
echo "  chmod +x ${OUT_DIR}/${asset} ${OUT_DIR}/${engine_asset}"
echo "  ${OUT_DIR}/${asset} --data-path ./data start --engine radiance \\"
echo "      --http-proxy-addr 127.0.0.1:18070 --socks-proxy-addr 127.0.0.1:18071"
echo "(keep ${OUT_DIR}/${engine_asset} next to the CLI)"
