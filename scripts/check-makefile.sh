#!/bin/sh
# Checks every Makefile and every *.mk include in the tree for mistakes that break a build on some
# machines only. `make makefile-check` runs it. It refuses:
#
#   1. a bare $(MAKE), which is a shell syntax error when the path has a space
#   2. a special target that GNU make 3.81 ignores
#   3. a recipe continuation that does not begin with a tab, and a quoted awk,
#      sed or grep program split across a continuation
#   4. an echo with a backslash escape
#   5. a status marker outside plain ASCII
#   6. a .PHONY list that differs from the targets in the file
#   7. a continuation joined into one line
#
# Each refusal prints the file and the line to edit. The prose rules of
# .agents/rules/writing-style.md reach a Makefile through
# scripts/check-writing-style.sh and are not checked here.
#
# The Makefiles are found by a walk, so a new one is checked without an edit
# here.
#
# The whole run is one function, called on the last line, so the shell parses
# this file to its end before executing any of it. sh reads a script while it
# runs, and an edit during a run would otherwise change what runs.
main() {

set -e

FAILED=0
ROOT=$(cd "$(dirname "$0")/.." && pwd)
cd "$ROOT"

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT INT TERM

find . \( -path ./.git -o -name node_modules -o -name vendor \
	-o -name dist \) -prune -o \( -name Makefile -o -name '*.mk' \) -print |
	sed 's|^\./||' | sort > "$TMP/makefiles"

# armed <what> <count> <floor> stops the run when a check read fewer items
# than its floor. Every check below is a grep over a stream, and a grep over an
# empty stream finds nothing and would pass.
armed() {
	[ "$2" -ge "$3" ] && return 0
	echo "FAIL  the gate read $2 $1, under its floor of $3, so this run proved nothing."
	echo "      Either the input moved, or the walk stopped reaching it."
	exit 1
}

report() {
	echo "FAIL  $1"
	FAILED=1
}

armed "Makefiles" "$(wc -l < "$TMP/makefiles" | tr -d ' ')" 1
echo "judging: $(tr '\n' ' ' < "$TMP/makefiles")"

# Every line of every Makefile, tagged "path:line:text", so each check below
# is one grep.
awk '{ print FILENAME ":" FNR ":" $0 }' $(cat "$TMP/makefiles") > "$TMP/lines"
LINES=$(wc -l < "$TMP/lines" | tr -d ' ')
armed "Makefile lines" "$LINES" 100

# A recipe line begins with a tab. Checks 3 and 4 read recipe lines only.
grep -E '^[^:]*:[0-9]+:	' "$TMP/lines" > "$TMP/recipes" || true
armed "recipe lines" "$(wc -l < "$TMP/recipes" | tr -d ' ')" 50

# Each file has its own floor as well. With a floor over the whole tree only,
# a root Makefile whose recipes lost their tabs would drop out of checks 3 and
# 4 while another Makefile kept the count up. An extension's Makefile is a few
# lines by design, so the root alone carries the larger floor.
while read -r f; do
	case "$f" in
	Makefile) lines=20 recipes=5 ;;
	*) lines=5 recipes=1 ;;
	esac
	armed "lines in $f" "$(grep -c "^$f:" "$TMP/lines" || true)" "$lines"
	armed "recipe lines in $f" "$(grep -c "^$f:" "$TMP/recipes" || true)" "$recipes"
done < "$TMP/makefiles"

echo "1. no bare \$(MAKE)..."
# On Windows MAKE expands to a path with a space in it, so the unquoted form
# is a shell syntax error. The literal name `make` is refused too: it does not
# re-enter the binary make was started as, which matters where GNU make is
# `gmake`.
MAKE_USES=$(grep -c '\$(MAKE)' "$TMP/lines" || true)
armed "\$(MAKE) invocations" "$MAKE_USES" 1
HITS=$(grep '\$(MAKE)' "$TMP/lines" | grep -v '"\$(MAKE)"' | head -20 || true)
if [ -n "$HITS" ]; then
	report 'a bare $(MAKE). Write "$(MAKE)" with the quotes.'
	echo "$HITS" | sed 's/^/    /'
