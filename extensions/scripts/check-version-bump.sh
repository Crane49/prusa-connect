#!/bin/sh
# Refuses a change to what an extension ships when its module.yaml keeps the
# same version.
#
# The server image carries the extensions in /opt/modules, and an install
# offers an update only when the offered version differs from the installed
# one. A change shipped under the old version therefore never reaches a server
# that already has the module, and nothing says so.
#
# What does not ship is exempt: Markdown, tests, the test helpers under a
# src/test/ folder, and each extension's scripts/.
# A version bump also needs a CHANGELOG.md heading, which the extension's own
# gate checks.
#
# Usage: scripts/check-version-bump.sh <base-commit>. The repository's working
# tree is compared with that commit.
set -e

if [ "$#" -ne 1 ]; then
	echo "usage: $0 <base-commit>" >&2
	exit 2
fi
BASE=$1

cd "$(dirname "$0")/../.."

version() {
	sed -n 's/^version:[ 	]*//p' | head -1
}

CHANGED=$(git diff --name-only "$BASE" -- extensions/ |
	grep -v -e '\.md$' -e '_test\.go$' -e '\.test\.ts$' -e '/src/test/' -e '^extensions/[^/]*/scripts/' |
	sed -n 's|^extensions/\([^/]*\)/.*|\1|p' | sort -u)

FAILED=0
for ext in $CHANGED; do
	[ -f "extensions/$ext/module.yaml" ] || continue
	# A new extension has no version to compare with.
	before=$(git show "$BASE:extensions/$ext/module.yaml" 2> /dev/null | version) || true
	[ -n "$before" ] || continue
	after=$(version < "extensions/$ext/module.yaml")
	if [ "$before" = "$after" ]; then
		echo "✗ extensions/$ext changed and module.yaml still declares $after; raise version and add its CHANGELOG.md entry"
		FAILED=1
	fi
done

exit $FAILED
