#!/bin/sh
# The writing-style rules, enforced. Each fails, none warns.
#
#   1. an em dash anywhere
#   2. a comment describing what the code used to do
#   3. a comment addressing the reader
#   4. a heading that names nothing
#   5. a count with no items behind it
#   6. a bolded lead-in budget, per file
#   7. a word sequence repeated inside one document
#   8. a contrast budget, per file
#   9. an absent-agent budget, per file
#  12. an idiom or a phrasal verb, per file
#  13. a prose sentence over 30 words, per file
#  14. a table cell over 120 rendered characters, per file
#  15. a comment dated against the repository's own past
#  16. an emoji
#  17. a rule attributed to a person or to a date, per file
#
# Check 15 is ADR-20260826-zbh rules 1 to 5. It reads the same comment markers check 2
# reads and carries a baseline for the same reason checks 4 to 9 do.
#
# Checks 4 to 9 are ADR-20260826-gtb, measured in earlier research. Each carries a baseline
# in scripts/writing-style.baseline, because a threshold calibrated against
# human prose flags most of this tree. The baseline is a ratchet: a count that
# rises fails, and a count that falls fails until the file records the new
# number. A check with no baseline line expects zero.
#
# Checks 12 to 14 are ADR-20260826-shb rules 7, 8 and 9, and they carry no baseline
# line. They read the markdown files this push changes and hold each one to its
# own count at the base, so the ratchet is read out of git rather than out of a
# file. A tree-wide ratchet was measured at 277 baseline lines the day these
# were written: 80 files over the cell limit, 122 over the sentence limit, and
# 75 carrying an idiom, and that cost is the reason for the shape. The ratchet
# wins and the baseline may grow, so the 277 lines are a price to weigh and not
# a bar. Moving these three onto the baseline is possible later work, not a rule
# they already break.
#
# Check 17 is the attribution rule in .agents/rules/writing-style.md. It reads
# the changed files the way checks 12 to 14 do and holds each to its count at
# the base.
#
# Checks 10 and 11 are ADR-20260826-shb rules 4 and 6. Neither is built. Rule 4 needs
# the CEFR-J A1 to B2 word list, which is a licence call nobody has taken, and
# rule 6 needs the project term list rule 4 also needs.
#
# What is deliberately NOT gated: comment length, prose quality, heading case,
# and the density of any construction in a comment. A checker that passes bad
# prose licenses it, and length is the wrong measure. A twelve-line comment that
# earns its place is better than three that do not. ADR-20260826-zbh rule 8 records why
# the contrast frame at 33 times the Go standard library's rate gets a reviewer
# and not a rate cap: 32 of 32 sampled name the alternative they reject, and the
# cheapest way to pass a rate cap is to delete it.
# The whole run is one function, called on the last line, so the shell parses
# this file to its end before executing any of it. sh reads a script while it
# runs, and an edit landing mid-run moves the read offset under the reader.
# One run died on `run: command not found`, the head of run_counter read at a
# moved offset. Several agents edit this checkout at once, so the gate has to
# survive its own file being rewritten while a run is in flight.
main() {

set -e

FAILED=0
ROOT=$(cd "$(dirname "$0")/.." && pwd)
cd "$ROOT"

BASELINE=scripts/writing-style.baseline

# ADR-20260826-gtb rule 7. This script and its baseline are the only files the gate
# does not read, they are named here and printed on every run instead of hiding
# in an exclude flag, and a missing one fails, so the exclusion cannot outlive
# what it excludes.
if [ ! -f "$BASELINE" ]; then
	echo "✗ $BASELINE is missing. The gate excludes it by name and cannot verify itself."
	exit 1
fi
echo "not scanned: scripts/check-writing-style.sh, $BASELINE"

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT INT TERM

# node_modules and generated artefacts are not ours to style, and neither is a
# Python virtual environment. A package installed into one carries markdown
# licences, and two of them failed check 7 on the sentence every BSD licence
# repeats, so a developer who had run such a program once could not push.
#
# Makefile and Dockerfile* carry ADR-20260826-gtv rule 20, which is why they are here by
# name rather than by extension: neither has one. Both are prose surfaces a
# person reads, and check 1 was reported as holding the em dash rule everywhere
# while 23 shipped in three files it did not open. The Dockerfile glob is
# `Dockerfile*` because .github/dockerfiles/Dockerfile.build is one of the two
# and an exact `Dockerfile` reads neither it nor any future variant.
#
# The banner exemption of writing-style.md is a `# -- section --` separator and
# not punctuation, so it changes nothing here: check 1 reads a code point these
# files may not carry either way.
# A file git ignores is not part of the repository this gate judges, so the
# files come from git and not from a walk of the disk. A worktree kept under
# .claude/worktrees/ is a whole second copy of the tree: a walk would read it
# and report its files as this one's, while git lists a nested checkout as one
# entry and does not descend. Untracked files git does not ignore stay in, so a
# file not yet added is judged before its commit.
git ls-files --cached --others --exclude-standard > "$TMP/files"

grep -E '(\.(go|ya?ml|sh)|(^|/)Makefile|(^|/)Dockerfile[^/]*)$' "$TMP/files" |
	grep -v '^scripts/check-writing-style\.sh$' > "$TMP/code" || true

# code_grep runs grep over the code files the comment above names. /dev/null
# keeps the last batch xargs hands grep from being one file, which grep prints
# without its name, and stops an empty list from reading standard input.
code_grep() {
	tr '\n' '\0' < "$TMP/code" | xargs -0 grep -H "$@" /dev/null
}

# CLA.md is a legal text, kept word for word as the lawyer wrote it, like
# LICENSE. No check here may ask for it to be rewritten.
grep '\.md$' "$TMP/files" | grep -v '^CLA\.md$' | sort -u > "$TMP/list"

# A file can vanish between the listing and the read while another change is in
# flight, and a gate that dies on that reports nothing about the rest. A tracked
# file deleted from the disk is still listed by git.
while read -r f; do [ -r "$f" ] && echo "$f"; done < "$TMP/list" > "$TMP/md_all"

# ADR-20260826-gtb rule 8: a page a program wrote names its generator and says
# "do not edit" on one line in its head, and checks 4 to 9 skip it. The
# generator writes that line, so this exclusion cannot go stale the way a
# directory exclusion does. Checks 1 and 2 still read those pages, because a
# generator emitting an em dash is a defect in the generator.
#
# Both halves are required on one line. A bare "do not edit" appears in ordinary
# prose about a generated directory, and that let two hand-written pages under
# frontend/ out of checks 4 to 9 without anyone choosing it.
#
# An HTML comment is not that line. ADR-20260910-he3 rule 2 puts a generated region
# inside a hand-written page, opened by a `<!-- generated by ... Do not edit`
# marker that sits in the head of a short page, and the page around it is a
# person's prose this gate reads. The region itself is blanked below.
#
# One awk process reads the whole set. A `head` and a `grep` per file cost 3.6
# seconds of process start each on the machine this runs on, which over 183
# markdown files is eleven minutes to decide which pages a program wrote. awk
# opens each file itself and stops at the twentieth line. A file it cannot open
# counts as hand-written, which is the answer a `head` that fails also gives.
awk -v list="$TMP/md_all" '
	BEGIN {
		while ((getline path < list) > 0) {
			generated = 0

			for (i = 1; i <= 20; i++) {
				if ((getline line < path) <= 0) break
				if (line ~ /^[ \t]*<!--/) continue
				if (tolower(line) ~ /^[^A-Za-z]*generated by .*do not edit/) { generated = 1; break }
			}

			close(path)
			if (!generated) print path
		}
	}' < /dev/null > "$TMP/md"

# armed <what> <list> <floor>. The arming check, under the name requireScanned
# carries in internal/arch/scanned_test.go, because it answers the same
# question in the other language.
#
# A gate whose input set is empty reports green over nothing, and its green then
# says "nothing was found" and "nothing was looked at" in one word. Emptying
# $TMP/md leaves checks 5, 6, 8, 9 and 12 to 14 silent and the run printing a
# tick; only 4 and 7 speak, and then through the ratchet's own "a baseline line
# matches nothing" branch rather than through anything that watches the input.
#
# It exits rather than reporting, because every check under it reads one of
# these two lists and none of them proves anything once a list is empty.
armed() {
	count=$(wc -l < "$2" | tr -d ' ')
	[ "$count" -ge "$3" ] && return 0
	echo "✗ the gate read $count $1, under its floor of $3, so this run proved nothing."
	echo "  Either the input moved, or the walk stopped reaching it."
	exit 1
}

armed "markdown files" "$TMP/md_all" 1
armed "markdown files no program wrote" "$TMP/md" 1

# ADR-20260910-he3 rule 2: a hand-written page may carry one region a generator owns,
# from a line opening `<!-- generated by` to the line `<!-- end generated -->`.
# Checks 4 to 9 and 12 to 14 judge the person's prose, so they read a copy of
# every hand-written page with the region blanked line for line, under $HAND
# at the page's own relative path, which keeps every line number and every
# reported path the same. Checks 1, 2, 15 and 16 read the page as committed,
# region included, because a generator emitting an em dash is a defect in the
# generator. One awk writes every copy; the directories are made first, in
# one call, because a process per file is what the classifier above avoids.
HAND="$TMP/hand"
mkdir -p "$HAND"
sed -n 's|/[^/]*$||p' "$TMP/md" | sort -u | sed "s|^|$HAND/|" | xargs mkdir -p
awk -v hand="$HAND" '
	FNR == 1 { if (out != "") close(out); out = hand "/" FILENAME; gen = 0 }
	/^[ \t]*<!-- generated by / { gen = 1 }
	/^[ \t]*<!-- end generated -->/ { gen = 0; print "" > out; next }
	gen { print "" > out; next }
	{ print > out }' $(cat "$TMP/md")

# in_hand <command...>: run a check over the blanked copies. The file lists
# hold paths relative to the root, so the same list names the copies there.
in_hand() {
	(cd "$HAND" && "$@")
}

# ADR-20260826-gtb rule 7: a fenced block is not prose. Every markdown check reads one
# of these streams, so a document may quote what it forbids inside a fence.
strip_fences() {
	awk 'FNR==1 { inf = 0 }
	     /^[ \t]*```/ { inf = !inf; next }
	     inf { next }
	     { print FILENAME ":" FNR ":" $0 }' $(cat "$1")
}
strip_fences "$TMP/md_all" > "$TMP/prose_all"
in_hand strip_fences "$TMP/md" > "$TMP/prose"

report() {
	echo "✗ $1"
	FAILED=1
}

# ratchet <check-id> <results-file> <message>. Results hold "path<TAB>count".
# Sets RAISED to 1 when it reported, so the caller can print line-level hits.
ratchet() {
	RAISED=0
	grep "^$1	" "$BASELINE" 2>/dev/null | cut -f2,3 | sort > "$TMP/base" || true
	sort "$2" > "$TMP/now"
	awk -F'\t' -v id="$1" -v basefile="$TMP/base" '
		FILENAME == basefile { b[$1] = $2; next }
		{ a[$1] = $2 }
		END {
			for (p in a) {
				if (!(p in b)) printf "over\t%s (%s, no baseline line)\n", p, a[p]
				else if (a[p] + 0 > b[p] + 0) printf "over\t%s (%s, baseline allows %s)\n", p, a[p], b[p]
				else if (a[p] + 0 < b[p] + 0) printf "under\t%s\t%s\t%s\n", id, p, a[p]
			}
			for (p in b) if (!(p in a)) printf "gone\t%s\t%s\n", id, p
		}' "$TMP/base" "$TMP/now" | sort > "$TMP/delta"
	if grep -q '^over	' "$TMP/delta"; then
		report "$3"
		grep '^over	' "$TMP/delta" | cut -f2- | sed 's/^/    /'
		RAISED=1
	fi
	if grep -q '^under	' "$TMP/delta"; then
		report "$1: a count fell. Replace the line in $BASELINE with:"
		grep '^under	' "$TMP/delta" | cut -f2- | sed 's/^/    /'
	fi
	if grep -q '^gone	' "$TMP/delta"; then
		report "$1: a baseline line matches nothing. Delete it from $BASELINE:"
		grep '^gone	' "$TMP/delta" | cut -f2- | sed 's/^/    /'
	fi
}

# per_file <raw-hits> <results>. Raw hits are "path:line:text".
per_file() {
	cut -d: -f1 "$1" | sort | uniq -c | awk '{ printf "%s\t%s\n", $2, $1 }' > "$2"
}

# show <raw-hits>: the offending lines, once the ratchet has reported.
show() {
	if [ "$RAISED" -eq 1 ] && [ -s "$1" ]; then
		head -20 "$1" | sed 's/^/      /'
	fi
}

# ADR-20260826-gtb rule 7: named by code point, never spelled, because a shell script
# has no fence and check 1 scans bytes.
EMDASH=$(printf '\342\200\224')

echo "1. no em dashes..."
HITS=$( { code_grep -n -- "$EMDASH" 2>/dev/null || true
	grep -- "$EMDASH" "$TMP/prose_all" 2>/dev/null || true; } | head -20)
if [ -n "$HITS" ]; then
	report "an em dash. Write a plain hyphen, a comma, or a new sentence."
	echo "$HITS" | sed 's/^/    /'
fi

echo "2. no comment describing what the code used to do..."
# Anchored to a comment marker so prose about a migration is not caught. The
# patterns are the ones an audit actually found, not a guess.
# In markdown the anchor is an HTML comment, because `#` opens a heading there
# and `## The reviewer must be unbiased` is a lookup key, not a comment.
TENSE='(used to (be|do|have|return|accept)|previously (was|did|returned)|changed from|no longer (does|did)|before this (change|commit)|the reviewer|we used to)'
PAST="(//|#)[^\"]*\\b$TENSE\\b"
PAST_MD="<!--[^>]*\\b$TENSE\\b"
HITS=$( { code_grep -niE -- "$PAST" 2>/dev/null || true
	grep -iE -- "$PAST_MD" "$TMP/prose_all" 2>/dev/null || true; } | head -20)
if [ -n "$HITS" ]; then
	report "a comment describing what the code used to do. That is what git is for."
	echo "$HITS" | sed 's/^/    /'
fi

echo "3. no comment addressing the reader..."
# "you" inside a comment is the clearest signal of a comment written as a
# reply. Excludes doc prose, where addressing a reader is correct.
READER='(//|#)[^"]*\b(you should|you can|you may|you will|as you|note that you|if you)\b'
HITS=$(code_grep -niE -- "$READER" 2>/dev/null | grep -E "^[^:]*\.go:" | head -20 || true)
if [ -n "$HITS" ]; then
	report "a comment addressing the reader. Describe the code, not the person reading it."
	echo "$HITS" | sed 's/^/    /'
fi

echo "4. no heading that fails to describe its section..."
# ADR-20260826-gtb rule 6, which adopts WCAG 2.2 SC 2.4.6 at Level AA: "Headings and
# labels describe topic or purpose." 71.6% of screen reader users search a long
# page by its heading list first (WebAIM survey 10, 2024), so an undescriptive
# heading is an accessibility defect and not a matter of taste.
#
# What this check can see is six templates, and nothing else:
#   a. the reader's position in the document, or a bare category label
#   b. a transformation narrated between two figures of speech
#   c. the writer's activity hung on the subject after a comma, which reports
#      what was done in the place where the reader wants what was found
#   d. the document's own form or length as the subject: "in one diagram",
#      "in four sentences", "by its three files". The section is about a
#      schema or a system, and the heading names the prose instead
#   e. a subordinate clause standing alone: "Whether this tree has the
#      sprawl". It is a question with the question mark filed off, and it
#      cannot be front-loaded. "How", "why" and "when" clauses stand, because
#      this tree uses them as claims a reader can check
#   f. a third-person singular pronoun: "the code he criticizes". A heading
#      is read alone in the heading list, where no antecedent survives.
#      "they" and "them" are not caught: measured over this tree, their
#      antecedent is usually inside the same heading
#
# What it cannot see is the rule. Whether a heading describes the section under
# it is a judgement over two texts, and no pattern makes it. ADR-20260826-gtb says so on
# the record, and the same record rejects the one mechanical proxy that was
# tried and scored. A heading can pass all three templates and still fail rule
# 6, so this gate holds a floor and a reviewer holds the rule.
POSITION_HEADING='^[^:]*:[0-9]+:#{1,6} +((the |a |an )?(overview|summary|key takeaways?|conclusions?|introduction|background|notes|considerations|additional context|getting started|start here|what this is|the (whole )?point)|.*(at a glance|in one picture|worth knowing)|how (a|an|the) .+ becomes? .+|from .+ to .+|(a|the) (journey|story|life) of .+|a day in the life.*) *$'
# The participles are a closed list of reporting verbs, so a heading whose tail
# describes the code rather than the author is not caught: "one rule, checked at
# the edge" survives on purpose. An adverb or a prepositional tail after the
# participle is still the same template, so both are allowed to follow.
ACTIVITY_TAIL='(measured|tested|counted|judged|compared|weighed|priced|ranked|itemi[sz]ed|restated|proved|proven|evaluated|examined|condensed|quantified|benchmarked|assessed|summari[sz]ed|analy[sz]ed|audited|profiled|surveyed|re-derived|revisited|read properly|read as)'
ACTIVITY_HEADING="^[^:]*:[0-9]+:#{1,6} +.*, +(again |properly |plainly |fairly |honestly |precisely |exactly |seriously |today |first |second |twice |whole |in full |in detail |side by side |for comparison )*$ACTIVITY_TAIL([ ,][^|]*)? *(\\{#[a-z0-9-]+\\})? *$"
# Template d, in two branches. The first is a document form after "in": the
# unit list carries prose forms only, because "the settings in one file" names
# the system and not the writing. The second is a counted arrangement after a
# comma, "The block schema, by its three files", where any noun follows: the
# tail narrates how the writer laid the section out, whatever it counts.
FORM_UNITS='(sentence|word|line|paragraph|bullet|point|table|list|diagram|picture|figure|page|heading|question)'
FORM_NUM='(a|an|one|two|three|four|five|six|seven|eight|nine|ten|[0-9]+)'
FORM_HEADING="^[^:]*:[0-9]+:#{1,6} .*\\b(in|by its|by their) $FORM_NUM ${FORM_UNITS}s?\\b"
FORM_TAIL_HEADING="^[^:]*:[0-9]+:#{1,6} .*, +by (its|their) $FORM_NUM [a-z-]+"
# Template e. Only conjunctions that cannot open a statement. "if" is left out
# on no evidence either way: the tree holds no heading opening with it.
CLAUSE_HEADING='^[^:]*:[0-9]+:#{1,6} +(whether|although|though|because|unless|while|since) '
# Template f.
PRONOUN_HEADING='^[^:]*:[0-9]+:#{1,6} .*\b(he|she|him|his|her|hers)\b'
{ grep -iE -- "$POSITION_HEADING" "$TMP/prose" 2>/dev/null || true
	grep -iE -- "$ACTIVITY_HEADING" "$TMP/prose" 2>/dev/null || true
	grep -iE -- "$FORM_HEADING" "$TMP/prose" 2>/dev/null || true
	grep -iE -- "$FORM_TAIL_HEADING" "$TMP/prose" 2>/dev/null || true
	grep -iE -- "$CLAUSE_HEADING" "$TMP/prose" 2>/dev/null || true
	grep -iE -- "$PRONOUN_HEADING" "$TMP/prose" 2>/dev/null || true; } | sort -u > "$TMP/h.raw"
per_file "$TMP/h.raw" "$TMP/h.res"
ratchet heading "$TMP/h.res" "a heading that does not describe its section. WCAG 2.4.6: a heading describes topic or purpose. Front-load the word a reader looks for, and give the finding rather than the activity."
show "$TMP/h.raw"

echo "5. no count with nothing behind it..."
# ADR-20260826-gtb rule 2. The count is delivered where it is announced: a colon on the
# announcing sentence, or a list, table or heading within four lines.
cat > "$TMP/count.awk" <<'AWK'
FNR == 1 { pend = 0; inf = 0 }
/^[ \t]*```/ { inf = !inf; next }
inf { next }
{
  if (pend) {
    if ($0 ~ /^[ \t]*$/) { blank++; if (blank > 2) pend = 0; next }
    blank = 0
    look++
    if ($0 ~ /^[ \t]*([0-9]+[.)]|[-*+])[ \t]/ || $0 ~ /^[ \t]*\|/ || $0 ~ /^#/ || index($0, ":")) pend = 0
    else if (look >= 4) { print FILENAME ":" pend ":" ptxt; pend = 0 }
  }
  if (!pend && $0 !~ /^[ \t]*\|/ &&
      $0 ~ /(^|[^A-Za-z])(Two|Three|Four|Five|Six|Seven) things([^A-Za-z]|$)/) {
    rest = $0
    sub(/.*(Two|Three|Four|Five|Six|Seven) things/, "", rest)
    if (index(rest, ":") == 0) { pend = FNR; ptxt = $0; look = 0; blank = 0 }
  }
}
AWK
in_hand awk -f "$TMP/count.awk" $(cat "$TMP/md") > "$TMP/c.raw" 2>/dev/null || true
per_file "$TMP/c.raw" "$TMP/c.res"
ratchet count "$TMP/c.res" "a count with no items behind it. Deliver the items where you announce them, or link to where they are named and drop the count."
show "$TMP/c.raw"

# One density pass serves checks 6, 8 and 9. Words are counted outside fences
# and outside inline code, and a file under 800 prose words is too short for a
# rate to mean anything.
cat > "$TMP/density.awk" <<'AWK'
function flush() {
  if (name != "" && w >= 800 && 1000 * n / w > CAP + 0) printf "%s\t1\n", name
  name = FILENAME; w = 0; n = 0; inf = 0
}
FNR == 1 { flush() }
/^[ \t]*```/ { inf = !inf; next }
inf { next }
{
  line = $0
  gsub(/`[^`]*`/, "", line)
  gsub(/\([^) ]*\)/, "", line)
  w += gsub(/[A-Za-z][A-Za-z'-]*/, "&", line)
  if (MODE == "contrast") {
    t = line; n += gsub(/(^|[^A-Za-z])rather than([^A-Za-z]|$)/, "", t)
    t = line; n += gsub(/,[ \t]+not[ \t]+[a-z]/, "", t)
  } else if (MODE == "absent") {
    t = line
    n += gsub(/(^|[^A-Za-z])(nobody|no one|no author|no reviewer|no operator|no reader)([^A-Za-z]|$)/, "", t)
  } else if (MODE == "bold" && $0 ~ /^([ \t]*([-*+]|[0-9]+[.)])[ \t]+)?\*\*[^*]{3,90}\*\*[.,:]?[ \t]+[^ \t]/) {
    n++
  }
}
END { FILENAME = ""; flush() }
AWK

echo "6. no file over the bolded lead-in budget..."
# ADR-20260826-gtb rule 4. 5.48 per thousand words is the human control's 99th
# percentile, earlier research. A bolded lead-in is a heading that
# avoided the table of contents, the anchor and the obligation to be specific.
#
# The list marker in front of the bolding is optional, because rule 4 binds a
# paragraph that opens with a bolded phrase and a list item is one. Anchored at
# `**` alone the check flagged no file in this tree, over 782 occurrences that
# carried a bullet or a number. It flags 12 with the marker, which is the price
# measured before this landed.
#
# The checkbox form a task list writes, `- [x] **Thing.** text`, is out of
# scope. A row is an entry in a table of work rather than a paragraph, and its
# bolding is what makes the list readable at length.
in_hand awk -f "$TMP/density.awk" -v MODE=bold -v CAP=5.48 $(cat "$TMP/md") > "$TMP/b.res" 2>/dev/null || true
ratchet bold "$TMP/b.res" "a file over the bolded lead-in budget of 5.48 per thousand words. Promote the phrase to a heading, or drop the bolding."

echo "7. no word sequence repeated inside one document..."
# ADR-20260826-gtb rule 5. Twelve words, outside fences, headings, blockquotes and
# table rows, and maximal, so one long repeat is reported once.
cat > "$TMP/repeat.pl" <<'PL'
use strict; use warnings;
my $N = shift @ARGV;
for my $f (@ARGV) {
	open my $fh, '<', $f or next;
	my @l = <$fh>;
	close $fh;
	my ($inf, $inq, @w, @wl) = (0, 0);
	for my $i (0 .. $#l) {
		my $ln = $l[$i];
		if ($ln =~ /^\s*```/) { $inf = !$inf; next }
		next if $inf;
		# A quotation does not survive a paragraph break, so one unbalanced
		# quote mark cannot blank the rest of the file.
		if ($ln =~ /^\s*$/) { $inq = 0; next }
		next if $ln =~ /^\s*(#|>|\||<!--)/;
		my $t = lc $ln;
		$t =~ s/`[^`]*`/ /g;
		# Quoting one source twice is not writing the same sentence twice, so
		# double-quoted spans are dropped, across lines when a quote runs on.
		my @seg = split /"/, $t, -1;
		for my $j (0 .. $#seg) {
			$seg[$j] = ' ' x length $seg[$j] if $inq;
			$inq = !$inq if $j < $#seg;
		}
		$t = join ' ', @seg;
		$t =~ s/\[([^\]]*)\]\([^)]*\)/$1/g;
		# A record id such as ADR-20261025-k7q is one name, so it is one word.
		$t =~ s/\badr-([0-9]{8})-([a-z0-9]{3})\b/adr$1$2/g;
		$t =~ s/[^a-z0-9 ]/ /g;
		for my $tok (split /\s+/, $t) {
			next unless length $tok;
			push @w, $tok;
			push @wl, $i + 1;
		}
	}
	next if @w < $N;
	my %at;
	for my $i (0 .. $#w - $N + 1) { push @{ $at{ join ' ', @w[ $i .. $i + $N - 1 ] } }, $i }
	# Report each maximal repeat once and at its full length. A gram whose left
	# neighbour is the same at every occurrence belongs to a longer repeat and
	# is reported there; the survivors are extended right until they diverge.
	for my $k (sort keys %at) {
		my @p = @{ $at{$k} };
		next if @p < 2;
		my $same = 1;
		for my $i (@p) {
			if ($i == 0 || $w[ $i - 1 ] ne $w[ $p[0] - 1 ] || $p[0] == 0) { $same = 0; last }
		}
		next if $same;
		my $len = $N;
		while (1) {
			my $nxt;
			my $ok = 1;
			for my $i (@p) {
				if ($i + $len > $#w) { $ok = 0; last }
				$nxt = $w[ $p[0] + $len ];
				if ($w[ $i + $len ] ne $nxt) { $ok = 0; last }
			}
			last unless $ok;
			$len++;
		}
		my %ln = map { $wl[$_] => 1 } @p;
		next if keys(%ln) < 2;
		printf "%s:%s: (%d words) %s\n", $f, join(',', sort { $a <=> $b } keys %ln),
			$len, join(' ', @w[ $p[0] .. $p[0] + $len - 1 ]);
	}
}
PL
in_hand perl "$TMP/repeat.pl" 12 $(cat "$TMP/md") > "$TMP/r.raw" 2>/dev/null || true
per_file "$TMP/r.raw" "$TMP/r.res"
ratchet repeat "$TMP/r.res" "the same twelve words twice in one document. Say it once and link to it, or say the second one differently."
show "$TMP/r.raw"

echo "8. no file over the contrast budget..."
# ADR-20260826-gtb rule 1. 6.00 per thousand words is this corpus's own 90th percentile
# rounded down, earlier research. The frame is correct in the
# instance and the density is the tell, so this is a budget and not a ban.
in_hand awk -f "$TMP/density.awk" -v MODE=contrast -v CAP=6.00 $(cat "$TMP/md") > "$TMP/x.res" 2>/dev/null || true
ratchet contrast "$TMP/x.res" "a file over the contrast budget of 6.00 per thousand words. Name the alternative and why it lost, or state the choice flatly."

echo "9. no file over the absent-agent budget..."
# ADR-20260826-gtb rule 3. 2.00 per thousand words, this corpus's own 90th percentile
# rounded up, which is one instance per five hundred words.
in_hand awk -f "$TMP/density.awk" -v MODE=absent -v CAP=2.00 $(cat "$TMP/md") > "$TMP/a.res" 2>/dev/null || true
ratchet absent "$TMP/a.res" "a file over the absent-agent budget of 2.00 per thousand words. Name what does decide, or keep the sentence where the absence is the subject."

# The scope for checks 12 to 14: the markdown files this change touches, held
# to their own counts at the base. WRITING_STYLE_BASE names the base, and the
# branch's upstream is the default, which is the range
# scripts/check-changelog.sh already reads.
#
# The scope is three sets: files the commits since the base changed, tracked
# files edited in the working tree, and untracked files. The last two are read
# because a document is written, reviewed and handed over before it is
# committed. A scope of commits alone reads a new document only after it has
# landed, and this gate approved a briefing it never saw that way.
#
# A file the base does not carry is new and is held to zero. The ratchet
# exists to spare an author who did not write the prose that fails. A new file
# has no such author: every line in it is being written now, by somebody who
# can repair every hit before it lands. So a new file gets no allowance, and
# an allowance would also make deleting and recreating a file cheaper than
# fixing it.
#
# Reading the working tree costs something in a shared checkout: one agent's
# run can report another agent's open draft. That draft fails its own push on
# the same lines, so the report is early and not wrong.
#
# Where no base resolves, the three count and print and refuse nothing, and the
# line below says so in those words.
#
# The case that produced it in CI was not a shallow checkout, which is what this
# comment said before. actions/checkout leaves a pull request run on a detached
# HEAD, and a detached HEAD has no upstream whatever the depth. Reproduced both
# ways locally: after `checkout -B <branch> origin/<branch>`, which is the push
# event, `@{upstream}` resolves; on the detached HEAD of a pull request it
# resolves to nothing. So checks 12 to 14 refused nothing on any pull request.
# .github/workflows/ci.yml now sets WRITING_STYLE_BASE from the range it already
# resolves for the changelog and commit-message gates, which is the pull
# request's own base.
#
# WRITING_STYLE_BASE=none takes that branch on a machine that has an upstream,
# which is how it is probed and how the whole-tree count is read.
STYLE_BASE="${WRITING_STYLE_BASE:-}"
if [ "$STYLE_BASE" = none ]; then
	STYLE_BASE=
elif [ -z "$STYLE_BASE" ]; then
	STYLE_BASE=$(git rev-parse --verify --quiet '@{upstream}' 2>/dev/null || true)
fi
: > "$TMP/scoped"
if [ -n "$STYLE_BASE" ]; then
	{
		git diff --name-only "$STYLE_BASE" HEAD -- '*.md' 2>/dev/null
		git diff --name-only HEAD -- '*.md' 2>/dev/null
		git ls-files --others --exclude-standard -- '*.md' 2>/dev/null
	} | sed 's|^\./||' | sort -u > "$TMP/changed"
	comm -12 "$TMP/changed" "$TMP/md" > "$TMP/scoped" || true
	echo "checks 12 to 14 read $(wc -l < "$TMP/scoped" | tr -d ' ') changed file(s) against $STYLE_BASE"
else
	cp "$TMP/md" "$TMP/scoped"
	echo "checks 12 to 14 resolved no base, so they count and print and refuse nothing"
fi

# Each scoped file's content at the base, written once, and two awk runs per
# check over the whole set. A git process per file per check took over two
# minutes on the machine this runs on.
#
# One `git cat-file --batch` serves the whole set, because a `git show` per file
# costs 3.6 seconds of process start there and the scoped set is over a hundred
# files. The batch stream answers in the order it was asked, so perl walks the
# request list and the stream together. A path the base does not carry answers
# `missing`, and lands as an empty file, which is what holds a new file to zero.
mkdir -p "$TMP/basecopy"
: > "$TMP/basefiles"
: > "$TMP/map"
if [ -n "$STYLE_BASE" ]; then
	while read -r f; do
		[ -r "$f" ] && printf '%s\n' "$f"
	done < "$TMP/scoped" > "$TMP/basewanted"

	cat > "$TMP/basecopy.pl" <<'PL'
use strict;
use warnings;
my ($listfile, $dir, $mapfile, $idfile) = @ARGV;
my %newid;
if (open my $idf, '<', $idfile) {
	while (my $line = <$idf>) {
		chomp $line;
		my ($old, $new) = split /\t/, $line;
		$newid{$old} = $new;
	}
	close $idf;
}
open my $lf, '<', $listfile or die "$listfile: $!";
chomp(my @paths = <$lf>);
close $lf;
binmode STDIN;
open my $map, '>', $mapfile or die "$mapfile: $!";
my $n = 0;
for my $path (@paths) {
	$n++;
	my $copy = "$dir/$n.md";
	my $body = '';
	my $head = <STDIN>;
	# `<oid> <type> <size>` introduces a blob. Every other answer is `missing`
	# or `ambiguous` and carries no body.
	if (defined $head && $head =~ /^\S+ \S+ ([0-9]+)$/) {
		my $size = $1;
		my $eol;
		read STDIN, $body, $size;
		read STDIN, $eol, 1;
	}
	# The generated region is blanked here as it is in the working copy, so
	# the base and the tree are counted by the same rule.
	my $gen = 0;
	my $kept = '';
	for my $line (split /^/, $body) {
		$gen = 1 if $line =~ /^\s*<!-- generated by /;
		if ($gen) {
			$gen = 0 if $line =~ /^\s*<!-- end generated -->/;
			$kept .= "\n";
			next;
		}
		$kept .= $line;
	}
	$body = $kept;
	# A record renamed since the base is cited by its new id in the base count
	# too, so a line that grew by the longer id alone is not a new hit.
	$body =~ s/\bADR-([0-9]{4})(?![0-9])/exists $newid{$1} ? "ADR-$newid{$1}" : "ADR-$1"/ge;
	open my $out, '>', $copy or die "$copy: $!";
	binmode $out;
	print $out $body;
	close $out;
	print $map "$copy\t$path\n";
	print "$copy\n";
}
close $map;
PL

	# A file renamed since the base is read under its old name, so a rename
	# keeps the allowance of the prose it carries and is not held to zero.
	git diff -M --name-status --diff-filter=R "$STYLE_BASE" HEAD -- '*.md' 2>/dev/null |
		awk -F'\t' '{ print $3 "\t" $2 }' > "$TMP/renamed" || true
	awk -F'\t' 'FILENAME == ARGV[1] { old[$1] = $2; next } { print (($0 in old) ? old[$0] : $0) }' \
		"$TMP/renamed" "$TMP/basewanted" > "$TMP/baseasked"
	awk -F'\t' '$1 ~ /^docs\/decisions\/[0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9]-[a-z0-9][a-z0-9][a-z0-9]-/ && $2 ~ /^docs\/decisions\/[0-9][0-9][0-9][0-9]-/ {
		print substr($2, 16, 4) "\t" substr($1, 16, 12)
	}' "$TMP/renamed" > "$TMP/newids"
	sed "s|^|$STYLE_BASE:|" "$TMP/baseasked" |
		git cat-file --batch |
		perl "$TMP/basecopy.pl" "$TMP/basewanted" "$TMP/basecopy" "$TMP/map" "$TMP/newids" > "$TMP/basefiles"
fi

# tally <raw-hits> <out>. Raw hits are "path:line:text"; out is "path<TAB>count"
# for every path in <list>, zero included, so a file that lost its last hit is
# still comparable.
tally() {
	cut -d: -f1 "$1" | sort | uniq -c |
		awk '{ printf "%s\t%s\n", $2, $1 }' | sort > "$2"
}

# run_counter <awk-or-fn> <file-list> <out>. awk over an empty list reads
# stdin and never returns, so an empty list is answered without calling it.
run_counter() {
	: > "$3"
	[ -s "$2" ] || return 0
	# shellcheck disable=SC2046
	in_hand "$1" $(cat "$2") > "$3" 2>/dev/null || true
}

# ADR-20260826-shb rule 9. Rendered length: a link counts as its text, an image as its
# alt text, and inline code without its backticks. Counting the URL charges a
# cell for something no reader sees, so the cheapest way to pass would be to
# delete the link.
cat > "$TMP/cells.awk" <<'AWK'
FNR == 1 { inf = 0; fm = ($0 == "---") }
fm && FNR > 1 && $0 == "---" { fm = 0; next }
fm && FNR > 40 { fm = 0 }
fm { next }
/^[ \t]*```/ { inf = !inf; next }
inf { next }
$0 !~ /^[ \t]*\|/ { next }
{
  row = $0
  if (row ~ /^[ \t]*\|[ \t:|-]*\|[ \t]*$/) next
  sub(/^[ \t]*\|/, "", row)
  sub(/\|[ \t]*$/, "", row)
  n = split(row, cell, "|")
  for (i = 1; i <= n; i++) {
    t = cell[i]
    gsub(/!\[([^]]*)\]\([^)]*\)/, "\\1", t)
    while (match(t, /\[[^]]*\]\([^)]*\)/)) {
      pre = substr(t, 1, RSTART - 1)
      mid = substr(t, RSTART, RLENGTH)
      post = substr(t, RSTART + RLENGTH)
      sub(/\]\([^)]*\)$/, "", mid)
      sub(/^\[/, "", mid)
      t = pre mid post
    }
    gsub(/`/, "", t)
    gsub(/^[ \t]+|[ \t]+$/, "", t)
    if (length(t) > LIMIT + 0) printf "%s:%d: cell of %d rendered characters\n", FILENAME, FNR, length(t)
  }
}
AWK

# ADR-20260826-shb rule 8. A sentence ends at a full stop followed by a capital, and a
# heading, a table row, a blockquote, a blank line and a list marker all end
# one too. A bullet is its own sentence, so a run of them is not read as one.
#
# YAML frontmatter is not prose, and all three counters skip it. Read as prose
# it joins into one long buffer, so an epic scored worse the more decision ids
# its `decisions:` field carried, and the backlog's own rules are what put them
# there. A gate that penalises another rule teaches an author to break it.
# The 40 line cap bounds what an opener with no closing marker can swallow: a
# block that never closes would take a whole file out of the count, which is
# the failure these counters exist to refuse.
#
# A code span is replaced by the single word X and never by a space. The word
# keeps the split alive when a span opens a sentence: blanked to a space, the
# splitter saw no capital after the full stop, joined the two sentences, and
# twice sent an author to reword a paragraph whose sentences were both legal.
#
# The buffer is flushed when the next file starts, not only at END. Flushed at
# END alone, every file but the last in the batch dropped its final sentence,
# and the batch is the whole scoped set. fname carries the name the buffer
# belongs to, because at the flush FILENAME already names the next file.
cat > "$TMP/sentences.awk" <<'AWK'
function emit(s,   n, t) {
  t = s
  gsub(/^[ \t]+|[ \t]+$/, "", t)
  if (t == "") return
  n = gsub(/[A-Za-z0-9][A-Za-z0-9'._-]*/, "&", t)
  if (n > LIMIT + 0) printf "%s:%d: sentence of %d words\n", fname, ln, n
}
FNR == 1 { emit(buf); buf = ""; fname = FILENAME; inf = 0; ln = 0; fm = ($0 == "---") }
fm && FNR > 1 && $0 == "---" { fm = 0; next }
fm && FNR > 40 { fm = 0 }
fm { next }
/^[ \t]*```/ { inf = !inf; next }
inf { next }
/^[ \t]*\|/ || /^[ \t]*(#|>)/ || /^[ \t]*<!--/ || /^[ \t]*$/ { emit(buf); buf = ""; next }
/^[ \t]*([*+-]|[0-9]+[.)])[ \t]/ { emit(buf); buf = "" }
{
  line = $0
  gsub(/`[^`]*`/, " X ", line)
  gsub(/\[([^]]*)\]\([^)]*\)/, "\\1", line)
  sub(/^[ \t]*([*+-]|[0-9]+[.)])[ \t]+/, "", line)
  if (buf == "") ln = FNR
  buf = buf " " line
  while (match(buf, /[.!?]("|\))?[ \t]+[A-Z0-9(`"'*]/)) {
    emit(substr(buf, 1, RSTART))
    buf = substr(buf, RSTART + RLENGTH - 1)
    ln = FNR
  }
}
END { emit(buf) }
AWK

