#!/bin/sh
# Checks that the git hooks of this repository run in this clone. The pre-push
# hook and `make hooks-check` run it.
#
# .githooks/ has the hooks, and `make install-hooks` points core.hooksPath
# there. The frontend's `pnpm install` runs husky, which points the same
# setting at frontend/.husky/_. frontend/.husky/commit-msg and
# frontend/.husky/pre-push call the hooks in .githooks/, so either install
# order works.
#
#   1. the hooks directory git uses for this clone has every hook that
#      .githooks/ has
#   2. its commit-msg refuses a `fix:` message with no `Changelog:` trailer,
#      and accepts the same message with one
#
# Check 2 runs the hook. A husky dispatcher with no delegate beside it is
# present, executable and exits 0 on every message, so check 1 alone would
# pass it.
#
# pre-push is checked for its presence only. Running it would run every gate
# in it, and it calls this script.
#
# To fix a refusal, run `make install-hooks`, or restore the missing file in
# frontend/.husky.
set -e

ROOT=$(git rev-parse --show-toplevel)
cd "$ROOT"

# The directory git looks in: core.hooksPath when it is set, .git/hooks when
# it is not.
HOOKS=$(git rev-parse --git-path hooks)

FAILED=0

fail() {
	echo "✗ $1" >&2
	FAILED=1
}

# The hook names are read from .githooks/, so a new hook is checked without an
# edit here.
NAMES=$(ls .githooks 2>/dev/null | grep -v '^\.' || true)
COUNT=$(printf '%s\n' "$NAMES" | grep -c . || true)

# .githooks/ has at least commit-msg and pre-push. Fewer means the folder was
# not read.
if [ "$COUNT" -lt 2 ]; then
	echo "✗ the gate read $COUNT hook(s) in .githooks/, under its floor of 2, so this run proved nothing." >&2
	echo "  Either .githooks/ moved, or the read stopped reaching it." >&2
	exit 1
fi

for hook in $NAMES; do
	[ -f "$HOOKS/$hook" ] && continue
	fail "no $hook in $HOOKS, and .githooks/$hook exists, so that gate does not run in this clone.
    Run \`make install-hooks\`. If the frontend's install moved core.hooksPath,
    frontend/.husky/$hook is the delegate that has gone missing."
done

if [ "$FAILED" -eq 1 ]; then
	echo ""
	echo "hook gates failed. Run \`make install-hooks\`." >&2
	exit 1
fi

PROBE=$(mktemp -d)
trap 'rm -rf "$PROBE"' EXIT INT TERM

# A `fix:` message with no `Changelog:` trailer, which the hook must refuse.
# `Fixes: none` is there so that the missing `Changelog:` trailer is the only
# reason to refuse it.
cat > "$PROBE/refused" <<'EOF'
fix(probe): the message scripts/check-hooks.sh expects to be refused

Fixes: none
EOF

# The same subject with the trailer, which the hook must accept. It catches a
# hook that refuses every message.
cat > "$PROBE/accepted" <<'EOF'
fix(probe): the message scripts/check-hooks.sh expects to be accepted

Fixes: none
Changelog: none
EOF

# The refused message with a blank line and a comment in front, which is what
# an editor or a template can leave there. A hook that reads the subject with
# `head -1` gets an empty subject and accepts the message.
{
	printf '\n'
	printf '# please enter the commit message for your changes\n'
	printf 'fix(probe): the message scripts/check-hooks.sh expects to be refused\n'
	printf '\n'
	printf 'Fixes: none\n'
} > "$PROBE/refused-padded"

for probe in refused refused-padded; do
	if sh "$HOOKS/commit-msg" "$PROBE/$probe" >"$PROBE/out" 2>&1; then
		fail "$HOOKS/commit-msg accepted a fix: message with no Changelog: trailer ($probe), so it is inert.
    Its output was:"
		sed 's/^/    /' "$PROBE/out" >&2
	fi
done

if ! sh "$HOOKS/commit-msg" "$PROBE/accepted" >"$PROBE/out" 2>&1; then
	fail "$HOOKS/commit-msg refused a fix: message carrying \`Changelog: none\`, which the rule
    asks for. A hook that refuses every message is switched off within the day.
    Its output was:"
	sed 's/^/    /' "$PROBE/out" >&2
fi

if [ "$FAILED" -eq 1 ]; then
	echo ""
	echo "hook gates failed. See .agents/rules/changelog-discipline.md" >&2
	exit 1
fi

echo "✓ hook gates passed, from $HOOKS"
