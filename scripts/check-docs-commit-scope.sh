#!/bin/sh
# Refuses a `docs:` commit that changes Go code. A reader who later looks for
# a behaviour change does not look in a `docs:` commit. The commit-msg hook
# runs it.
#
# It reads the staged diff of the Go files under internal/, sdk/, cmd/ and
# adapters/. A changed line is not code when it is:
#
#   a comment          //, /*, * or */ after leading whitespace
#   a blank line
#   a string literal   a line that opens with a double quote, which is how
#                      prose and paths are written in a Go file
#
# Any other changed line fails the commit. To fix it, use the prefix the change
# needs (feat, fix, refactor or test), or split the commit.
#
# `style:` is not checked, because formatting a Go file changes code lines.
#
# Usage: sh scripts/check-docs-commit-scope.sh <subject> [diff file]
#   Without a diff file it reads `git diff --cached`. With `git commit -o` that
#   is the diff of the named paths only. A test passes a diff file.
set -e

SUBJECT=$1
DIFF=$2

case "$SUBJECT" in
docs:* | docs\(*\):*) ;;
*) exit 0 ;;
esac

if [ -n "$DIFF" ]; then
	cat "$DIFF"
else
	git diff --cached -- "internal/*.go" "sdk/*.go" "cmd/*.go" "adapters/*.go"
fi > /tmp/.docs-scope.$$ 2>/dev/null || true

CODE=$(grep -E '^[+-]' /tmp/.docs-scope.$$ |
	grep -vE '^(\+\+\+|---)' |
	sed 's/^[+-]//' |
	grep -vE '^[[:space:]]*(//|/\*|\*|\*/)' |
	grep -vE '^[[:space:]]*"' |
	grep -vE '^[[:space:]]*$' || true)

rm -f /tmp/.docs-scope.$$

[ -n "$CODE" ] || exit 0

cat >&2 <<'EOF'
FAIL  this docs: commit changes Go code

A docs: prefix says the change is documentation, so nobody looks in it for
behaviour. The lines below are neither a comment, a blank line, nor a string
literal, which makes them code somebody will later go looking for under
another commit.

Use the prefix the change earns: feat:, fix:, refactor: or test:. Where the
documentation and the code genuinely belong together, they are two commits.

EOF
printf '%s\n' "$CODE" | head -10 | sed 's/^/    /' >&2
echo "" >&2
echo "See .agents/rules/git-discipline.md, the prefix table." >&2

exit 1