# ADR-20260826-shb rule 7, seeded from forms found in these files. Recall is unknown
# and the list is the whole cost of the check. Read outside fenced blocks and
# outside inline code, which is ADR-20260826-gtb rule 7: a record that states the rule
# has to be able to name what the rule forbids.
#
# Table cells are read. Check 7 skips any line opening with a pipe, and that
# hid 58 of the 59 twelve-word repeats in this tree on the day this was
# written, so nothing here repeats it.
cat > "$TMP/idioms.txt" <<'IDIOM'
a far cry from
and nothing else
as the API spells it
across the board
at first blush
at the end of the day
back of the envelope
ballpark
bear in mind
bear out
bells and whistles
big picture
is what makes
bite the bullet
blow up
boil down to
boil the ocean
bread and butter
bring about
by and large
by hand
catch up
come across
come down to
come up with
corner cases
cut both ways
cut down
down the line
down the road
drill down
drop in the bucket
drop off
elephant in the room
end up
fall short
figure out
find out
foot the bill
from scratch
get around
get at
get by
get the ball rolling
give or take
go about
go hand in hand
grain of salt
hand in glove
hard and fast
heavy lifting
hit the ground running
in a nutshell
in the long run
in the same vein
in the weeds
iron out
keep an eye on
kick off
kick the can
knock out
last but not least
let down
line up
live with
load-bearing
look into
low-hanging fruit
make or break
miss out
move on
moving parts
moving target
nail down
needle in a haystack
off the shelf
on the fly
on the same page
on the table
out of the box
pan out
paper over
par for the course
pass over
pay off
pick up
pin down
play nice
play out
point out
pull in
pull the plug
push back
put it another way
put off
put up with
ramp up
read as
reads as
ring true
rip out
roll out
round out
rule of thumb
rule out
run into
run of the mill
run out of
scale back
second nature
settle on
shake out
shore up
sign off
silver bullet
slippery slope
sort out
spell out
square one
stack up
stem from
stick to
sticking point
take over
tease apart
the jury is out
throw away
tie in
tighten up
tip of the iceberg
to that end
touch base
turn out
under the hood
up in the air
up to speed
water under the bridge
wear many hats
weed out
with nothing of a
whole nine yards
which is why
wind up
work out
worth its salt
write off
zero in on
IDIOM
sed 's/.*/\\b&\\b/' "$TMP/idioms.txt" > "$TMP/idioms.re"

