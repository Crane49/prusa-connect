#!/bin/sh
# Writes a new decision record, with a fresh id and the front matter the
# gates read.
#
# An id is the date in UTC and three random characters, such as 20261025-k7q.
# Two pull requests never pick one id by counting, so neither has to rebase over
# the other's number. The characters are lowercase base32 without i, l, o and
# u, which read like 1, 1, 0 and v. internal/arch/record_ids_test.go refuses
# two files with one id, and a file with a sequential number.
#
# Usage: new-record.sh adr <title>
#
# The title is the record's short title in lowercase words. The file name is
# the id and the title as a slug. The script prints the path it wrote.
set -e

case "$0" in
*/*) cd "${0%/*}/.." ;;
esac

KIND="${1:-}"
TITLE="${2:-}"

usage() {
	echo "usage: $0 adr <title>" >&2
	echo "  make adr-new TITLE=\"a server answers to one id\"" >&2
	exit 2
}

case "$KIND" in
adr) DIR=docs/decisions ;;
*) usage ;;
esac

[ -n "$TITLE" ] || usage

case "$TITLE" in
*'"'*)
	echo "new-record: the title holds a double quote, which the front matter cannot carry as written." >&2
	exit 2
	;;
esac

# The title as a file name: lowercase letters and digits, one hyphen between
# each run, cut at a word to at most 60 characters.
SLUG=$(printf '%s' "$TITLE" | tr '[:upper:]' '[:lower:]' | tr -cs 'a-z0-9' '-' | sed 's/^-*//; s/-*$//' |
	awk '{ if (length($0) > 60) { s = substr($0, 1, 60); sub(/-[^-]*$/, "", s); print s } else print }')

if [ -z "$SLUG" ]; then
	echo "new-record: \"$TITLE\" holds no letter or digit to name the file by." >&2
	exit 2
fi

DAY=$(date -u +%Y%m%d)
DATE=$(date -u +%Y-%m-%d)
ALPHABET=abcdefghjkmnpqrstvwxyz234567

# Three random characters from the alphabet above. A byte modulo 28 favours
# the first four characters a little, which costs nothing here.
suffix() {
	od -An -N3 -tu1 /dev/urandom | awk -v a="$ALPHABET" 'NF >= 3 {
		s = ""
		for (i = 1; i <= 3; i++) s = s substr(a, ($i % 28) + 1, 1)
		print s
		exit
	}'
}

for _ in 1 2 3 4 5 6 7 8; do
	ID="$DAY-$(suffix)"
	if ! ls docs/decisions 2>/dev/null | grep -q "^$ID-"; then
		break
	fi
	ID=
done

if [ -z "$ID" ]; then
	echo "new-record: eight ids in a row were taken already, which should not happen. Run it again." >&2
	exit 1
fi

FILE="$DIR/$ID-$SLUG.md"
mkdir -p "$DIR"

case "$KIND" in
adr)
	cat >"$FILE" <<EOF
---
title: "ADR-$ID: $TITLE"
description: "One sentence saying what this record rules about."
date: $DATE
status: proposed
supersedes: []
amends: []
tags:
  - adr
---
# ADR-$ID: $TITLE

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md)

## rules

Numbered, imperative, one rule per line.

1. ...

## enforcement

The gate that makes a rule fail CI rather than fail review.

## rejected

| option | why not |
|---|---|
| ... | ... |
EOF
	;;
esac

echo "$FILE"
