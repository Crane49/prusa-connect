#!/bin/sh
# Runs the documentation link check, docs/scripts/check-links.mjs, over the
# built site.
#
# Needs node and docs/dist, which scripts/check-docs-site.sh builds. The check
# reads the routes and anchors from the build. It does not need
# docs/node_modules.
#
# --verbose also lists the links that work for one kind of reader only: a link
# to a repository file the site does not publish, and a link to a site route
# that has no file in the repository.
set -e

ROOT=$(cd "$(dirname "$0")/.." && pwd)

if ! command -v node >/dev/null 2>&1; then
	echo "FAIL docs-links: node is not on PATH. Install the version in .nvmrc" >&2
	exit 1
fi

if [ ! -d "$ROOT/docs/dist" ]; then
	echo "FAIL docs-links: no built site at docs/dist." >&2
	echo "  Run 'make docs-site-check' first. This gate does not pass without a build." >&2
	exit 1
fi

exec node "$ROOT/docs/scripts/check-links.mjs" "$@"