# The line above builds a pattern by transformation, and a lost backslash level
# turns every word boundary into the letter b. Such a file still greps, still
# prints, and matches nothing, so the run looks exactly like a clean one. Two
# reviewers read this check as dead after retyping that line into a shell that
# ate a backslash; the gate was correct and the copy was not. This is the
# question they should have asked the gate, and it is asked on every run: an
# idiom standing alone matches, and the same letters inside a word do not.
if ! printf 'this design is load-bearing\n' | grep -qiE -f "$TMP/idioms.re" ||
	printf 'the Xload-bearingY class\n' | grep -qiE -f "$TMP/idioms.re"; then
	report "check 12's idiom patterns are not word boundaries, so it can only report clean. First pattern:"
	head -1 "$TMP/idioms.re" | od -c | head -1 | sed 's/^/    /'
fi

# A blockquote is somebody else's sentence, so check 12 skips one for the
# reason check 7 skips a quoted span: quoting an idiom is not writing one.
cat > "$TMP/spans.awk" <<'AWK'
FNR == 1 { inf = 0; fm = ($0 == "---") }
fm && FNR > 1 && $0 == "---" { fm = 0; next }
fm && FNR > 40 { fm = 0 }
fm { next }
/^[ \t]*```/ { inf = !inf; next }
inf { next }
/^[ \t]*>/ { next }
{ line = $0; gsub(/`[^`]*`/, " ", line); print FILENAME ":" FNR ":" line }
AWK

