#!/bin/sh
# Writes a new migration pair, one file per engine, named by the date and time
# it is now in UTC (ADR-20261027-kqd). Two pull requests never pick one name by
# counting, and the order of two migrations is the order they were written in.
#
# Usage: new-migration.sh <package directory> <words>
#
# The directory is the package that owns the tables, such as
# internal/platform/devicestore. The words name the change in a few lowercase
# words, such as "device notes". The script prints the two paths it wrote.
set -e

case "$0" in
*/*) cd "${0%/*}/.." ;;
esac

DIR="${1:-}"
WORDS="${2:-}"

if [ -z "$DIR" ] || [ -z "$WORDS" ]; then
	echo "usage: $0 <package directory> <words>" >&2
	echo "  make migration-new DIR=internal/platform/devicestore NAME=\"device notes\"" >&2
	exit 2
fi

if [ ! -d "$DIR/migrations/sqlite" ] || [ ! -d "$DIR/migrations/postgres" ]; then
	echo "new-migration: $DIR holds no migrations/sqlite and migrations/postgres directories." >&2
	exit 2
fi

# The words as internal/arch/migration_names_test.go takes them: lowercase
# letters and digits, one underscore between each run.
SLUG=$(printf '%s' "$WORDS" | tr '[:upper:]' '[:lower:]' | tr -cs 'a-z0-9' '_' | sed 's/^_*//; s/_*$//')

if [ -z "$SLUG" ]; then
	echo "new-migration: \"$WORDS\" holds no letter or digit to name the file by." >&2
	exit 2
fi

NAME="$(date -u +%Y%m%dT%H%M%S)_$SLUG.sql"

for ENGINE in sqlite postgres; do
	if [ -e "$DIR/migrations/$ENGINE/$NAME" ]; then
		echo "new-migration: $DIR/migrations/$ENGINE/$NAME exists already. Wait a second, or use other words." >&2
		exit 1
	fi
done

for ENGINE in sqlite postgres; do
	FILE="$DIR/migrations/$ENGINE/$NAME"
	printf -- '-- %s\n--\n-- What this changes and why. It runs in the one transaction of the upgrade,\n-- so it holds no BEGIN, COMMIT or CONCURRENTLY.\n' "$WORDS" >"$FILE"
	echo "$FILE"
done
