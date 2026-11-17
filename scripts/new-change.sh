#!/bin/sh
# Writes a fragment of pending news in docs/changes/ (ADR-20261021-arm). The name is the
# date and time it is now in UTC, then the words given, so two pull requests
# never pick one name and a release files them in the order they were written.
#
# Usage: new-change.sh <words> [category]
#
# The words name the change in a few lowercase words, such as nearby-printers.
# The category is one of the six in changelog-discipline.md and is Fixed when
# none is given. The fragment holds one empty bullet to replace with the entry.
set -e

case "$0" in
*/*) cd "${0%/*}/.." ;;
esac

WORDS="${1:-}"
CATEGORY="${2:-Fixed}"
CHANGES="${CHANGES:-docs/changes}"

if [ -z "$WORDS" ]; then
	echo "usage: $0 <words> [category]" >&2
	echo "  make changelog-new NAME=nearby-printers CATEGORY=Changed" >&2
	exit 2
fi

case " Security Removed Changed Deprecated Added Fixed " in
*" $CATEGORY "*) ;;
*)
	echo "new-change: \"$CATEGORY\" is not a category. Use one of: Security Removed Changed Deprecated Added Fixed" >&2
	exit 2
	;;
esac

# The words as the gate takes them: lowercase letters and digits, one hyphen
# between each run.
SLUG=$(printf '%s' "$WORDS" | tr '[:upper:]' '[:lower:]' | tr -cs 'a-z0-9' '-' | sed 's/^-*//; s/-*$//')

if [ -z "$SLUG" ]; then
	echo "new-change: \"$WORDS\" holds no letter or digit to name the fragment by." >&2
	exit 2
fi

FILE="$CHANGES/$(date -u +%Y%m%d-%H%M%S)-$SLUG.md"

if [ -e "$FILE" ]; then
	echo "new-change: $FILE exists already. Wait a second, or use other words." >&2
	exit 1
fi

mkdir -p "$CHANGES"
printf '### %s\n- \n' "$CATEGORY" >"$FILE"

echo "$FILE"