count_cells() { awk -v LIMIT=120 -f "$TMP/cells.awk" "$@"; }
count_sentences() { awk -v LIMIT=30 -f "$TMP/sentences.awk" "$@"; }
count_idiom() { awk -f "$TMP/spans.awk" "$@" | grep -iE -f "$TMP/idioms.re" || true; }

# All three counters skip YAML frontmatter, and a skip has two ways to be
# wrong. It can read the block, which scored an epic worse the more decision
# ids the backlog's own rules put in it. It can swallow the document, which
# reports clean over a file nothing read. The probe below asks each counter
# both questions on every run, because neither failure looks like a failure.
cat > "$TMP/probe-frontmatter.md" <<'PROBE'
---
title: "a far cry from"
description: "a sentence in the frontmatter of more than thirty words, written here so a counter reading yaml as prose has something long to find, and finding it is what this probe refuses to let happen at all"
decisions: [M1, M6, D3]
---
| a | b |
|---|---|
| a cell cannot stand in frontmatter, so this one stands under it and runs past one hundred and twenty rendered characters to say the counter still reads the body |

A body sentence of more than thirty words, written under the closing marker so a skip that swallowed the whole document has something long to miss, and this probe fails when the counter reads none of it.

The body is a far cry from the frontmatter.
PROBE

