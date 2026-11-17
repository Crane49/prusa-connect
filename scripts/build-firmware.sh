#!/usr/bin/env bash
# Builds a Prusa-Firmware-Buddy image without a bootloader for one preset, and
# prints its sha256. That value goes into tools/mini404/knowngood.json as
# firmware_sha256. The image is installed into
# ${XDG_CACHE_HOME:-~/.cache}/pcl-mini404/firmware/<firmware_ref>-<preset>/.
#
#   scripts/build-firmware.sh          # the mk4 preset
#   scripts/build-firmware.sh coreone  # another preset
#
# Needs git, sha256sum and a firmware checkout at ../Prusa-Firmware-Buddy, or
# at the path in BUDDY_SOURCE. It also needs python3, because the firmware's
# own bootstrap and build tools are Python programs. They write into that
# checkout. This script writes nothing into this repository.
#
# The image is built with `--bootloader no` because MINI404 loads it at the
# reset vector. An image that expects a bootloader does not boot there.
set -euo pipefail

PRESET="${1:-mk4}"

ROOT=$(cd "$(dirname "$0")/.." && pwd)
PIN_FILE="$ROOT/tools/mini404/knowngood.json"
FIRMWARE="${BUDDY_SOURCE:-$ROOT/../Prusa-Firmware-Buddy}"
CACHE="${XDG_CACHE_HOME:-$HOME/.cache}/pcl-mini404"

fail() {
	echo "FAIL build-firmware: $*" >&2
	exit 1
}

command -v python3 >/dev/null 2>&1 || fail "python3 is not on PATH"
[ -f "$PIN_FILE" ] || fail "no pin table at $PIN_FILE"
[ -d "$FIRMWARE/.git" ] || fail "no firmware checkout at $FIRMWARE. Set BUDDY_SOURCE to one"

# the pinned reference for this preset

# The firmware_ref of the first row whose preset matches. The pin file has one
# key per line, and a row ends at its closing brace.
REF=$(awk -v want="$PRESET" '
	/"preset":/       { split($0, q, "\""); preset = q[4] }
	/"firmware_ref":/ { split($0, q, "\""); ref = q[4] }
	/}/               { if (preset == want && !found) { print ref; found = 1 } preset = ""; ref = "" }
' "$PIN_FILE")

[ -n "$REF" ] || fail "$PIN_FILE has no row for preset $PRESET"

HEAD_SHA=$(git -C "$FIRMWARE" rev-parse HEAD)
echo "build-firmware: preset $PRESET, pinned $REF, checkout at ${HEAD_SHA:0:10}"
case "$REF" in
*"@${HEAD_SHA:0:9}"*) ;;
*) echo "build-firmware: the checkout is not the pinned revision, so the image will not match the pin" >&2 ;;
esac

# bootstrap and build

(cd "$FIRMWARE" && python3 utils/bootstrap.py)
(cd "$FIRMWARE" && python3 utils/build.py --preset "$PRESET" --build-type release --bootloader no)

# The build directory name depends on the preset and the build type, so the
# image is searched for. An image from the last two hours is preferred.
IMAGE=$(find "$FIRMWARE/build" -name 'firmware.bin' -newermt '-2 hours' -print 2>/dev/null | head -1)
[ -n "$IMAGE" ] || IMAGE=$(find "$FIRMWARE/build" -name 'firmware.bin' -print 2>/dev/null | head -1)
[ -n "$IMAGE" ] || fail "the build produced no firmware.bin under $FIRMWARE/build"

# install into the cache

TARGET="$CACHE/firmware/$REF-$PRESET"
mkdir -p "$TARGET"
install -m 0644 "$IMAGE" "$TARGET/firmware.bin"

# The firmware loads its resources from firmware.bbf on the emulated USB
# drive at first boot, so the tests need both files.
BBF="${IMAGE%.bin}.bbf"
[ -f "$BBF" ] || fail "the build produced no firmware.bbf beside $IMAGE"
install -m 0644 "$BBF" "$TARGET/firmware.bbf"

DIGEST=$(sha256sum "$TARGET/firmware.bin" | cut -d' ' -f1)
echo "build-firmware: installed $TARGET/firmware.bin"
echo "build-firmware: sha256 $DIGEST"
echo "build-firmware: put that value in $PIN_FILE as firmware_sha256 for preset $PRESET"
