#!/bin/sh
# Lays out this machine's release of the module in bin/release: the bundle
# under ui/dist, this platform's binary and checksums.txt, which is what
# `prusa-connect module add -from` and core.modules_local read.
#
# It builds the client and the bundle first, and runs no gate. `make test` and
# `make ui-check` are the gates.
set -e

ROOT=$(cd "$(dirname "$0")/.." && pwd)
cd "$ROOT"

OUT=${1:-bin/release}
BINARY="buddy3d-camera_$(go env GOOS)_$(go env GOARCH)$(go env GOEXE)"

(cd client && pnpm run build > /dev/null)
(cd ui && pnpm run build > /dev/null)

mkdir -p bin
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o "bin/$BINARY" .
sh ../scripts/pack-release.sh ui/dist "bin/$BINARY" "$OUT"
echo "OK    $OUT"