probe_frontmatter() {
	[ "$2" = "$3" ] && return 0
	report "the frontmatter skip is wrong: $1 counted $3 in the probe and $2 was expected"
	sed -n '1,5p' "$TMP/probe-frontmatter.md" | sed 's/^/    /'
}

probe_frontmatter "check 13" 1 "$(count_sentences "$TMP/probe-frontmatter.md" | wc -l | tr -d ' ')"
probe_frontmatter "check 14" 1 "$(count_cells "$TMP/probe-frontmatter.md" | wc -l | tr -d ' ')"
probe_frontmatter "check 12" 1 "$(count_idiom "$TMP/probe-frontmatter.md" | wc -l | tr -d ' ')"

# An opener with no closing marker is the second failure, and the line cap is
# what bounds it. Forty-five keys put the sentence below past the cap, so a
# counter that reads it has released the block rather than run to the end of
# the file.
{
	echo '---'
	key=1
	while [ "$key" -le 45 ]; do
		echo "key$key: value"
		key=$((key + 1))
	done
	echo ''
	echo 'A sentence of more than thirty words placed past the cap, so an opener that never closes releases the counter here rather than swallowing every line under it, and this probe fails when it does not.'
} > "$TMP/probe-unclosed.md"

if [ "$(count_sentences "$TMP/probe-unclosed.md" | wc -l | tr -d ' ')" -eq 0 ]; then
	report "a frontmatter opener with no closing marker takes the whole file out of checks 12 to 14"
