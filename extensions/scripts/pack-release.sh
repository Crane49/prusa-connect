#!/bin/sh
# Lays an extension out as a release, the directory `module add` reads and
# `core.modules_local` names: the built bundle at the root (module.yaml beside
# ui/), the process binary beside it, and a checksums.txt over all of them.
#
# An extension's gate packs one to install into its demo, and the server image
# packs the same layout into /opt/modules. Both call this, so the image cannot
# carry a layout the gate never installed.
#
# Usage: scripts/pack-release.sh <bundle-dir> <binary> <release-dir>
#
# <binary> keeps its file name, which must be <name>_<GOOS>_<GOARCH>: that is
# the file the host looks for in the directory the declaration points at.
set -e

if [ "$#" -ne 3 ]; then
	echo "usage: $0 <bundle-dir> <binary> <release-dir>" >&2
	exit 2
fi

BUNDLE=$1
BINARY=$2
RELEASE=$3

rm -rf "$RELEASE"
mkdir -p "$RELEASE"
cp -R "$BUNDLE"/. "$RELEASE"/
cp "$BINARY" "$RELEASE"/
# The host launches this file. A tar entry with no executable bit is unpacked
# read-only, and the module would then never start.
chmod +x "$RELEASE/$(basename "$BINARY")"

sh "$(dirname "$0")/write-checksums.sh" "$RELEASE"
