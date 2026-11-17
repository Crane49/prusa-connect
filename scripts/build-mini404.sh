#!/usr/bin/env bash
# Builds qemu-system-buddy, the MINI404 printer emulator, at the revision
# tools/mini404/knowngood.json pins. It installs the binary into
# ${XDG_CACHE_HOME:-~/.cache}/pcl-mini404/mini404-<sha>/ and writes nothing
# into this repository.
#
# Needs git, ninja, pkg-config and the libraries in APT_LINE. It also needs
# python3, because the emulator's own configure step runs on it.
#
# The source is the sibling checkout at ../MINI404 when its HEAD is the pinned
# revision. Otherwise the script clones the emulator into the cache. It never
# changes a sibling checkout that is on another revision.
#
# MINI404_SOURCE           another path for the sibling checkout
# MINI404_FORCE=1          rebuild when the binary already exists
# MINI404_CONFIGURE_EXTRA  extra configure flags, default --enable-gcov
#
# The default is --enable-gcov because the emulator's script console replies
# "ACK" only in a gcov build (hw/arm/prusa/utility/p404_script_console.c), and
# ScriptConsole.Do in tools/mini404 waits for that reply. In a build without
# it, use ScriptConsole.Send.
set -euo pipefail

ROOT=$(cd "$(dirname "$0")/.." && pwd)
PIN_FILE="$ROOT/tools/mini404/knowngood.json"
SIBLING="${MINI404_SOURCE:-$ROOT/../MINI404}"
UPSTREAM="https://github.com/vintagepc/MINI404"
CONFIGURE_EXTRA="${MINI404_CONFIGURE_EXTRA:---enable-gcov}"

CACHE="${XDG_CACHE_HOME:-$HOME/.cache}/pcl-mini404"

fail() {
	echo "FAIL build-mini404: $*" >&2
	exit 1
}

# dependencies

APT_LINE="sudo apt install -y ninja-build libslirp-dev libusb-1.0-0-dev libelf-dev libpng-dev"
MISSING=()

# python3 is for the emulator's configure step. This script runs no Python.
for tool in git python3 ninja pkg-config; do
	command -v "$tool" >/dev/null 2>&1 || MISSING+=("$tool")
done

if command -v pkg-config >/dev/null 2>&1; then
	for lib in slirp libusb-1.0 libpng glib-2.0 pixman-1 libelf; do
		pkg-config --exists "$lib" || MISSING+=("$lib (pkg-config)")
	done
fi

if [ ${#MISSING[@]} -gt 0 ]; then
	echo "FAIL build-mini404: missing build dependencies:" >&2
	for m in "${MISSING[@]}"; do
		echo "  - $m" >&2
	done
	echo >&2
	echo "  $APT_LINE" >&2
	exit 1
fi

# the pinned revision

[ -f "$PIN_FILE" ] || fail "no pin table at $PIN_FILE"

# The first mini404_sha in the file. The pin file has one key per line.
SHA=$(sed -n 's/.*"mini404_sha": *"\([^"]*\)".*/\1/p' "$PIN_FILE" | head -1)

[ -n "$SHA" ] || fail "$PIN_FILE has no mini404_sha"

TARGET="$CACHE/mini404-$SHA"
BINARY="$TARGET/qemu-system-buddy"

if [ -x "$BINARY" ] && [ -z "${MINI404_FORCE:-}" ]; then
	echo "build-mini404: $BINARY is already built. Set MINI404_FORCE=1 to rebuild."
	exit 0
fi

# the source tree

SOURCE=""
if [ -d "$SIBLING/.git" ]; then
	SIBLING_HEAD=$(git -C "$SIBLING" rev-parse HEAD)
	case "$SIBLING_HEAD" in
	"$SHA"*)
		SOURCE=$(cd "$SIBLING" && pwd)
		echo "build-mini404: building from the sibling checkout at $SOURCE ($SHA)"
		;;
	*)
		echo "build-mini404: the sibling checkout is at ${SIBLING_HEAD:0:10}, not $SHA, so it is left alone"
		;;
	esac
fi

if [ -z "$SOURCE" ]; then
	SOURCE="$CACHE/src/MINI404"
	if [ ! -d "$SOURCE/.git" ]; then
		mkdir -p "$(dirname "$SOURCE")"
		echo "build-mini404: cloning $UPSTREAM into $SOURCE"
		git clone "$UPSTREAM" "$SOURCE"
	fi
	git -C "$SOURCE" fetch --all --tags
	git -C "$SOURCE" checkout --detach "$SHA"
	git -C "$SOURCE" submodule update --init --recursive
	echo "build-mini404: building from the cache clone at $SOURCE ($SHA)"
fi

# configure and build

# A gcov configure includes the prusa qtests, and their meson list expects
# hw/arm/prusa reachable as tests/qtest/prusa. Upstream CI makes the same
# symlink (.github/workflows/tests.yml, "Test Symlink").
ln -sfn "$SOURCE/hw/arm/prusa" "$SOURCE/tests/qtest/prusa"

# patches/mini404-xbuddy-compat.patch lets current firmware boot. Without it
# the emulated board reports BOM id 0, which the firmware stops on with "BOM
# ID not compatible", and it has no TCA6408A I/O expander at I2C address 0x20,
# which the firmware stops on with I2C_TX_ERROR. The patch sets BOM id 14 and
# adds the expander.
PATCH_DIR="$(cd "$(dirname "$0")" && pwd)/patches"
if git -C "$SOURCE" apply --check "$PATCH_DIR/mini404-xbuddy-compat.patch" 2>/dev/null; then
	git -C "$SOURCE" apply "$PATCH_DIR/mini404-xbuddy-compat.patch"
	echo "build-mini404: applied mini404-xbuddy-compat.patch"
elif git -C "$SOURCE" apply --check --reverse "$PATCH_DIR/mini404-xbuddy-compat.patch" 2>/dev/null; then
	echo "build-mini404: mini404-xbuddy-compat.patch already applied"
else
	fail "mini404-xbuddy-compat.patch neither applies nor is applied; the pin moved under the patch"
fi

BUILD="$SOURCE/build"
mkdir -p "$BUILD"

if [ ! -f "$BUILD/build.ninja" ] || [ -n "${MINI404_FORCE:-}" ]; then
	# --disable-werror: the pinned source has compiler warnings under newer
	# host compilers, and they must not stop the build.
	# shellcheck disable=SC2086 # CONFIGURE_EXTRA is a list of flags and must split
	(cd "$BUILD" && ../configure \
		--target-list=buddy-softmmu \
		--enable-slirp \
		--disable-werror \
		$CONFIGURE_EXTRA)
fi

ninja -C "$BUILD" qemu-system-buddy

[ -x "$BUILD/qemu-system-buddy" ] || fail "the build produced no $BUILD/qemu-system-buddy"

# install into the cache

# QEMU looks for its data files, the keymaps among them, in pc-bios beside the
# executable, so pc-bios is copied with the binary.
mkdir -p "$TARGET"
install -m 0755 "$BUILD/qemu-system-buddy" "$BINARY"
if [ -d "$BUILD/pc-bios" ]; then
	rm -rf "$TARGET/pc-bios"
	cp -r "$BUILD/pc-bios" "$TARGET/pc-bios"
fi

echo "build-mini404: installed $BINARY"
"$BINARY" --version | head -1
