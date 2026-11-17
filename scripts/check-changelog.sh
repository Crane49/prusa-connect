#!/bin/sh
# The changelog gates of .agents/rules/changelog-discipline.md. Each fails,
# none warns.
#
#   1. a bump-worthy commit with no Changelog: trailer
#   2. a trailer that is not `none` with nothing added to the pending news in
#      docs/changes/ across the push
#   3. a trailer whose text is not the first sentence of an entry
#   4. a fragment in docs/changes/ the release could not file, which
#      scripts/check-changes.sh judges
#
# Range defaults to @{upstream}..HEAD, which is what a push publishes. Pass a
# range to check something else: `scripts/check-changelog.sh main..HEAD`.
#
# EXCLUDE_PUBLISHED in the environment narrows that range to the commits no
# remote-tracking ref already holds. See where it is read, below.
#
# Nothing at or below FLOOR is judged. The rule landed in b935cff and a commit
# written before it could not carry a trailer, so a range reaching further back
# asks 260 authors for something that did not exist. CI passes
# origin/main..HEAD, which reaches all of them.
#
# Not checked: entry wording, entry length, and whether links resolve. A
# breaking change needs a long entry, and a network call would make an offline
# commit fail.
set -e

# Both paths below are repository-relative, and so is the pathspec this gate
# hands `git diff`. Run from a subdirectory, the exceptions file is missing and
# the pathspec matches nothing, so the gate refused a push it had never read.
# scripts/check-commit-style.sh and scripts/check-hooks.sh both do this cd, and
# it is parameter expansion rather than `dirname` for the reason the first of
# them gives: a command substitution forks, and a fork measured 0.5 seconds on
# the workstation these run on.
case "$0" in
*/*) cd "${0%/*}/.." ;;
esac

CHANGELOG="docs/CHANGELOG.md"
CHANGES="docs/changes"
EXCEPTIONS="scripts/changelog.exceptions"
RANGE="${1:-}"

# The two files this gate reads by name. Neither absence can be an empty
# answer: a missing changelog makes every trailer look unbacked, and a missing
# exceptions file drops the record of what history can never satisfy.
if [ ! -f "$CHANGELOG" ]; then
	echo "changelog: $CHANGELOG is missing. This gate reads it by name and cannot run without it."
	exit 1
fi

# The fragments in the tree, whatever the range and before any answer that
# ends the gate early: a release files every one of them, so one it could not
# file fails the push that would leave it there, even a push with no commit to
# judge.
if ! sh scripts/check-changes.sh "$CHANGES" "$CHANGELOG"; then
	echo ""
	echo "changelog gates failed. See .agents/rules/changelog-discipline.md"
	exit 1
fi

if [ -z "$RANGE" ]; then
	if ! git rev-parse --abbrev-ref '@{upstream}' >/dev/null 2>&1; then
		echo "changelog: no upstream for this branch, so there is no push to check"
		exit 0
	fi
	RANGE="@{upstream}..HEAD"
fi

# The commit that added .agents/rules/changelog-discipline.md.
FLOOR=b935cff

if ! git cat-file -e "$FLOOR^{commit}" 2>/dev/null; then
	echo "changelog: $FLOOR is not in this clone, so the floor cannot be applied."
	echo "  A shallow checkout cannot run this gate. Use fetch-depth: 0."
	exit 1
fi

# The range is resolved before it is read, and a range that does not resolve is
# a failure rather than an empty answer. Without this, `2>/dev/null || true`
# below turned "this range names a ref this clone does not have" into the same
# empty string as "these two commits are the same", and the branch below then
# printed `nothing to check` and exited 0. A bad RANGE= from a caller, a
# renamed branch, or a shallow checkout all took this gate green over every
# commit they were meant to judge. The floor two lines up has always refused
# the same failure in its own half, and this is that refusal for the range.
if ! git rev-list --no-merges "$RANGE" >/dev/null 2>&1; then
	echo "changelog: $RANGE does not resolve in this clone, so nothing can be judged."
	echo "  A shallow checkout or a missing remote-tracking ref does this. Use fetch-depth: 0."
	exit 1
fi

# What the push adds, rather than every commit its range spans. A merge push
# carries the whole branch it merges, and a two-dot range from the merge base
# spans all of it, so this gate judged commits the push does not add: published,
# written by someone else, and unrewritable. EXCLUDE_PUBLISHED narrows the two
# enumerations below to the commits no remote-tracking ref already holds.
# .githooks/pre-push sets it. CI never does, because a pull request is answerable
# for its whole range.
#
# The value appends to an existing `--not` and never opens a second one. A second
# `--not` reverses the first back, so `--not "$FLOOR" --not --remotes` would ask
# for every remote-tracking ref instead of excluding it. Where no `--not` is
# there yet, one is opened here; empty, it selects nothing and git ignores it.
EXCLUDED=""
[ -n "${EXCLUDE_PUBLISHED:-}" ] && EXCLUDED="--remotes"

ALL=$(git rev-list --no-merges "$RANGE" --not $EXCLUDED 2>/dev/null || true)
COMMITS=$(git rev-list --no-merges "$RANGE" --not "$FLOOR" $EXCLUDED 2>/dev/null || true)

if [ -z "$ALL" ]; then
	echo "changelog: nothing to check in $RANGE"
	exit 0
fi

if [ -z "$COMMITS" ]; then
	echo "changelog: every commit in $RANGE predates the rule at $FLOOR"
	exit 0
fi

SKIPPED=$(($(printf '%s
' "$ALL" | grep -c .) - $(printf '%s
' "$COMMITS" | grep -c .)))

if [ "$SKIPPED" -gt 0 ]; then
	echo "changelog: $SKIPPED commit(s) predate the rule at $FLOOR and are not judged"
fi

# What the push adds to the news. A single entry may legitimately cover several
# commits, which is why this is a pre-push check over the whole range rather
# than a per-commit one. This one keeps the whole range whatever
# EXCLUDE_PUBLISHED says: it is the haystack a trailer is matched against, and an
# entry a merged branch added still backs the trailer that names it.
#
# A change writes its entry as a fragment in $CHANGES (ADR-20261021-arm). $CHANGELOG is
# read as well, because a release files the fragments there in the same push
# that deletes them, and a range reaching back past a release finds its entries
# in the file.
#
# The haystack is the lines the range adds net, and also every line any commit
# in the range added to a news file the range leaves added or changed. When a
# later commit on a branch rewords an entry, the earlier commit's trailer is
# missing from the net diff, and an exception for that commit would name a sha
# that a squash merge deletes. A file the range deletes backs no trailer, so
# news that a later commit withdraws whole is still refused. The net diff stays
# in because a commit that rewrites one line of a wrapped entry adds that line
# alone, and only the net diff holds the whole sentence.
ADDED=$(git diff "$RANGE" -- "$CHANGES" "$CHANGELOG" 2>/dev/null | sed -n 's/^+//p' || true)
NEWS=$(git diff --name-only --no-renames --diff-filter=AM "$RANGE" -- "$CHANGES" "$CHANGELOG" 2>/dev/null || true)
if [ -n "$NEWS" ]; then
	# A fragment name carries no space: scripts/check-changes.sh, run above,
	# refuses one that does.
	# shellcheck disable=SC2086
	ADDED="$ADDED
$(git log -p --no-renames --format= "$RANGE" -- $NEWS 2>/dev/null | sed -n 's/^+//p' || true)"
fi

# Rule 4 of changelog-discipline.md: the trailer is the entry's first sentence,
# in prose. A trailer lives in a commit message, where a backtick is a backtick,
# and the entry lives in markdown, where the same words carry code spans. Byte
# equality would make the rule unsatisfiable unless the author typed markdown
# into the trailer, so the comparison drops code-span markers from both sides
# and compares the sentence. A word that differs still fails.
# The Changelog: trailer of commit $1. A squash merge on GitHub appends a
# Co-authored-by paragraph naming the bot that opened the pull request, and git
# then reads that paragraph as the trailers and never sees the Changelog: line
# above it. A last paragraph that names only bots is dropped before parsing.
changelog_trailer() {
	git log -1 --format=%B "$1" |
		awk '
			{ line[NR] = $0 }
			END {
				n = NR
				while (n > 0 && line[n] ~ /^[ \t]*$/) n--
				start = n
				while (start > 0 && line[start] !~ /^[ \t]*$/) start--
				botonly = n > start
				for (i = start + 1; i <= n; i++)
					if (line[i] !~ /^[Cc]o-[Aa]uthored-[Bb]y:.*\[bot\]/) botonly = 0
				if (botonly) n = start
				for (i = 1; i <= n; i++) print line[i]
			}' |
		git interpret-trailers --parse |
		sed -n 's/^Changelog:[[:space:]]*//p' | head -1 | sed 's/[[:space:]]*$//'
}

