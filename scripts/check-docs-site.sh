#!/bin/sh
# Builds the documentation site into docs/dist and fails when it does not
# build. One page with frontmatter that YAML cannot parse fails the whole
# content collection, so every page of the site is lost.
#
# Needs node and docs/node_modules. It fails when either is absent:
# `make install-docs-deps` installs the second, and needs pnpm.
#
# It leaves docs/dist in place, because scripts/check-docs-links.sh reads it.
set -e

ROOT=$(cd "$(dirname "$0")/.." && pwd)
DOCS="$ROOT/docs"
DIST="$DOCS/dist"

# The package entry point. node_modules/.bin/astro is a shell script, or a
# .CMD file on Windows, and `node` cannot run either.
ASTRO="$DOCS/node_modules/astro/bin/astro.mjs"

fail() {
	echo "FAIL docs-site: $1" >&2
	exit 1
}

[ -d "$DOCS" ] || fail "no docs/ directory under $ROOT"

command -v node >/dev/null 2>&1 || fail "node is not on PATH. Install the version in .nvmrc"

if [ ! -f "$ASTRO" ]; then
	echo "FAIL docs-site: docs/node_modules is not installed, so the site cannot be built." >&2
	echo "  Install it with 'make install-docs-deps', which needs pnpm, then run this again." >&2
	echo "  This gate does not pass without a build. The defect it guards is a build failure." >&2
	exit 1
fi

# Built from empty. In an old dist a deleted page keeps its index.html, and
# the link check would accept a link to it.
rm -rf "$DIST"

# Astro caches the HTML of each page here, keyed by the page's bytes. A change
# to the markdown pipeline in astro.config.mjs does not change those bytes, so
# the cache is removed to make the build render every page again.
rm -f "$DOCS/node_modules/.astro/data-store.json"

echo "docs-site: building the Starlight site"

# Astro is called directly. `pnpm run build` would also run the prebuild step
# of docs/package.json, which rewrites the AsyncAPI reference pages.
cd "$DOCS" && node "$ASTRO" build || fail "the site does not build. A page with unparseable frontmatter fails the whole collection, not only its own page"

PAGES=$(find "$DIST" -name index.html | wc -l | tr -d ' ')

# A build that finds no content succeeds with almost no pages, so fewer than
# 40 pages fails.
[ "$PAGES" -ge 40 ] || fail "the build emitted $PAGES page(s), and this gate expects at least 40"

echo "OK docs-site: $PAGES page(s) built to docs/dist"

# Every mermaid block must parse and must be in the built page as a figure,
# and not as a code block.
node "$ROOT/docs/scripts/check-mermaid.mjs" || fail "the mermaid diagrams did not survive the build"
