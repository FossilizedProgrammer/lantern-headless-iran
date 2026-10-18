#!/usr/bin/env bash
#
# Cross-build the Windows (amd64) binaries from Linux and bundle them exactly
# like the release CI does:
#   dist/lantern-headless-windows-amd64.zip  → lantern.exe + lantern-radiance.exe
#
# Requirements:
#   • Go (>=1.24) and network access the first time (the engine pulls the
#     Go 1.26.x toolchain and modules).
#   • MinGW-w64 for the CLI, which needs CGO:
#       Debian/Ubuntu:  sudo apt-get install -y gcc-mingw-w64-x86-64 g++-mingw-w64-x86-64
#       Fedora:         sudo dnf install mingw64-gcc mingw64-gcc-c++
#   The engine itself is pure Go on Windows, so no C toolchain is needed for it.
#
# Usage:  ./scripts/build-windows.sh [output-dir]   (default: dist)
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

GOOS_TARGET="windows"
GOARCH_TARGET="${GOARCH:-amd64}"
OUT_DIR="${1:-dist}"
MODULE="github.com/oldblocker/lantern-headless-iran"
RADIANCE_TAGS="novpn,with_gvisor,with_quic,with_dhcp,with_wireguard,with_utls,with_acme"
RADIANCE_TOOLCHAIN="${RADIANCE_TOOLCHAIN:-go1.26.9}"

CC_HOST="${CC:-x86_64-w64-mingw32-gcc}"
CXX_HOST="${CXX:-x86_64-w64-mingw32-g++}"
cli_asset="lantern.exe"
engine_asset="lantern-radiance.exe"
archive="lantern-headless-${GOOS_TARGET}-${GOARCH_TARGET}.zip"

# ── version stamp (same scheme as the release CI) ────────────────────────────
if git describe --tags --exact-match >/dev/null 2>&1; then
  VERSION="$(git describe --tags --exact-match | sed 's/^v//')"
else
  VERSION="0.0.0-dev+$(git rev-parse --short HEAD)"
fi
REVISION_DATE="$(git log -1 --pretty=format:%ad --date=format:%Y%m%d.%H%M%S 2>/dev/null || date -u +%Y%m%d.%H%M%S)"
BUILD_DATE="$(date -u +%Y%m%d.%H%M%S)"

command -v go >/dev/null || { echo "error: go not found in PATH" >&2; exit 1; }
command -v "$CC_HOST" >/dev/null || {
  echo "error: $CC_HOST not found. Install MinGW-w64 (see the header of this script)." >&2
  exit 1
}

mkdir -p "$OUT_DIR"
OUT_DIR_ABS="$(cd "$OUT_DIR" && pwd)"
echo "Building $cli_asset (version $VERSION) ..."

# ── CLI (CGO via MinGW) ──────────────────────────────────────────────────────
GOOS="$GOOS_TARGET" GOARCH="$GOARCH_TARGET" CGO_ENABLED=1 CC="$CC_HOST" CXX="$CXX_HOST" \
  go build -buildvcs=false -trimpath \
  -ldflags="-s -w \
    -X ${MODULE}/shared.ApplicationVersion=${VERSION} \
    -X ${MODULE}/shared.RevisionDate=${REVISION_DATE} \
    -X ${MODULE}/shared.BuildDate=${BUILD_DATE}" \
  -o "${OUT_DIR}/${cli_asset}" .

# ── radiance engine (pure Go on Windows → CGO off) ───────────────────────────
echo "Building $engine_asset (tags: $RADIANCE_TAGS, toolchain: $RADIANCE_TOOLCHAIN, CGO=0) ..."
(
  cd radiance-engine
  GOOS="$GOOS_TARGET" GOARCH="$GOARCH_TARGET" CGO_ENABLED=0 \
    GOTOOLCHAIN="$RADIANCE_TOOLCHAIN" \
    go build -buildvcs=false -trimpath -tags "$RADIANCE_TAGS" \
    -o "${OUT_DIR_ABS}/${engine_asset}" .
)

# ── bundle both binaries into the single release archive ─────────────────────
echo "Packaging $archive ..."
rm -f "${OUT_DIR}/${archive}"
( cd "${OUT_DIR}" && zip -X "$archive" "$cli_asset" "$engine_asset" )

echo
echo "Done. Output in ${OUT_DIR}/:"
ls -la "${OUT_DIR}/${archive}"
echo
echo "Archive contains:"
unzip -l "${OUT_DIR}/${archive}"
echo
echo "On Windows, run (keep both files in the same folder):"
echo "  ${cli_asset} --data-path data start --engine radiance --http-proxy-addr 127.0.0.1:18070 --socks-proxy-addr 127.0.0.1:18071"
