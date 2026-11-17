#!/bin/sh
# The shape of the pending news (ADR-20261021-arm). A change a user observes is written
# as one file in docs/changes/, and a release files every one of them into
# docs/CHANGELOG.md. This gate refuses what the release could not file.
#
#   1. a file in docs/changes/ whose name is not <date>-<time>-<words>.md
#   2. a fragment with no entry, a heading outside the six categories, a
#      heading above level three, or text before its first category heading
#   3. a relative link that does not start with ../, because the release files
#      the text one folder up and takes one ../ off every link
#   4. a `## Unreleased` heading in docs/CHANGELOG.md, because every pull
#      request that wrote there conflicted with every other one
#
# Usage: check-changes.sh [changes-dir] [changelog]
#
# Both paths are arguments so that internal/arch/release_changelog_test.go can
# run this script over fixtures. scripts/cut-release-changelog.sh runs it before
# it files anything.
set -e

# With no argument the paths are the repository's, so the script moves to its
# root. Paths a caller gives are read from where the caller stands.
if [ $# -eq 0 ]; then
	case "$0" in
	*/*) cd "${0%/*}/.." ;;
	esac
fi

CHANGES="${1:-docs/changes}"
CHANGELOG="${2:-docs/CHANGELOG.md}"

if [ ! -d "$CHANGES" ]; then
	echo "changes: $CHANGES is missing. Pending news lives there, one file per change."
	exit 1
fi

if [ ! -f "$CHANGELOG" ]; then
	echo "changes: $CHANGELOG is missing. This gate reads it by name and cannot run without it."
	exit 1
fi

FAILED=0

fail() {
	echo "✗ $1"
	FAILED=1
}

# The categories of changelog-discipline.md, in the order a release files them.
# cut-release-changelog.sh reads the same list.
CATEGORIES="Security Removed Changed Deprecated Added Fixed"

for FILE in "$CHANGES"/*; do
	[ -e "$FILE" ] || continue

	NAME=${FILE##*/}

	# The folder's own README says how a fragment is written, and is no news.
	[ "$NAME" = README.md ] && continue

	if [ -d "$FILE" ]; then
		fail "$FILE is a folder. A fragment is one file directly in $CHANGES"
		continue
	fi

	# The date and time the author wrote it, so two pull requests never pick
	# one name, and a release files them in the order they were written.
	if ! printf '%s\n' "$NAME" | grep -Eq '^[0-9]{8}-[0-9]{6}-[a-z0-9]+(-[a-z0-9]+)*\.md$'; then
		fail "$FILE is not named <YYYYMMDD>-<HHMMSS>-<words>.md, such as 20261021-143210-nearby-printers.md.
    make changelog-new NAME=<words> writes one with the time it is now"
		continue
	fi

	# One awk pass reads the fragment as the release will. It prints one line
	# per defect, and the count of entries last.
	REPORT=$(awk -v categories="$CATEGORIES" '
		BEGIN { n = split(categories, list, " "); for (i = 1; i <= n; i++) known[list[i]] = 1 }
		{ sub(/\r$/, "") }
		/^[ \t]*```/ { fenced = !fenced; if (!current) print "line " NR ": text before the first category heading"; next }
		fenced { next }
		/^#{1,2}[ \t]/ { print "line " NR ": a level-one or level-two heading; a release writes those itself"; next }
		/^###[ \t]/ {
			name = $0
			sub(/^###[ \t]+/, "", name)
			sub(/[ \t]+$/, "", name)
			if (!(name in known)) print "line " NR ": \"" name "\" is not a category. Use one of: " categories
			current = name
			next
		}
		/^#{4,}[ \t]/ { print "line " NR ": a heading below level three; an entry is a bullet"; next }
		/^[ \t]*$/ { next }
		!current { print "line " NR ": text before the first category heading"; next }
		{
			rest = $0
			while (match(rest, /\]\([^)]*\)/)) {
				target = substr(rest, RSTART + 2, RLENGTH - 3)
				rest = substr(rest, RSTART + RLENGTH)
				if (target !~ /^(https?:\/\/|mailto:|#|\.\.\/)/)
					print "line " NR ": the link \"" target "\" is relative to this folder. Start it with ../, as the release files the entry one folder up"
			}
		}
		/^[-*][ \t]/ { entries++ }
		END { print "entries " entries + 0 }
	' "$FILE")

	ENTRIES=$(printf '%s\n' "$REPORT" | sed -n 's/^entries //p')

	printf '%s\n' "$REPORT" | grep -v '^entries ' | while IFS= read -r LINE; do
		[ -n "$LINE" ] && echo "✗ $FILE $LINE"
	done

	if printf '%s\n' "$REPORT" | grep -qv '^entries '; then
		FAILED=1
	fi

	if [ "$ENTRIES" -eq 0 ]; then
		fail "$FILE holds no entry. A fragment carries at least one bullet under a category heading"
	fi
done

if grep -q '^## Unreleased' "$CHANGELOG"; then
	fail "$CHANGELOG carries a '## Unreleased' heading. Pending news is a file in $CHANGES,
    so two pull requests never edit the same lines. Move the entries there"
fi

if [ "$FAILED" -ne 0 ]; then
	echo ""
	echo "changes gates failed. See .agents/rules/changelog-discipline.md and ADR-20261021-arm"
	exit 1
fi

echo "✓ changes gates passed"