sentence() {
	printf '%s' "$1" | tr -d '`' | tr -s '[:space:]' ' '
}

HAYSTACK_ALL=$(sentence "$ADDED")

FAILED=0

# A trailer is frozen in history and $CHANGELOG is not, so a range that reaches
# back far enough eventually holds a commit the two can never be made to agree
# on. $EXCEPTIONS records those, one line per commit, with the reason. Each
# line is checked against the commit it names below, so it fails once it stops
# being true, on the model of scripts/writing-style.baseline.
if [ ! -f "$EXCEPTIONS" ]; then
	echo "✗ $EXCEPTIONS is missing. This gate reads it by name and cannot run without it."
	exit 1
fi

exception_kind() {
	awk -v sha="$1" '
		/^[[:space:]]*#/ { next }
		NF == 0 { next }
		$1 == sha { print $2 }
	' "$EXCEPTIONS"
}

# Every line is verified whether or not its commit is in this range. A line
# whose commit is not judged tonight would otherwise go stale unseen.
while read -r SHA KIND REST; do
	# A checkout that materialises this file with CRLF leaves a carriage return
	# on the last field of every line. Without stripping it the blank separator
	# line reads as a one-character sha and the gate refuses its own exceptions
	# file, on a clone where nothing is wrong. This script is /bin/sh, so the
	# bash parameter expansion that would do it is not available.
	SHA=$(printf '%s' "$SHA" | tr -d '\r')
	KIND=$(printf '%s' "$KIND" | tr -d '\r')
	REST=$(printf '%s' "$REST" | tr -d '\r')

	case "$SHA" in "" | \#*) continue ;; esac

	# The full sha, because the loop below matches on what git rev-list prints.
	# A short one would name a real commit and exempt nothing.
	case "$SHA" in
	????????????????????????????????????????) ;;
	*)
		echo "✗ $EXCEPTIONS names $SHA. A line carries the full 40-character sha"
		FAILED=1
		continue
		;;
	esac

	if ! git cat-file -e "$SHA^{commit}" 2>/dev/null; then
		echo "✗ $EXCEPTIONS names $SHA, which is not a commit in this clone"
		FAILED=1
		continue
	fi

	if [ -z "$REST" ]; then
		echo "✗ $EXCEPTIONS gives no reason for $SHA. A record with no reason records nothing"
		FAILED=1
		continue
	fi

	LINE_TRAILER=$(changelog_trailer "$SHA")

	case "$KIND" in
	no-trailer)
		if [ -n "$LINE_TRAILER" ]; then
			echo "✗ $EXCEPTIONS records $SHA as carrying no trailer, and it carries one. Delete the line"
			FAILED=1
		fi
		;;
	trailer-wrong)
		if [ -z "$LINE_TRAILER" ]; then
			echo "✗ $EXCEPTIONS records a wrong trailer on $SHA, which carries none. Delete the line"
			FAILED=1
			continue
		fi

		NORMALISED=$(sentence "$LINE_TRAILER")

		case "$HAYSTACK_ALL" in
		*"$NORMALISED"*)
			echo "✗ $EXCEPTIONS records a wrong trailer on $SHA, and the news now carries it. Delete the line"
			FAILED=1
			;;
		esac
		;;
	*)
		echo "✗ $EXCEPTIONS gives $SHA the kind '$KIND'. It is no-trailer or trailer-wrong"
		FAILED=1
		;;
	esac
