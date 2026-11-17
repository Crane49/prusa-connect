#!/bin/sh
# The commit-message rules of .agents/rules/git-discipline.md, enforced. Each
# check fails, none warns.
#
#   1. an em dash in a commit message
#   2. a subject over 72 characters, not counting the ` (#123)` that a
#      squash merge on GitHub appends
#   3. a body over 20 lines of prose
#   4. the contrast frame over 6.00 per thousand words, across a window
#   5. a subject that does not begin with a prefix from the table
#   6. a Co-Authored-By: line anywhere in the message, except one naming a
#      [bot] account, which GitHub appends to a bot's squash merge
#   7. a fix: commit with no Fixes: trailer, or one whose value is not a
#      twelve-character sha and the repaired subject, or the null `none`
#
# Two modes, because a commit message is written once and never revised.
#
#   --message FILE   judge the message being written. The commit-msg hook.
#   [RANGE]          judge every commit in a range, and check 4 over the
#                    window ending at its tip. `make commit-style-check`, the
#                    pre-push hook and CI. RANGE defaults to @{upstream}..HEAD.
#
# EXCLUDE_PUBLISHED in the environment narrows range mode to the commits no
# remote-tracking ref already holds. See where it is read, below.
#
# Range mode exists for the same reason scripts/check-changelog.sh has one: a
# clone that never ran `make install-hooks` commits with no hook at all, so a
# rule that lives only in a hook is not a rule.
#
# COST. Every commit pays checks 1 to 3, and an author who fights a slow hook
# turns it off. So the message path spawns three processes and no more: this
# shell, one `git config`, and one `awk` that reads the message once. It writes
# no temporary file: `mktemp -d` alone measured 1.34 seconds on the Windows
# workstation this was written on, where an empty `sh` script measures 0.68
# seconds. Earlier research has the paired run. Range mode reads every
# message through the same single awk, so a push of forty commits costs the same
# three processes as a push of one.
#
# WHY EACH CHECK IS A CAP AND NOT A RATCHET. Checks 4 to 9 of
# scripts/check-writing-style.sh ratchet against scripts/writing-style.baseline:
# a count that rises fails, and a count that falls fails until the line is
# corrected. That form cannot work here, and the reason is mechanical. A ratchet
# needs a previous value for the object it judges. A file has one. A commit
# message has none: it is written once, git refuses to revise a pushed one, and
# this repository shares a branch, so a sweep would rewrite every commit after
# the one it touched. A baseline over a rate that every new commit moves also
# has no fixed point, because the correction to the line is itself a commit
# whose body enters the corpus and moves the rate again. So every check here is
# an absolute refusal or a hard cap, and none reads a baseline file.
#
# WHERE EACH NUMBER COMES FROM. Earlier research measured 473 commit bodies here
# against 67,061 in golang/go, 6,911 in rust-lang/rfcs, 7,717 in
# kubernetes/enhancements and 404 in golang/proposal.
#
#   72 characters   git-discipline.md already stated it and nothing read a
#                   commit message. 28% of the history here is over, against
#                   9% of golang/go's last 5,000
#   20 prose lines  the p90 of golang/go's last 5,000 bodies. Ours is 34, and
#                   the median body here is 18 lines against their 9. golang/go
#                   pastes benchmark output flush left, so their 20 counts
#                   pasted lines this check does not, which makes the cap the
#                   forgiving reading of the control
#   6.00 /kw        not a control-derived number. It is ADR-20260826-gtb rule 1's own
#                   budget for the same construction in a markdown file. The
#                   control sits at 0.44 /kw and this corpus at 5.55, so 6.00
#                   is a ceiling that stops the rate rising, and it is meant to
#                   be lowered by hand as the corpus improves
#
# WHAT IS DELIBERATELY NOT GATED, each declined against a measurement in
# earlier research:
#
#   - the contrast frame inside one body. Median 1, p90 3, max 6 over 473
#     bodies. A cap at 4 flags 1% of the last 100 commits, so no per-message
#     threshold sees the largest defect in the corpus. Check 4 gives it the
#     only denominator it has, which is many commits
#   - a rate of any kind on one body. The median body is 186 prose words and
#     the longest is 587. ADR-20260826-gtb rule 1 puts its own floor at 800 words, so
#     a per-body rate is a count wearing the clothes of a rate. ADR-20260826-zbh found
#     the same about comment files, and it holds harder here
#   - a sentence over 30 words. 26% of the sentences here are over, and no
#     control can say whether that is out of band: golang/go pastes benchmark
#     tables with no fence and no indent, and they read as 2,748-word sentences
#   - a banned word or phrase list. Earlier research scored this corpus on
#     Kobak et al.'s 407 annotated style words and it came out cleanest of five
#     corpora, three of them human. Such a list bans words nobody here writes
#   - prose quality, tone, or whether the body is honest. A checker that passes
#     bad prose approves it
#
# WHERE EACH PATTERN IS ANCHORED. Check 5 exists because an unanchored prefix
# test passed `@ docs(research):` four times. Every other pattern here was read
# for the same weakness, and each one is listed with the answer, because a
# pattern nobody has justified is a pattern nobody can review.
#
#   the em dash            unanchored on purpose. One anywhere is refused
#   the scissors line      anchored at column 1 by index()==1. A body line
#                          quoting a scissors line is prose
#   a comment line         anchored at column 1 by substr()
#   a trailer              anchored by ^. A trailer indented by one to three
#                          spaces is not stripped and counts as prose. Git does
#                          not write one that way and no message here has one
#   a fence, an indent     anchored by ^, leading whitespace allowed on a fence
#   the subject bytes      per character, for the continuation-byte subtraction
#   the contrast frame     unanchored on purpose, and it carries no word
#                          boundary because POSIX awk has none. Measured over
#                          all 473 messages: `[a-z]?rather than[a-z]?` matches
#                          only `rather than` and `Rather than`, so the boundary
#                          buys nothing here
#
# NON-NATIVE READERS. Most authors here write English at C1 and are not native
# speakers. Liang et al., Patterns 2023, found GPT detectors misclassify over
# half of non-native writing. Every check here counts a character, a length or
# a fixed two-word phrase, and none scores a sentence for how it is phrased, so
# none can penalise plain non-native English.
#
# HISTORY IS NOT REWRITTEN. Every message already in the tree stays as it is.
# FLOOR below is the commit the rule starts at, and range mode judges nothing
# at or before it, on scripts/check-changelog.sh's model. A check added after
# the first five carries a floor of its own, because rule 11 starts each rule at
# the commit that added it and FLOOR is where the first five started.
set -e

