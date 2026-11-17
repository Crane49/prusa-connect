#!/bin/sh
# Makes sure pnpm is available, for a contributor who has not installed it.
# The Makefile calls it before every target that runs pnpm.
#
# It tries, in order: pnpm on PATH, `corepack enable`, and then an install with
# npm. Corepack reads the `packageManager` field of each package.json and runs
# the pnpm version pinned there. Node 24, the version in .nvmrc, includes
# corepack. Node 25 does not, so the npm install exists for it.
#
# It asks before it installs, and only on a terminal. In CI, or with no
# terminal, it fails with the install commands, so a build never waits for
# input.
#
# Usage: sh scripts/ensure-pnpm.sh [auto]
#   auto: "1" installs without asking. The Makefile passes AUTO_INSTALL_PNPM.
set -e

AUTO=$1

command -v pnpm >/dev/null 2>&1 && exit 0
corepack enable >/dev/null 2>&1 && { echo "OK    pnpm enabled via corepack"; exit 0; }

echo "pnpm is required but is not installed."
echo "Node 24 (see .nvmrc) bundles corepack; Node 25+ dropped it, and it is"
echo "not on this machine either."

if [ "$AUTO" = "1" ]; then
	reply=y
elif [ -n "$CI" ] || [ ! -r /dev/tty ]; then
	echo ""
	echo "FAIL  Non-interactive shell, not installing anything unasked."
	echo "  Re-run with AUTO_INSTALL_PNPM=1, or install manually:"
	echo "    npm install -g corepack && corepack enable  # keeps the pinned version"
	echo "    npm install -g pnpm                         # pnpm 10+ self-pins"
	exit 1
else
	printf "\nInstall it now (npm install -g corepack && corepack enable)? [y/N] "
	read -r reply < /dev/tty || reply=""
fi

case "$reply" in
	[yY] | [yY][eE][sS]) ;;
	*)
		echo "Aborted. See https://pnpm.io/installation"
		exit 1
		;;
esac

echo "-> installing corepack..."
if npm install -g corepack && corepack enable; then
	hash -r 2>/dev/null || true
	echo "OK    pnpm ready via corepack ($(pnpm --version 2>/dev/null || echo '?'))"
elif echo "-> corepack route failed, installing pnpm directly..." && npm install -g pnpm; then
	hash -r 2>/dev/null || true
	echo "OK    pnpm installed ($(pnpm --version 2>/dev/null || echo '?'))"
else
	echo "FAIL  Install failed. A global npm install may need sudo, or npm is absent."
	echo "  See https://pnpm.io/installation"
	exit 1
fi
