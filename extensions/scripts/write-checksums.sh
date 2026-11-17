#!/bin/sh
# Writes checksums.txt in a release directory: one sha256sum line for every
# file in it, sorted by path in byte order.
#
# The host checks this file at install, and the marketplace checks that it
# lists every file of an upload exactly once. The file itself and its
# signature, checksums.txt.sig, are not listed.
#
# Linux has sha256sum and macOS has shasum. Both print the same line, the
# digest, two spaces and the path.
#
# Usage: scripts/write-checksums.sh <release-dir>
set -e

if [ "$#" -ne 1 ]; then
	echo "usage: $0 <release-dir>" >&2
	exit 2
fi

if command -v sha256sum > /dev/null 2>&1; then
	SHA256="sha256sum"
elif command -v shasum > /dev/null 2>&1; then
	SHA256="shasum -a 256"
else
	echo "neither sha256sum nor shasum is on PATH" >&2
	exit 1
fi

cd "$1"
find . -type f ! -name checksums.txt ! -name checksums.txt.sig | sed 's|^\./||' | LC_ALL=C sort |
	while IFS= read -r file; do
		$SHA256 "$file" || exit 1
	done > checksums.txt