fi
# A literal `make` is refused only where it is the command being run: at the
# head of a recipe line after make's `@`, `+` and `-` prefixes, or after `;`,
# `&&`, `||`, `(` or `{`. An echo that tells a person to type `make build` is
# allowed.
HITS=$(grep -E '^[^:]*:[0-9]+:	[@+-]*make |[;{(][ 	]*make |(&&|\|\|)[ 	]*make ' "$TMP/recipes" | head -20 || true)
if [ -n "$HITS" ]; then
	report 'a recipe re-entering the literal `make`. Write "$(MAKE)".'
	echo "$HITS" | sed 's/^/    /'
fi

echo "2. no special target GNU make 3.81 ignores..."
# .ONESHELL, .SHELLFLAGS and .RECIPEPREFIX arrived in GNU make 3.82, and 3.81
# ignores them without a message. Under 3.81 a recipe written for .ONESHELL
# runs one shell per line, loses its variables and still reports success.
HITS=$(grep -E '^[^:]*:[0-9]+:\.(ONESHELL|SHELLFLAGS|RECIPEPREFIX)\b' "$TMP/lines" | head -20 || true)
if [ -n "$HITS" ]; then
	report 'a special target that GNU make 3.81 ignores without a message.'
	echo "$HITS" | sed 's/^/    /'
fi

echo "3. no continuation that make will not read..."
# In a continued recipe every continued line ends in a backslash and every
# continuation begins with a tab. A continuation indented with spaces makes
# make report `missing separator`, and the message does not name the command.
#
# awk reads each file on its own, because the check looks at the line after a
# continued one, and the tagged stream joins the files end to end.
#
# Only recipes are checked. In a variable assignment make joins the lines
# itself and the indentation means nothing.
: > "$TMP/cont"
while read -r f; do
	awk -v path="$f" '
		{
			header = ($0 ~ /^[A-Za-z0-9_.][^ \t=]*:/)
			recipe = ($0 ~ /^\t/)
			blank  = ($0 ~ /^[ \t]*$/)

			if (prev != "" && !recipe) print path ":" NR - 1 ":" prev
			prev = (in_recipe && recipe && $0 ~ /\\$/) ? $0 : ""

			if (header) in_recipe = 1
			else if (!recipe && !blank) in_recipe = 0
		}
		END { if (prev != "") print path ":" NR ":" prev }
	' "$f" >> "$TMP/cont"
done < "$TMP/makefiles"
HITS=$(head -20 "$TMP/cont")
if [ -n "$HITS" ]; then
	report 'a recipe continuation whose next line does not begin with a tab. make reports `missing separator` and names nothing.'
	echo "$HITS" | sed 's/^/    /'