# Parameter expansion and not `dirname`, because a command substitution forks a
# subshell and a fork measured 0.5 seconds on the workstation this runs on.
case "$0" in
*/*) cd "${0%/*}/.." ;;
esac

# The commit that added this gate. Nothing at or before it is judged.
FLOOR=28bb30b

# The commit check 6 starts at, on the same model. It is later than FLOOR
# because the check is later: git-discipline.md has refused a Co-Authored-By
# line since 2026-08-24 and nothing read one, so messages between the two
# floors carry the line and are not judged for it. Message mode judges every
# message, whatever the floors say, because a message being written is not
# history yet.
ATTRIB_FLOOR=48576e6

# The commit check 7 starts at, on the same model and for the same reason.
# ADR-20260831-wab rule 7 states the trailer, and 78 fix: commits on this branch predate
# it. Judging them means asking an author to name a defect somebody else
# introduced, from a message that cannot be revised.
FIXES_FLOOR=5831177

SUBJECT_MAX=72
BODY_MAX=20
CONTRAST_MAX=600 # 6.00 per thousand words, in hundredths, so sh can compare
WINDOW=50
WINDOW_MIN_WORDS=800

# ADR-20260826-gtb rule 7: a marker a gate scans as bytes is named by its code point and
# never spelled, because a shell script has no fence to quote it inside. U+2014
# is built in awk from its three UTF-8 bytes, in BEGIN, so this file never holds
# one and no subshell is forked to make one.

# git writes its own instructions into the message file with this character, and
# `git commit -v` puts the whole diff below a scissors line built from it. The
# hook runs before git's cleanup, so both have to go, or a verbose commit is
# judged on its diff.
COMMENT_CHAR=$(git config core.commentchar 2>/dev/null || true)
[ -n "$COMMENT_CHAR" ] || COMMENT_CHAR='#'

# JUDGE reads one message file when LABEL names it, and otherwise a stream of
# messages, each opened by a line holding \001 and the label to report it under.
# \001\002 opens a message that only feeds check 4's window, so a commit inside
# both the range and the window is reported once.
#
# One awk program, and the whole gate. It was two, and merging them removed a
# process that every commit was paying for.
#
# EXPECT is how many messages the caller says are there. A run that reads fewer
# fails, and that is what stops this gate passing vacuously. A check that
# silently reads nothing reports nothing, and reporting nothing has gone green
# on this branch seven times.
JUDGE='
# attributed says whether check 6 judges this message. A message being written
# is always judged: it is not history and it can still be corrected. A commit in
# range mode is judged when it is in NEWSET, which the caller fills from
# ATTRIB_FLOOR.
function attributed(   f) {
	if (LABEL != "") return 1
	if (NEWSET == "") return 0
	split(label, f, " ")
	return index(NEWSET, " " f[1] " ") > 0
}
# The same question for check 7, against its own floor. A message being written
# is always judged: it is not history and it can still be corrected.
function repairs(   f) {
	if (LABEL != "") return 1
	if (FIXSET == "") return 0
	split(label, f, " ")
	return index(FIXSET, " " f[1] " ") > 0
}
# The trailer value is a twelve-character abbreviated sha and the subject it
# repairs, or the null. Written out rather than as a bounded repetition, because
# an interval in an ERE is not portable across the awks this runs under.
function fixes_shape(v,   hex) {
	if (v == "none") return 1
	if (v !~ /^[0-9a-f]+ \(".+"\)$/) return 0
	hex = v
	sub(/ .*/, "", hex)
	return length(hex) == 12
}
function flush(   len, cont, line) {
	if (!have) return
	if (inwin) { wmsgs++; return }
	msgs++
	if (nprose == 0 && subject == "") {
		bad = 1
		printf "%s %s\n", CROSS, label
		print "    has no message. A commit says what changed and why."
		return
	}
	if (emdash) {
		bad = 1
		printf "%s %s\n", CROSS, label
		print "    carries an em dash. writing-style.md refuses one in prose, UI copy,"
		print "    comments and commit messages alike. Write a plain hyphen, a comma, or"
		print "    a new sentence."
	}
	# The caller forces LC_ALL=C, so length() counts bytes. A UTF-8 continuation
	# byte is never a character of its own, so subtracting them gives the
	# character count, and a subject carrying a Czech accent is not charged twice
	# for it. The locale is forced because that byte range inside a bracket
	# expression is a collation error under a UTF-8 locale in gawk.
	#
	# A squash merge on GitHub appends ` (#123)` to the subject the pull request
	# was green with, so a subject of 70 characters reaches main at 77 and fails
	# there, where nobody can correct it. The merge wrote the suffix and the
	# author did not, so it is not counted. Anchored at the end, so a number
	# written inside the subject still counts. No apostrophe in this program:
	# it sits inside a single-quoted shell string.
	line = subject
	sub(/ \(#[0-9]+\)$/, "", line)
	len = length(line)
	cont = gsub(/[\200-\277]/, "", line)
	len -= cont
	if (len > SUBJECT_MAX) {
		bad = 1
		printf "%s %s\n", CROSS, label
		printf "    has a subject of %d characters, over %d. Name what changed, and move\n", len, SUBJECT_MAX
		print "    the reason into the body."
	}
	# Check 5. Anchored at the first character, because the defect this exists
	# for is a stray character in front of a correct prefix. `@ docs(research):`
	# and `@` alone on the subject line both reached this branch four times on
	# `restructure`, from a PowerShell here-string opener leaking into the Bash
	# tool. An unanchored test passes every one of them, and so does the
	# `case` in .githooks/commit-msg, which skips a subject it cannot match and
	# therefore skipped the changelog trailer too.
	#
	# The list is the prefix table in git-discipline.md, and
	# internal/arch/commit_style_test.go refuses a prefix in one and not the
	# other. Merge, Revert, fixup! and squash! are git own subjects.
	if (subject !~ /^(checkpoint|feat|fix|refactor|wip|docs|style|perf|test|infra|schema|chore)(\([A-Za-z0-9._\/-]+\))?: ./ &&
	    subject !~ /^(Merge |Revert |fixup! |squash! )/) {
		bad = 1
		printf "%s %s\n", CROSS, label
		printf "    opens with %s, which is not a prefix from git-discipline.md.\n", substr(subject, 1, 16)
		print "    A subject starts at its first character with one of checkpoint, feat,"
		print "    fix, refactor, wip, docs, style, perf, test, infra, schema or chore,"
		print "    an optional (scope), then a colon and a space."
	}
	# Check 6. git-discipline.md refuses the line under Attribution, and the
	# cost of it is not an untidy log. Git reads only the last paragraph of a
	# message as trailers, so a Co-Authored-By paragraph written after the
	# Changelog: trailer leaves `%(trailers:key=Changelog)` empty and the push
	# gate and CI find no trailer at all. Three commits on `restructure` are in
	# that state and repairing them rewrites a shared branch.
	if (attributed()) {
		amsgs++
		if (coauth) {
			bad = 1
			printf "%s %s\n", CROSS, label
			print "    carries a Co-Authored-By: line, which git-discipline.md refuses under"
			print "    Attribution. One also costs a message its Changelog: trailer: git reads"
			print "    only the last paragraph as trailers, so a Co-Authored-By paragraph after"
			print "    the trailer leaves nothing for the push gate to read. Delete the line."
		}
	}
	# Check 7. ADR-20260831-wab rule 7. Two prose counts of one class exist in this log
	# and disagree, because nothing groups a repair with what it repaired.
	# `none` is the null and a real answer: a fix with no commit behind it is a
	# defect that shipped in the first version of the thing.
	if (repairs() && subject ~ /^fix(\([A-Za-z0-9._\/-]+\))?: /) {
		fmsgs++
		if (fixes == "") {
			bad = 1
			printf "%s %s\n", CROSS, label
			print "    is a fix: with no Fixes: trailer. Name the commit that introduced the"
			print "    defect, so the class can be counted by a query instead of by a person:"
			print "        Fixes: <12 hex> (\"<subject of that commit>\")"
			print "        Fixes: none"
			print "    `none` is the answer where the defect was there from the first version."
		} else if (!fixes_shape(fixes)) {
			bad = 1
			printf "%s %s\n", CROSS, label
			printf "    has Fixes: %s, which is neither `none` nor a twelve-character sha and\n", fixes
			print "    the subject it repairs. git log -n 1 --format=%h <sha> prints the sha,"
			print "    and the subject goes in brackets and double quotes after it."
		}
	}
	if (nprose > BODY_MAX) {
		bad = 1
		printf "%s %s\n", CROSS, label
		printf "    has a body of %d prose lines, over %d. Say why this change and why now.\n", nprose, BODY_MAX
		print "    An argument for a design belongs in an ADR, a behaviour a user observes"
		print "    in the changelog, a measurement in the ADR or the pull request, and work"
		print "    still owed in a GitHub issue. Pasted evidence is free: indent a gate output by four"
		print "    spaces or fence it, and it does not count."
	}
}
# generated says whether this message was written by Dependabot, whose body is
# release notes it pasted. git-discipline.md rule 2 does not charge pasted
# evidence, so such a body is skipped whole and only the subject is judged.
function generated(   f) {
	if (BOTSET == "") return 0
	split(label, f, " ")
	return index(BOTSET, " " f[1] " ") > 0
}
function reset() { have = 0; inwin = 0; label = ""; subject = ""; nprose = 0; emdash = 0; coauth = 0; fixes = ""; body = 0; infence = 0; cut = 0; bot = 0 }
BEGIN {
	reset()
	EMDASH = sprintf("%c%c%c", 226, 128, 148)
	if (LABEL != "") { have = 1; label = LABEL }
}
LABEL == "" && /^\001\002/ { flush(); reset(); have = 1; inwin = 1; label = substr($0, 3); bot = generated(); next }
LABEL == "" && /^\001/ { flush(); reset(); have = 1; label = substr($0, 2); bot = generated(); next }
!have { next }
cut { next }
index($0, COMMENT_CHAR " ------------------------ >8 ------------------------") == 1 { cut = 1; next }
substr($0, 1, length(COMMENT_CHAR)) == COMMENT_CHAR { next }
# GitHub appends the line naming a bot to the squash merge of a pull request
# that bot opened, and nobody writes that line by hand.
/^[Cc]o-[Aa]uthored-[Bb]y:/ { if ($0 !~ /\[bot\]/) coauth = 1; next }
/^[Ff]ixes:[ \t]*/ { fixes = $0; sub(/^[Ff]ixes:[ \t]*/, "", fixes); next }
/^[Cc]hangelog:/ || /^[Ss]igned-off-by:/ { next }
/^[Rr]eviewed-by:/ || /^[Tt]ested-by:/ || /^[Aa]cked-by:/ { next }
/^[Rr]eported-by:/ || /^Change-Id:/ { next }
bot && body { next }
{
	if (index($0, EMDASH) > 0) emdash = 1
	if (!body) {
		if ($0 ~ /^[ \t]*$/) { if (subject != "") body = 1; next }
		if (subject == "") { subject = $0; next }
		# A subject wrapped over two lines is still the subject. The length
		# check reads the first line, so nothing here needs to split it.
		next
	}
	# Pasted evidence is a fenced block, or a block indented by four spaces or a
	# tab, which is git own convention for one. A gate going red, a benchmark, a
	# stack trace or a diff is what a reader cannot reconstruct, so it costs
	# nothing against the cap. Prose about it is prose.
	if ($0 ~ /^[ \t]*```/) { infence = !infence; next }
	if (infence) next
	if ($0 ~ /^(    |\t)/) next
	if ($0 ~ /^[ \t]*$/) next
	nprose++
	if (inwin) {
		line = $0
		words += gsub(/[A-Za-z][A-Za-z]*/, "", line)
		line = $0
		frame += gsub(/[Rr]ather than/, "", line)
		line = $0
		frame += gsub(/,[ \t]+not[ \t]+[a-z]/, "", line)
	}
}
END {
	flush()
	if (msgs != EXPECT + 0) {
		printf "%s the gate was handed %d message(s) and read %d, so it proved nothing.\n", CROSS, EXPECT, msgs
		exit 1
	}
	printf "commit style: %d message(s) judged, %d in the window\n", msgs, wmsgs
	# Check 6 has a floor of its own, so a run can judge every message for the
	# first five and none for this one. That has to be said, or a green line
	# reads as coverage the run did not have.
	if (WINDOWED != "") {
		printf "commit style: %d message(s) judged for attribution, at or after the check 6 floor\n", amsgs
		printf "commit style: %d fix: message(s) judged for a Fixes: trailer, at or after the check 7 floor\n", fmsgs
	}
	# Check 4 runs over a window, and a window needs many commits, so the
	# message path skips it. The push and CI run it.
	if (WINDOWED != "") {
		if (words == 0)
			print "commit style: the window holds no commit at or after the floor, so check 4 refused nothing"
		else if (words < MINW) {
			printf "commit style: the window holds %d prose words, under %d, so check 4 counted %d and refused nothing\n", words, MINW, frame
			print "  A rate under that floor is a count wearing the clothes of a rate."
		} else {
			rate = int(frame * 100000 / words)
			printf "commit style: the contrast frame runs at %d.%02d per thousand words over %d prose words\n", rate / 100, rate % 100, words
			if (rate > CONTRAST_MAX) {
				bad = 1
				printf "%s the contrast frame is over the budget of %d.00 per thousand words.\n", CROSS, CONTRAST_MAX / 100
				printf "    %d occurrences of rather-than and comma-not in %d prose words across\n", frame, words
				print "    the window. ADR-20260826-gtb rule 1: name the losing option and say why it lost"
				print "    in the same paragraph, or state the choice flatly and drop the contrast."
			}
		}
	}
	if (bad) {
		print ""
		print "commit style gates failed. See .agents/rules/git-discipline.md"
		exit 1
	}
	printf "%s commit style gates passed\n", TICK
}
'

# judge <expect> <windowed> <label> [file]. LC_ALL=C is what makes the byte
# range in the subject-length rule legal, and it is why length() counts bytes.
judge() {
	LC_ALL=C awk -v SUBJECT_MAX="$SUBJECT_MAX" -v BODY_MAX="$BODY_MAX" \
		-v CONTRAST_MAX="$CONTRAST_MAX" -v MINW="$WINDOW_MIN_WORDS" \
		-v COMMENT_CHAR="$COMMENT_CHAR" -v CROSS="$CROSS" -v TICK="✓" \
		-v NEWSET="${NEWSET:-}" -v FIXSET="${FIXSET:-}" -v BOTSET="${BOTSET:-}" \
		-v EXPECT="$1" -v WINDOWED="$2" -v LABEL="$3" "$JUDGE" ${4:+"$4"}
}

CROSS=✗

usage() {
	echo "usage: $0 [--message FILE | RANGE]" >&2
	exit 2
}

# The second half of check 7, and message mode only. The shape is judged in awk
# for every message; whether the sha names a commit this branch already holds
# needs git, and range mode would then fork once per commit for an answer that
# cannot change after the fact. Message mode is the moment an author can still
# correct it, and it is one message.
fixes_resolve() {
	sha=$(sed -n 's/^[Ff]ixes:[ \t]*//p' "$1" | head -1 | cut -d' ' -f1)
	[ -n "$sha" ] && [ "$sha" != none ] || return 0

	if ! git cat-file -e "$sha^{commit}" 2>/dev/null; then
		echo "$CROSS Fixes: names $sha, which is not a commit in this clone."
		echo '  Name the commit that introduced the defect, or the null `none` where it'
		echo '  was there from the first version. See git-discipline.md rule 12.'
		return 1
	fi

	if ! git merge-base --is-ancestor "$sha" HEAD 2>/dev/null; then
		echo "$CROSS Fixes: names $sha, which is not an ancestor of HEAD."
		echo "  A commit cannot repair one that is not behind it. Check the sha."
		return 1
	fi

	return 0
}

case "${1:-}" in
--message)
	[ -n "${2:-}" ] || usage
	[ -f "$2" ] || {
		echo "$CROSS $2 does not exist, so the gate read no message."
		exit 1
	}
	judge 1 "" "the message being written" "$2" || exit 1
	fixes_resolve "$2" || exit 1
	exit 0
	;;
-h | --help) usage ;;
esac

# Range mode.
RANGE="${1:-}"
if [ -z "$RANGE" ]; then
	if ! git rev-parse --abbrev-ref '@{upstream}' >/dev/null 2>&1; then
		echo "commit style: no upstream for this branch, so there is no push to check"
		exit 0
	fi
	RANGE="@{upstream}..HEAD"
fi

if [ -z "$FLOOR" ] || ! git cat-file -e "$FLOOR^{commit}" 2>/dev/null; then
	echo "$CROSS the floor '$FLOOR' is not a commit in this clone, so nothing can be judged."
	echo "  A shallow checkout cannot run this gate. Use fetch-depth: 0."
	exit 1
fi

if [ -z "$ATTRIB_FLOOR" ] || ! git cat-file -e "$ATTRIB_FLOOR^{commit}" 2>/dev/null; then
	echo "$CROSS the check 6 floor '$ATTRIB_FLOOR' is not a commit in this clone."
	echo "  Check 6 would then judge nothing while the run still printed a tick."
	exit 1
fi

if [ -z "$FIXES_FLOOR" ] || ! git cat-file -e "$FIXES_FLOOR^{commit}" 2>/dev/null; then
	echo "$CROSS the check 7 floor '$FIXES_FLOOR' is not a commit in this clone."
	echo "  Check 7 would then judge nothing while the run still printed a tick."
	exit 1
fi

# The same refusal the two floors above carry, for the range. EXPECT is what
# stops this gate passing vacuously, and EXPECT is JUDGEABLE, and JUDGEABLE is
# `git rev-list --count ... 2>/dev/null || echo 0` over this range. So a range
# that does not resolve set the floor to 0, fed awk 0 messages, and `0 != 0` is
# false: the arming check was computed by the command it was arming, and it
# reported a tick over five checks that had read nothing.
# `sh scripts/check-commit-style.sh origin/no-such-branch..HEAD` printed
# `0 message(s) judged` and `commit style gates passed` in the same run.
if ! git rev-list --no-merges "$RANGE" >/dev/null 2>&1; then
	echo "$CROSS $RANGE does not resolve in this clone, so nothing can be judged."
	echo "  A shallow checkout or a missing remote-tracking ref does this. Use fetch-depth: 0."
	exit 1
fi

# What the push adds, rather than every commit its range spans. A merge push
# carries the whole branch it merges, and a two-dot range from the merge base
# spans all of it, so this gate judged commits the push does not add: published,
# written by someone else, and unrewritable. EXCLUDE_PUBLISHED narrows the
# enumeration below to the commits no remote-tracking ref already holds.
# .githooks/pre-push sets it. CI never does, because a pull request is answerable
# for its whole range.
#
# The value appends to an existing `--not` and never opens a second one. A second
# `--not` reverses the first back, so `--not "$FLOOR" --not --remotes` would ask
# for every remote-tracking ref instead of excluding it. Where no `--not` is
# there yet, one is opened here; empty, it selects nothing and git ignores it.
EXCLUDED=""
[ -n "${EXCLUDE_PUBLISHED:-}" ] && EXCLUDED="--remotes"

# The set check 6 judges, as %h so it matches the label the same format writes.
# Range mode reads it; message mode judges every message and never looks.
NEWSET=" $(git log --no-merges --format=%h "$RANGE" --not "$ATTRIB_FLOOR" $EXCLUDED 2>/dev/null | tr '\n' ' ')"

# The same set for check 7, against its own floor.
FIXSET=" $(git log --no-merges --format=%h "$RANGE" --not "$FIXES_FLOOR" $EXCLUDED 2>/dev/null | tr '\n' ' ')"

ALL=$(git rev-list --count --no-merges "$RANGE" --not $EXCLUDED 2>/dev/null || echo 0)
JUDGEABLE=$(git rev-list --count --no-merges "$RANGE" --not "$FLOOR" $EXCLUDED 2>/dev/null || echo 0)
if [ "$ALL" -gt "$JUDGEABLE" ]; then
	echo "commit style: $((ALL - JUDGEABLE)) commit(s) at or before $FLOOR are not judged"
fi

# The window for check 4 is the last WINDOW commits at or after the floor,
# ending at the range's tip. One body has no denominator, so the frame is
# counted over many. A single commit cannot carry the window over the cap on its
# own, which is the point: an author is never argued with over one `rather
# than`, and a run of them fails the push.
#
# The window keeps its published commits, whatever EXCLUDE_PUBLISHED says. It is
# the denominator this rate needs, not the set the push is answerable for.
TIP=${RANGE##*..}
[ -n "$TIP" ] || TIP=HEAD

# The Dependabot commits in the range and the window, both reached from TIP.
BOTSET=" $(git log --no-merges --format=%h --author='^dependabot\[bot\] <' "$TIP" --not "$FLOOR" 2>/dev/null | tr '\n' ' ')"

{
	git log --no-merges --format="%x01%h %s%n%B" "$RANGE" --not "$FLOOR" $EXCLUDED 2>/dev/null || true
	git log --no-merges -n "$WINDOW" --format="%x01%x02%h %s%n%B" "$TIP" --not "$FLOOR" 2>/dev/null || true
} | judge "$JUDGEABLE" window ""