fi

# scoped <check-id> <counter> <message>. Holds every scoped file to its own
# count at the base, and a file the base does not carry to zero. Prints and
# refuses nothing when no base resolved.
scoped() {
	run_counter "$2" "$TMP/scoped" "$TMP/$1.raw"
	run_counter "$2" "$TMP/basefiles" "$TMP/$1.baseraw"
	tally "$TMP/$1.raw" "$TMP/$1.now"
	tally "$TMP/$1.baseraw" "$TMP/$1.was.tmp"
	# The base hits name a temp path. Translate it back before comparing.
	awk -F'\t' -v m="$TMP/map" '
		BEGIN { while ((getline l < m) > 0) { split(l, p, "\t"); name[p[1]] = p[2] } }
		{ print (($1 in name) ? name[$1] : $1) "\t" $2 }' "$TMP/$1.was.tmp" |
		sort > "$TMP/$1.was"
	awk -F'\t' -v was="$TMP/$1.was" '
		FILENAME == was { b[$1] = $2; next }
		{ if ($2 + 0 > b[$1] + 0) printf "%s (%s, %s at the base)\n", $1, $2, b[$1] + 0 }
	' "$TMP/$1.was" "$TMP/$1.now" > "$TMP/$1.delta"
	if [ -z "$STYLE_BASE" ]; then
		[ -s "$TMP/$1.now" ] || return 0
		echo "  $(wc -l < "$TMP/$1.now" | tr -d ' ') file(s), $(wc -l < "$TMP/$1.raw" | tr -d ' ') hit(s), counted and not refused"
		return 0
	fi
	[ -s "$TMP/$1.delta" ] || return 0
	report "$3"
	sed 's/^/    /' "$TMP/$1.delta"
	cut -f1 -d' ' "$TMP/$1.delta" | while read -r f; do
		grep -F "$f:" "$TMP/$1.raw" || true
	done | head -20 | sed 's/^/      /'
}