fi
# A line that ends in a backslash with an odd number of single or double
# quotes has left a string open. The shell then reports `unexpected EOF while
# looking for matching` and names no command. A line whose quotes balance is an
# ordinary continuation and is allowed.
HITS=$(awk -F: '
	{ line = $0; sub(/^[^:]*:[0-9]+:/, "", line) }
	line !~ /\\$/ { next }
	{
		sq = gsub(/'"'"'/, "", line)
		dq = gsub(/"/, "", line)
		if (sq % 2 == 1 || dq % 2 == 1) print $0
	}' "$TMP/lines" | head -20)
if [ -n "$HITS" ]; then
	report 'a quoted program split across a backslash continuation. Keep an awk, sed or grep invocation on one line.'
	echo "$HITS" | sed 's/^/    /'
fi

echo "4. no echo carrying an escape..."
# `echo "a\nb"` prints the backslash under the Git for Windows sh and a
# newline under a sh that expands escapes. printf behaves the same everywhere.
ECHOES=$(grep -cE '\b(echo|printf)\b' "$TMP/recipes" || true)
armed "echo and printf invocations" "$ECHOES" 20
HITS=$(grep -E '\becho\b[^;|&]*\\[ntr0\\]' "$TMP/recipes" | head -20 || true)
if [ -n "$HITS" ]; then
	report 'an echo carrying a backslash escape. Use printf.'
	echo "$HITS" | sed 's/^/    /'
fi

echo "5. no status marker outside plain ASCII..."
# GnuWin32 make on Windows prints a character outside ASCII as garbage, so a
# pass or fail line with one cannot be read there.
#
# U+2500, the line in a `# ── section ──` banner, is allowed inside a comment,
# because a comment is never printed. Only that code point is removed before
# the check, so a banner with any other marker still fails.
#
# LC_ALL=C makes awk and grep read bytes, so the result does not depend on the
# developer's locale.
SEP=$(printf '\342\224\200')
LC_ALL=C awk -v sep="$SEP" '
	{ line = $0; if (line ~ /^[ \t]*#/) gsub(sep, "", line); print FILENAME ":" FNR ":" line }
' $(cat "$TMP/makefiles") > "$TMP/ascii"
armed "Makefile lines read for markers" "$(wc -l < "$TMP/ascii" | tr -d ' ')" "$LINES"
HITS=$(LC_ALL=C grep -n '[^	 -~]' "$TMP/ascii" | cut -d: -f2- | head -20 || true)
if [ -n "$HITS" ]; then
	report 'a marker outside plain ASCII. Write OK, FAIL or WARN.'
	echo "$HITS" | sed 's/^/    /'
fi

echo "6. .PHONY names every target and only targets..."
# A target missing from .PHONY stops running when a file of the same name
# appears in the tree.
#
# A target header is a name at the start of a line, then a colon that is not
# part of an assignment. A name with a path separator is a file target and is
# not matched. FILE_TARGETS is for a file target with no separator in its
# name. An entry in it that names no target header fails.
FILE_TARGETS=""
#
# awk reads the declaration and not a sed range: a sed range looks for its end
# on the line after the start, so a one-line `.PHONY:` would also take the
# line below it.
while read -r f; do
	awk '
		/^\.PHONY:/ { collecting = 1; sub(/^\.PHONY:/, "") }
		collecting {
			more = ($0 ~ /\\$/)
			sub(/\\$/, "")
			print
			if (!more) collecting = 0
		}' "$f" |
		tr ' \t' '\n\n' | grep -v '^$' | sed "s|^|$f	|"
done < "$TMP/makefiles" | sort > "$TMP/phony"

while read -r f; do
	grep -E '^[A-Za-z0-9_][A-Za-z0-9_.-]*:([^=]|$)' "$f" |
		sed 's/:.*//' | sed "s|^|$f	|"
done < "$TMP/makefiles" | sort -u > "$TMP/targets"

armed "target headers" "$(wc -l < "$TMP/targets" | tr -d ' ')" 20
armed ".PHONY entries" "$(wc -l < "$TMP/phony" | tr -d ' ')" 20
while read -r f; do
	case "$f" in Makefile) floor=3 ;; *) floor=1 ;; esac
	armed "target headers in $f" "$(grep -c "^$f	" "$TMP/targets" || true)" "$floor"
	armed ".PHONY entries in $f" "$(grep -c "^$f	" "$TMP/phony" || true)" "$floor"
done < "$TMP/makefiles"

HITS=$(sort "$TMP/phony" | uniq -d | head -20)
if [ -n "$HITS" ]; then
	report 'a name listed twice in .PHONY.'
	echo "$HITS" | sed 's/^/    /'
fi
HITS=$(sort -u "$TMP/phony" > "$TMP/phony.u"; comm -23 "$TMP/phony.u" "$TMP/targets" | head -20)
if [ -n "$HITS" ]; then
	report 'a name in .PHONY that is not a target in the file.'
	echo "$HITS" | sed 's/^/    /'
fi
HITS=$(comm -13 "$TMP/phony.u" "$TMP/targets" | grep -v -F -x "$FILE_TARGETS" | head -20)
if [ -n "$HITS" ]; then
	report 'a target the file declares and .PHONY does not name. A file of that name in the tree would shadow it.'
	echo "$HITS" | sed 's/^/    /'
fi
for entry in $FILE_TARGETS; do
	grep -q "	$entry\$" "$TMP/targets" || report "FILE_TARGETS names $entry, which is not a target header. Delete the line."
done

echo "7. no continuation flattened into one line..."
# When a backslash and its newline are deleted from a continued recipe, the
# tab of the next line stays in the middle of the joined line. The shell reads
# it as whitespace, so the recipe still runs and make says nothing.
#
# The check looks for a space followed by a tab: a continued line ends in
# "<space>\" and the next begins with a tab. A tab after any other character
# is allowed, for example a column separator in a docker `--format` string.
HITS=$(grep -E '^[^:]*:[0-9]+:	.* 	' "$TMP/recipes" | cut -c1-120 | head -20 || true)
if [ -n "$HITS" ]; then
	report 'a tab in the middle of a recipe line, after a space. That is a backslash continuation with its newline deleted.'
	echo "$HITS" | sed 's/^/    /'
fi

if [ "$FAILED" -eq 1 ]; then
	echo ""
	echo "Makefile gates failed. Each line above names the file and the line to edit."
	exit 1
fi

echo "OK    Makefile gates passed"

}
main "$@"
