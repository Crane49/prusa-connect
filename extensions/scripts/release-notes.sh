#!/bin/sh
# Prints the release notes of an extension's newest version: the text under
# the top `## <version> - YYYY-MM-DD` heading of its CHANGELOG.md, up to the
# next such heading, with the blank lines around it removed.
#
# The release workflow sends this text to the marketplace with the release.
#
# Usage: scripts/release-notes.sh <extension-dir>
set -e

if [ "$#" -ne 1 ]; then
	echo "usage: $0 <extension-dir>" >&2
	exit 2
fi

NOTES=$(awk '
	/^## [0-9][0-9.]* - [0-9-]*$/ { found++; next }
	found == 1 { print }
	found == 2 { exit }
' "$1/CHANGELOG.md" | sed -e '/./,$!d')

if [ -z "$NOTES" ]; then
	echo "FAIL  $1/CHANGELOG.md has no text under its top release heading" >&2
	exit 1
fi

printf '%s\n' "$NOTES"