echo "12. no idiom and no phrasal verb..."
scoped idiom count_idiom "an idiom or a phrasal verb. A reader whose first language is not English cannot decode one from its parts. Write the plain verb."

echo "13. no prose sentence over 30 words..."
scoped sentence count_sentences "a prose sentence over 30 words. One idea per sentence, 25 words as the target."

echo "14. no table cell over 120 rendered characters..."
scoped cell count_cells "a table cell over 120 rendered characters. A cell holds a value. An argument goes under a heading, where a reader can link to it."

echo "15. no comment dated against the repository's own past..."
# ADR-20260826-zbh rules 1 to 5. Every form here can only mean a revision of this
# repository, so the reader has to know when the line was written to decode it.
#
# What is NOT here, and why, because each was counted before it was dropped
# (ADR-20260826-zbh rule 6 and its rejected table):
#   no longer   47 hits, and every one is a runtime state: an instance that no
#               longer exists, a token that no longer holds. Nothing to repair
#   today       19 hits, about half of them the domain (a certificate valid for
#               every printer today) and half a hedge about this build. A hedge
#               still reads correctly in five years
#   the old     13 hits, mostly the old machine or the old instance at runtime
#   currently   6 hits, all runtime state
#   earlier     18 hits, mostly an earlier build stage or an earlier activation
# A pattern cannot tell those from the defect, and a check that costs an author
# an argument is a check an author learns to route around.
#
# `used to` is refused in both its senses. One spelling carries two meanings,
# the past habitual and "employed to", and the reader has to guess which. The
# second sense rewrites to the plain verb, which ADR-20260826-shb rule 7 asks for
# anyway: "a Store used to assert the plumbing" becomes "a Store that asserts
# the plumbing".
# `before this` splits on the word after it. A noun after it names something the
# repository grew, so the sentence is a diff. A verb after it is control flow
# inside the running program, `before this returns`, and stays.
MOMENT='(this (change|commit|patch|pull request|diff|revision)|until now|up to now|until this|before this[ 	]*,|before this (nothing|the|a|an)|before this [a-z]+ (existed|exists|could|would|had|did|was|were|is|are)|previously|used to|the previous (version|implementation|behaviour|behavior|design|approach|shape|handler|scheme|rule)|(was|were) changed)'
MOMENT_CODE="(//|#)[^\"]*(^|[^A-Za-z])$MOMENT([^A-Za-z]|\$)"
MOMENT_MD="<!--[^>]*(^|[^A-Za-z])$MOMENT([^A-Za-z]|\$)"
{ code_grep -niE -- "$MOMENT_CODE" 2>/dev/null || true
	grep -iE -- "$MOMENT_MD" "$TMP/prose_all" 2>/dev/null || true; } | sort -u > "$TMP/m.raw"