done <"$EXCEPTIONS"

fail() {
	echo "✗ $1"
	FAILED=1
}

for SHA in $COMMITS; do
	SUBJECT=$(git log -1 --format=%s "$SHA")
	SHORT=$(git log -1 --format=%h "$SHA")

	# Bump-worthy prefixes, from git-discipline.md. A prefix outside this set
	# may still carry a trailer, and when it does the trailer is checked; it is
	# only the requirement that is limited to these.
	case "$SUBJECT" in
	feat:* | feat\(*\):* | fix:* | fix\(*\):* | perf:* | perf\(*\):* | schema:* | schema\(*\):* | infra:* | infra\(*\):*)
		REQUIRED=yes
		;;
	*)
		REQUIRED=no
		;;
	esac

	TRAILER=$(changelog_trailer "$SHA")

	# $EXCEPTIONS is verified above, line by line, against the commit each line
	# names. Reaching here means the reason still holds, so the commit is
	# named rather than passed over in silence.
	KIND=$(exception_kind "$SHA")

	if [ -n "$KIND" ]; then
		echo "changelog: $SHORT is recorded in $EXCEPTIONS as $KIND"
		continue
	fi

	if [ -z "$TRAILER" ]; then
		if [ "$REQUIRED" = yes ]; then
			fail "$SHORT $SUBJECT
    has no 'Changelog:' trailer. Every feat, fix, perf, schema and infra commit
    carries one, because deciding what a user observes is the author's job and
    nobody else can do it later. Use 'Changelog: none' when nothing outside this
    repository can observe the change."
		fi
		continue
	fi

	if [ "$TRAILER" = none ]; then
		continue
	fi

	# The trailer is the entry's first sentence, so it must appear in what the
	# push added. Substring rather than exact line, since an entry wraps.
	NORMALISED=$(sentence "$TRAILER")
	HAYSTACK="$HAYSTACK_ALL"

	case "$HAYSTACK" in
	*"$NORMALISED"*) ;;
	*)
		fail "$SHORT $SUBJECT
    carries 'Changelog: $TRAILER'
    but nothing matching it was added to a fragment in $CHANGES in this push.
    The trailer is the entry's first sentence, which is what keeps the two from
    drifting. make changelog-new NAME=<words> writes a fragment to put it in."
		;;
	esac
done

if [ "$FAILED" -eq 1 ]; then
	echo ""
	echo "changelog gates failed. See .agents/rules/changelog-discipline.md"
	exit 1
fi

echo "✓ changelog gates passed"
