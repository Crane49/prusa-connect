#!/bin/sh
# Files the pending news under the version being released (ADR-20261021-arm). Every
# fragment in docs/changes/ is read in the order of its name, which is the order
# it was written in. Its entries go under a new `## <version> - <date>` heading
# in docs/CHANGELOG.md, grouped by category, and the fragment is deleted. It is
# the step scripts/check-release-changelog.sh asks for before a tag, done by a
# machine when a release is cut from .github/workflows/cut-release.yml.
#
# It refuses a release with nothing pending: a version with no entry would
# give the reader nothing to open. Bullets are counted the way the gate counts
# them, outside fenced blocks.
#
# With PREVIEW=1 in the environment it prints the section it would file and
# changes nothing. `make changelog-preview` runs it that way.
#
# Usage: cut-release-changelog.sh <version> [date] [changelog] [changes-dir]
#
# The date defaults to today in UTC. The two paths are arguments so that
# internal/arch/release_changelog_test.go can run this script over fixtures.
set -e

VERSION="${1:-}"
DATE="${2:-$(date -u +%Y-%m-%d)}"
CHANGELOG="${3:-docs/CHANGELOG.md}"
CHANGES="${4:-docs/changes}"

if [ -z "$VERSION" ]; then
	echo "usage: $0 <version> [date] [changelog] [changes-dir]" >&2
	exit 2
fi

if [ ! -f "$CHANGELOG" ]; then
	echo "release: $CHANGELOG is missing, so there is nowhere to file the news." >&2
	exit 1
fi

# The fragments are read as the gate reads them, so a release never files one
# the gate would have refused.
if ! sh "${0%/*}/check-changes.sh" "$CHANGES" "$CHANGELOG" >&2; then
	echo "release: the pending news in $CHANGES does not have the shape a release files." >&2
	exit 1
fi

if grep -qE "^## $(printf '%s' "$VERSION" | sed 's/[.]/[.]/g')( |$)" "$CHANGELOG"; then
	echo "release: $CHANGELOG already has a heading for $VERSION." >&2
	exit 1
fi

# Every fragment, in the order of its name. The README says how one is written.
FRAGMENTS=$(find "$CHANGES" -maxdepth 1 -type f -name '*.md' ! -name README.md | LC_ALL=C sort)

# The section, built from the fragments. Each category holds the entries of
# every fragment in turn, and the categories come in the order of
# changelog-discipline.md, highest impact first.
SECTION=$(
	# shellcheck disable=SC2086 # one path per word, and none holds a space.
	awk -v version="$VERSION" -v date="$DATE" '
		BEGIN {
			n = split("Security Removed Changed Deprecated Added Fixed", order, " ")
		}
		FNR == 1 { flush(); current = ""; fenced = 0 }
		{ sub(/\r$/, "") }
		# A line inside a fenced block is text of the entry, a "###" line
		# in a code sample included, exactly as check-changes.sh reads it.
		/^[ \t]*```/ { fenced = !fenced; if (current != "") block[++lines] = $0; next }
		fenced { if (current != "") block[++lines] = $0; next }
		/^###[ \t]/ {
			flush()
			current = $0
			sub(/^###[ \t]+/, "", current)
			sub(/[ \t]+$/, "", current)
			next
		}
		# A fragment sits one folder below the changelog, so a relative link
		# loses one "../" when it is filed. check-changes.sh lets a fragment
		# link nowhere else.
		current != "" { gsub(/\]\(\.\.\//, "]("); block[++lines] = $0 }
		function flush(    first, last, i) {
			if (current == "" || lines == 0) { lines = 0; return }
			first = 1
			while (first <= lines && block[first] ~ /^[ \t]*$/) first++
			last = lines
			while (last >= first && block[last] ~ /^[ \t]*$/) last--
			for (i = first; i <= last; i++) body[current] = body[current] block[i] "\n"
			lines = 0
		}
		END {
			flush()
			printf "## %s - %s\n", version, date
			for (i = 1; i <= n; i++) {
				if (order[i] in body) printf "\n### %s\n%s", order[i], body[order[i]]
			}
		}
	' $FRAGMENTS </dev/null
)

BULLETS=$(printf '%s\n' "$SECTION" | awk '
	/^[ \t]*```/ { fenced = !fenced; next }
	fenced { next }
	/^[ \t]*[-*][ \t]/ { count++ }
	END { print count + 0 }
')

if [ -z "$FRAGMENTS" ] || [ "$BULLETS" -eq 0 ]; then
	echo "release: $CHANGES holds no entry, so $VERSION would ship no news." >&2
	echo "  Add the entries this release ships, or cut no release." >&2
	exit 1
fi

if [ -n "${PREVIEW:-}" ]; then
	printf '%s\n' "$SECTION"
	exit 0
fi

# The file up to its first version heading, the new section, then every
# released section exactly as it was. A file with no release yet takes the
# section at its end.
TMP="$CHANGELOG.cut.$$"
SECTION_FILE="$CHANGELOG.section.$$"
printf '%s\n' "$SECTION" >"$SECTION_FILE"

awk -v section="$SECTION_FILE" '
	function emit(    line) {
		while (n > 0 && intro[n] ~ /^[ \t]*$/) n--
		for (i = 1; i <= n; i++) print intro[i]
		print ""
		while ((getline line < section) > 0) print line
		close(section)
		done = 1
	}
	!done && /^## / { emit(); print ""; print; next }
	!done { intro[++n] = $0; next }
	{ print }
	END { if (!done) emit() }
' "$CHANGELOG" >"$TMP"

rm -f "$SECTION_FILE"
mv "$TMP" "$CHANGELOG"

# The news is filed, so the fragments go. The commit that files them deletes
# them, and the next pull request starts from an empty folder.
for FRAGMENT in $FRAGMENTS; do
	rm -f "$FRAGMENT"
done

COUNT=$(printf '%s\n' "$FRAGMENTS" | grep -c .)
echo "release: filed $BULLETS entr$( [ "$BULLETS" -eq 1 ] && echo y || echo ies) from $COUNT fragment(s) under '## $VERSION - $DATE' in $CHANGELOG"
