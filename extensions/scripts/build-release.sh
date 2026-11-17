#!/bin/sh
# Builds the release of one extension for the marketplace: the manifest, one
# binary for every platform module.yaml declares, the built browser bundle
# under ui/, the licence, and a checksums.txt over all of them.
#
# The layout is the one scripts/pack-release.sh writes for one platform. A
# browser bundle alone declares no platforms, so its release carries no
# binary.
#
# The browser bundle must already be built. The extension's gate builds it
# into ui/dist and tests it there, so the release ships the bundle the gate
# passed. This script refuses a ui/dist whose module.yaml is not the current
# one, because that bundle was built from an older tree.
#
# It also refuses a version that is not the top heading of CHANGELOG.md. The
# marketplace shows that section as the release notes.
#
# The release carries LICENSE at its root, because people download it from
# the marketplace. An extension of this repository has the core's licence, so
# the file is the LICENSE at the root of this repository. An extension with a
# LICENSE of its own ships that one instead. scripts/pack-release.sh adds no
# licence: other repositories pack their own modules with it.
#
# Usage: scripts/build-release.sh <extension-dir> <release-dir>
#
#   scripts/build-release.sh extensions/parts bin/release/parts
#
# <release-dir> is deleted first. Each binary is built with CGO off and
# -trimpath, so it runs on a host with no C library of a given version and
# names no path of the machine that built it. -s -w leave out the symbol table
# and the DWARF data, which halves the archive every server downloads. A stack
# trace still names the functions and lines.
set -e

if [ "$#" -ne 2 ]; then
	echo "usage: $0 <extension-dir> <release-dir>" >&2
	exit 2
fi

EXT_DIR=$(cd "$1" && pwd)
RELEASE=$2
MANIFEST=$EXT_DIR/module.yaml
DIST=$EXT_DIR/ui/dist

fail() {
	echo "FAIL  $*" >&2
	exit 1
}

[ -f "$MANIFEST" ] || fail "$1 has no module.yaml"

scalar() {
	sed -n "s/^$1:[ 	]*//p" "$MANIFEST" | head -1
}

NAME=$(scalar name)
VERSION=$(scalar version)
[ -n "$NAME" ] || fail "$MANIFEST declares no name"
[ -n "$VERSION" ] || fail "$MANIFEST declares no version"

# The list under `platforms:`, one `- goos_goarch` per line, up to the next
# key at the left margin.
PLATFORMS=$(awk '
	/^platforms:/ { inside = 1; next }
	inside && /^[^ \t#]/ { inside = 0 }
	inside && /^[ \t]*-[ \t]/ { sub(/^[ \t]*-[ \t]*/, ""); sub(/[ \t]*(#.*)?$/, ""); print }
' "$MANIFEST")

# The top heading of the changelog, `## 1.2.3 - 2026-10-19`.
TOP=$(sed -n 's/^## \([0-9][0-9.]*\) - [0-9-]*$/\1/p' "$EXT_DIR/CHANGELOG.md" | head -1)
[ "$TOP" = "$VERSION" ] ||
	fail "module.yaml declares $VERSION and the top heading of CHANGELOG.md is '${TOP:-none}'"

[ -f "$DIST/module.yaml" ] && [ -d "$DIST/ui" ] ||
	fail "$DIST holds no built bundle. Run the gate first: make -C extensions check EXT=$(basename "$EXT_DIR")"
cmp -s "$MANIFEST" "$DIST/module.yaml" ||
	fail "$DIST/module.yaml differs from module.yaml, so the bundle is from an older tree. Run the gate again"

if [ -n "$PLATFORMS" ] && [ ! -f "$EXT_DIR/go.mod" ]; then
	fail "module.yaml declares platforms and $1 has no go.mod to build them from"
fi

rm -rf "$RELEASE"
mkdir -p "$RELEASE"
RELEASE=$(cd "$RELEASE" && pwd)
cp -R "$DIST"/. "$RELEASE"/

LICENCE=$EXT_DIR/LICENSE
[ -f "$LICENCE" ] || LICENCE=$(cd "$(dirname "$0")/../.." && pwd)/LICENSE
[ -f "$LICENCE" ] || fail "neither $1 nor the repository root has a LICENSE"
cp "$LICENCE" "$RELEASE/LICENSE"

for platform in $PLATFORMS; do
	goos=${platform%%_*}
	goarch=${platform#*_}
	binary=${NAME}_${platform}
	[ "$goos" = windows ] && binary=$binary.exe

	echo "building $binary"
	( cd "$EXT_DIR" && CGO_ENABLED=0 GOOS=$goos GOARCH=$goarch go build -trimpath -ldflags='-s -w' -o "$RELEASE/$binary" . ) ||
		fail "the build for $platform failed"
	# The host launches this file. A tar entry with no executable bit is
	# unpacked read-only, and the module would then never start.
	chmod +x "$RELEASE/$binary"
done

sh "$(dirname "$0")/write-checksums.sh" "$RELEASE"

echo "OK    $NAME $VERSION is in $RELEASE"
