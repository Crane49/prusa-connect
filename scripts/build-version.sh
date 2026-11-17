#!/bin/sh
# Prints the version a build stamps into the binary. The Makefile passes it to
# the linker, and `make docker-check` passes it to the image build.
#
#   a checkout at a release tag       0.2.0                the tag without its v
#   the same with changed files       0.2.0+dirty
#   any other commit                  0.1.0+3835f5f6e0a1   VERSION, then the commit
#   the same with changed files       0.1.0+3835f5f6e0a1.dirty
#   not a git checkout of this tree   0.1.0                VERSION alone
#
# The part after the plus is semver build metadata, so a module's core range
# still compares against VERSION.
#
# A source archive unpacked inside some other git checkout is not a checkout
# of this tree, so git is asked only when its top level is this folder.
#
# Usage: sh scripts/build-version.sh
set -eu

root=$(cd "$(dirname "$0")/.." && pwd -P)
base=$(tr -d '[:space:]' < "$root/VERSION")

toplevel=$(git -C "$root" rev-parse --show-toplevel 2>/dev/null || true)
if [ -z "$toplevel" ] || [ "$(cd "$toplevel" && pwd -P)" != "$root" ]; then
	echo "$base"
	exit 0
fi

dirty=""
if [ -n "$(git -C "$root" status --porcelain --untracked-files=no 2>/dev/null)" ]; then
	dirty="dirty"
fi

tag=$(git -C "$root" describe --tags --exact-match --match 'v[0-9]*' 2>/dev/null || true)
if [ -n "$tag" ]; then
	echo "${tag#v}${dirty:++$dirty}"
	exit 0
fi

commit=$(git -C "$root" rev-parse --short=12 HEAD)
echo "${base}+${commit}${dirty:+.$dirty}"