per_file "$TMP/m.raw" "$TMP/m.res"
ratchet moment "$TMP/m.res" "a comment dated against the repository's own past. A comment describes the code, and a reader cannot date it. Put the change in the changelog, the decision in an ADR, the moment in the commit message, and cite history by commit hash, ADR number or issue number."
show "$TMP/m.raw"

echo "16. no emoji..."
# .agents/rules/writing-style.md: no emoji unless the user asks. Named by code
# point on check 1's model, never spelled, because this script has no fence.
#
# Two bands. \360\237 is the lead pair of every UTF-8 sequence in U+1F000 to
# U+1FFFF, which is where the pictograph blocks sit, so one two-byte needle
# covers all of them. The six after it are the BMP emoji this tree has carried,
# and U+FE0F is the variation selector that turns a text glyph into one.
#
# U+2713, U+2717 and U+26A0 are deliberately absent. Those are the status
# markers ADR-20260826-gtv rule 14 governs, they are ASCII-art rather than pictographs,
# and the records that state that rule have to quote them. A gate refusing them
# would refuse the documents that carry the rule.
#
# It ships with no baseline. Fifteen instances were repaired in the change that
# added it, and a zero-tolerance check is the shape check 1 already has.
EMOJI_PLANE=$(printf '\360\237')
EMOJI_TICK=$(printf '\342\234\205')
EMOJI_CROSS=$(printf '\342\235\214')
EMOJI_BALLOT=$(printf '\342\235\216')
EMOJI_STAR=$(printf '\342\255\220')
EMOJI_BANG=$(printf '\342\235\227')
EMOJI_SPARKLE=$(printf '\342\234\250')
EMOJI_SELECTOR=$(printf '\357\270\217')
HITS=$( { code_grep -nF -e "$EMOJI_PLANE" -e "$EMOJI_TICK" -e "$EMOJI_CROSS" \
		-e "$EMOJI_BALLOT" -e "$EMOJI_STAR" -e "$EMOJI_BANG" \
		-e "$EMOJI_SPARKLE" -e "$EMOJI_SELECTOR" 2>/dev/null || true
	grep -F -e "$EMOJI_PLANE" -e "$EMOJI_TICK" -e "$EMOJI_CROSS" \
		-e "$EMOJI_BALLOT" -e "$EMOJI_STAR" -e "$EMOJI_BANG" \
		-e "$EMOJI_SPARKLE" -e "$EMOJI_SELECTOR" "$TMP/prose_all" 2>/dev/null || true; } | head -20)
if [ -n "$HITS" ]; then
	report "an emoji. A screen reader speaks a table cell alone, so U+2705 is read as \"white heavy check mark\" and not as yes. Write the word."
	echo "$HITS" | sed 's/^/    /'
fi

echo "17. no rule attributed to a person or to a date..."
# .agents/rules/writing-style.md: a rule states the rule. Who decided it and
# when goes in the commit message.
#
# The list is the forms this tree carried. Each line is one pattern, read
# case-insensitively, over prose with code spans blanked and blockquotes
# skipped. A line opening with # is a comment and is dropped.
cat > "$TMP/attribution.txt" <<'ATTR'
# a rule handed to the owner by a verb: "the owner set the bar"
\bthe owner (said|says|ruled|rules|set|sets|asked|asks|chose|decided|wants|wanted|settled|approved|confirmed|rejected|took|answered|agreed|named|told|overruled|held|could not|has not accepted)\b
# the owner's ruling, the owner's words
\bowner'?s (ruling|call|words|decision|answer|instruction|directive|steer|standing)\b
# the pronoun standing for the owner: "his call", "only he can change it"
\bhis (ruling|call|words|decision|answer|instruction|directive|steer|authority|future)\b
\bhis to (reverse|change)\b
\b(is|are) his\b
\bhe (said|ruled|set|asked|chose|decided|wants|settled|approved|confirmed|rejected|took|answered|agreed|named|told|overruled|was asked|was given|could not)\b
\bonly he can\b
\bask him\b
# an owner decision cited by number or as a callout: "(owner decision 11)"
\bowner decision( [0-9]+|[.,:;]| rules?|$)
\bowner decisions this record\b
\baccepted by the owner\b
\bdecided by (the owner|him)\b
# a decision verb dated: "asked for this on 2026-08-27", "ruled 2026-09-10".
\b(asked|ruled|settled|set|took|decided|answered|confirmed|named|chose|overruled|accepted)( [a-z]+){0,3} (on )?20[0-9][0-9]-[0-9][0-9]-[0-9][0-9]\b
\b(ruling|decision|answer|entry|entries|directive) of 20[0-9][0-9]-[0-9][0-9]-[0-9][0-9]\b
# the writing moment as a date
\bat the time of writing\b
\bon the day (this|these) \b
\bon the day the [a-z ]+ (landed|was written|were written)\b
# a model named as the author: \"two Claude instances\". A quoted stranger
# naming one, and the file CLAUDE.md, are not caught
\b(claude|opus|fable|sonnet|haiku) (wrote|said|instances?|agents?|produced|drafted|rewrote|models?|sessions?|runs?)\b
\ban agent (wrote|said|decided|chose|ruled)\b
# a person by name
\b(ithan|slava)\b
\bmarc('s| owns| is taking| had| has not)\b
ATTR
grep -v '^#' "$TMP/attribution.txt" > "$TMP/attribution.re"

# The counter reads what spans.awk reads, with code spans blanked and fences,
# frontmatter and blockquotes dropped, and it drops the exemption by name
# so the pattern list above stays one list. A quoted line is somebody else's
# sentence, which is why a document may quote a stranger naming a model.
cat > "$TMP/attribution.awk" <<'AWK'
FNR == 1 { inf = 0; fm = ($0 == "---") }
fm && FNR > 1 && $0 == "---" { fm = 0; next }
fm && FNR > 40 { fm = 0 }
fm { next }
/^[ \t]*```/ { inf = !inf; next }
inf { next }
/^[ \t]*>/ { next }
{ line = $0; gsub(/`[^`]*`/, " ", line); print FILENAME ":" FNR ":" line }
AWK

count_attribution() { awk -f "$TMP/attribution.awk" "$@" | grep -iE -f "$TMP/attribution.re" || true; }

# The probe asks the counter both questions on every run: a sentence in prose
# is counted once, and the same sentence inside a fence or a blockquote is not.
mkdir -p "$TMP/docs/probe"
cat > "$TMP/docs/probe/probe-attribution.md" <<'PROBE'
# a probe

The owner set the bar on 2026-09-11 and only he can change it.

> The owner set the bar on 2026-09-11.

```
The owner set the bar on 2026-09-11.
```

## the next section

Nothing here names a person.
PROBE
probe_frontmatter "check 17" 1 "$(count_attribution "$TMP/docs/probe/probe-attribution.md" | wc -l | tr -d ' ')"

scoped attribution count_attribution "a rule attributed to a person or to a date. State the rule. Who decided it and when goes in the commit message."

if [ "$FAILED" -eq 1 ]; then
	echo ""
	echo "writing-style gates failed. See .agents/rules/writing-style.md,"
	echo "docs/decisions/20260826-gtb-writing-that-reads-as-written.md and"
	echo "docs/decisions/20260826-shb-a-document-is-read-at-speed.md"
	exit 1
fi

echo "✓ writing-style gates passed"

}
main "$@"
