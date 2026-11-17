#!/bin/sh
# Reachability: a function under internal/ or adapters/ that no path from a
# main or a test reaches.
#
# Nothing else in this repository measures this. A function keeps compiling,
# keeps passing review and keeps being maintained long after its last caller
# went, and the only way to see it is whole-program analysis.
#
# golang.org/x/tools/cmd/deadcode does that analysis. It builds the call graph
# from every main package and, with -test, from every test binary as well, and
# prints what no edge reaches.
#
# Scope, and why it is not the whole tree:
#
#   internal/, adapters/   judged. Every caller they can have is in this module
#   sdk/                   not judged. It is a separate module published for
#                          third parties, so its whole surface is unreachable
#                          from here by design. `make sdk-check` holds it, by
#                          compiling an outside module against it
#
# One tag set, which is the full one. A function reachable only in a build that
# omits a tag reads as unreachable here, and that is what the allow file's
# reason column is for.
set -e

ROOT=$(cd "$(dirname "$0")/.." && pwd)
cd "$ROOT"

ALLOW=scripts/reachability.allow

if [ -z "$DEADCODE_TAGS" ]; then
	echo "FAIL  DEADCODE_TAGS is empty. The caller passes the tag set, so this"
	echo "      script and the Makefile cannot disagree about the full build."
	exit 1
fi

if [ -z "$DEADCODE_VERSION" ]; then
	echo "FAIL  DEADCODE_VERSION is empty. The Makefile pins it."
	exit 1
fi

if [ ! -f "$ALLOW" ]; then
	echo "FAIL  $ALLOW is missing. Every finding it holds would be reported as new."
	exit 1
fi

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT INT TERM

echo "=== Reachability ==="
echo "tags: $DEADCODE_TAGS"

if ! go run "golang.org/x/tools/cmd/deadcode@$DEADCODE_VERSION" \
	-test -tags "$DEADCODE_TAGS" ./... > "$TMP/raw" 2>"$TMP/err"; then
	echo "FAIL  deadcode did not run:"
	sed 's/^/      /' "$TMP/err"
	exit 1
fi

# Windows writes the path with backslashes, and the line and column drift with
# every edit above them. The key is the file and the symbol, so a repair one
# line up does not have to be chased through the allow file.
tr '\\' '/' < "$TMP/raw" |
	sed -n 's#^\([^:]*\):[0-9]*:[0-9]*: unreachable func: \(.*\)$#\1	\2#p' |
	sort -u > "$TMP/all"

# The arming check. deadcode over this tree prints unreachable functions in
# every module it reads, sdk/ included, and that total does not go to zero while
# the tree stands. A run that read nothing prints nothing, and without this the
# gate would call that a clean tree.
#
# The floor was re-measured after the retired server was deleted. The run reads
# 111 functions over the whole tree, and 80 leaves room for a wave of repairs
# without leaving the gate disarmed.
TOTAL=$(wc -l < "$TMP/all" | tr -d ' ')
FLOOR=80

if [ "$TOTAL" -lt "$FLOOR" ]; then
	echo "FAIL  deadcode reported $TOTAL functions over the whole tree, under the floor of $FLOOR."
	echo "      A green over an empty read says \"nothing was found\" and \"nothing was"
	echo "      looked at\" in the same word. Either the analysis stopped reaching the"
	echo "      packages, or the output format moved."
	exit 1
fi

echo "read: $TOTAL unreachable functions over the whole tree"

grep -E '^(internal|adapters)/' "$TMP/all" > "$TMP/judged" || true

# The allow file is TAB separated: path, symbol, reason. A line needs all three,
# because the whole failure mode is an entry nobody had to argue for.
grep -v '^#' "$ALLOW" | grep -v '^[[:space:]]*$' > "$TMP/allow.lines" || true

awk -F'\t' -v allow="$ALLOW" '
	NF != 3 || $1 == "" || $2 == "" || $3 == "" {
		printf "%s line %d holds %d field(s), and a line is path, symbol and reason: %s\n", allow, FNR, NF, $0
		bad = 1
	}
	END { exit bad ? 1 : 0 }' "$TMP/allow.lines" > "$TMP/shape" || true

if [ -s "$TMP/shape" ]; then
	echo "FAIL  the allow file is malformed:"
	sed 's/^/      /' "$TMP/shape"
	exit 1
fi

cut -f1,2 "$TMP/allow.lines" | sort -u > "$TMP/allowed"

FAILED=0

comm -23 "$TMP/judged" "$TMP/allowed" > "$TMP/new"
if [ -s "$TMP/new" ]; then
	echo "FAIL  a function under internal/ or adapters/ that nothing reaches:"
	sed 's/^/      /' "$TMP/new"
	echo ""
	echo "      Delete it, or wire it up, or add a line to $ALLOW naming the"
	echo "      file, the symbol and the reason it is unreachable here."
	FAILED=1
fi

comm -13 "$TMP/judged" "$TMP/allowed" > "$TMP/stale"
if [ -s "$TMP/stale" ]; then
	echo "FAIL  a line in $ALLOW describes nothing:"
	sed 's/^/      /' "$TMP/stale"
	echo ""
	echo "      The symbol is reachable now, or it is gone. Delete the line."
	FAILED=1
fi

if [ "$FAILED" -eq 1 ]; then
	exit 1
fi

echo "OK    reachability: $(wc -l < "$TMP/judged" | tr -d ' ') allowed, none unaccounted for"
