#!/bin/sh
# Checks the server binary for every shipping target.
#
#   1. each target cross-compiles with CGO_ENABLED=0
#   2. the binary is static: its recorded build settings say CGO_ENABLED=0, and
#      its dependency graph does not contain runtime/cgo
#   3. the binary is no larger than the budget of its row in BUDGETS
#
# Needs go and git. Run it with `make binary-check`.
#
# When a row is over budget, find what grew. If the growth is wanted, raise the
# row and give the reason in the commit message.
set -e

# Every path below is relative to the repository root.
ROOT=$(git rev-parse --show-toplevel)
cd "$ROOT"

PKG=./cmd/prusa-connect
FULL="full diagnostics observability"
LDFLAGS="-s -w"

# The printer build takes its tags from the Makefile, so this gate builds what
# `make build-printer` builds. makefile_value prints the words a variable is
# given, with the comment at the end of each line removed.
makefile_value() {
	awk -v name="$1" '$1 == name && ($2 == ":=" || $2 == "+=" || $2 == "?=") {
		sub(/#.*/, ""); for (i = 3; i <= NF; i++) printf "%s ", $i
	}' Makefile
}
PRINTER_TAGS=$(makefile_value PRINTER_TAGS)
PRINTER_CHECK_MODEL=$(makefile_value PRINTER_CHECK_MODEL)
PRINTER_CONFIG=$(makefile_value PRINTER_CONFIG)
PRINTER_CONFIG_TAGS=$(go run ./cmd/detect-build-tags --config $PRINTER_CONFIG --variant full)
PRINTER="full printer $PRINTER_TAGS $PRINTER_CONFIG_TAGS"

# armed fails the run when a scan read fewer items than its floor. A scan that
# read nothing would otherwise pass.
# $1 names the input set, $2 is the count read, $3 is the floor.
armed() {
	if [ "$2" -lt "$3" ]; then
		echo "✗ the gate read $2 $1, under its floor of $3, so this run proved nothing."
		echo "  Either the input moved, or the scan stopped reaching it."
		exit 1
	fi
}

# One row per shipping target: GOARCH, variant, budget in bytes. The target OS
# is linux. The minimal variant builds with the `minimal` tag, the full
# variant builds with the tags in FULL, and the printer variant builds with
# the tags in PRINTER.
#
#   amd64:minimal  a 64-bit x86 server, without the optional features
#   amd64:full     a 64-bit x86 server, with every optional feature
#   arm64:minimal  a 64-bit Raspberry Pi, without the optional features
#   arm64:full     a 64-bit Raspberry Pi, with every optional feature
#   arm:minimal    a 32-bit Raspberry Pi, without the optional features
#   arm:full       a 32-bit Raspberry Pi, with every optional feature
#   arm64:printer  the printer's own board, the binary `make build-printer` writes
BUDGETS="
amd64:minimal:42991616
amd64:full:47185920
arm64:minimal:38797312
arm64:full:42991616
arm:minimal:38797312
arm:full:42991616
arm64:printer:38797312
"

# ldflags_for prints the linker flags of a variant. The printer variant also
# names the model its profile was written for, as `make build-printer` does.
ldflags_for() {
	case "$1" in
	printer) echo "$LDFLAGS -X github.com/prusa3d/prusa-connect/internal/printer/profile.BuiltFor=$(echo $PRINTER_CHECK_MODEL)" ;;
	*) echo "$LDFLAGS" ;;
	esac
}

# tags_for prints the build tags of a variant.
tags_for() {
	case "$1" in
	full) echo "$FULL" ;;
	printer) echo "$PRINTER" ;;
	*) echo minimal ;;
	esac
}

# The directory `//go:embed all:appdist` reads behind the full tag. The full
# rows are larger when it holds a built web application, so the run prints how
# much it found there.
EMBEDS="internal/transport/http/httpapi/appdist"

FAILED=0
BUILT=0
OUT=$(mktemp -d)
trap 'rm -rf "$OUT"' EXIT

EMBED_DIRS=0
EMBED_FILES=0
EMBED_BYTES=0
for dir in $EMBEDS; do
	[ -d "$dir" ] || continue
	EMBED_DIRS=$((EMBED_DIRS + 1))
	found=$(find "$dir" -type f ! -name .gitkeep | wc -l | tr -d ' ')
	EMBED_FILES=$((EMBED_FILES + found))
	if [ "$found" -gt 0 ]; then
		bytes=$(find "$dir" -type f ! -name .gitkeep -exec wc -c {} + |
			awk '$2 != "total" { total += $1 } END { print total + 0 }')
		EMBED_BYTES=$((EMBED_BYTES + bytes))
	fi
done
armed "app embed directories" "$EMBED_DIRS" 1
echo "app embed: $EMBED_FILES files, $EMBED_BYTES bytes under $EMBEDS"

# The printer variant embeds the profile of one printer model. This gate is
# not given the printer's model, so it writes the profile of the released
# model PRINTER_CHECK_MODEL names, the way `make build-printer` writes it.
armed "printer tags" "$(echo $PRINTER_TAGS | wc -w | tr -d ' ')" 1
go run ./cmd/extract-printer-profile -model $PRINTER_CHECK_MODEL
echo "printer tags: $PRINTER"

printf '%-8s %-9s %12s %12s %8s\n' target variant size budget margin

for row in $BUDGETS; do
	[ -n "$row" ] || continue
	BUILT=$((BUILT + 1))

	arch=$(echo "$row" | cut -d: -f1)
	variant=$(echo "$row" | cut -d: -f2)
	budget=$(echo "$row" | cut -d: -f3)

	tags=$(tags_for "$variant")

	if ! CGO_ENABLED=0 GOOS=linux GOARCH="$arch" \
		go build -tags "$tags" -ldflags="$(ldflags_for "$variant")" -o "$OUT/b" "$PKG" 2>"$OUT/err"; then
		printf '%-8s %-9s %12s\n' "$arch" "$variant" "BUILD FAILED"
		sed 's/^/    /' "$OUT/err"
		FAILED=1
		continue
	fi

	size=$(wc -c <"$OUT/b" | tr -d ' ')
	margin=$((budget - size))

	printf '%-8s %-9s %12s %12s %8s\n' "$arch" "$variant" "$size" "$budget" "$margin"

	if [ "$size" -gt "$budget" ]; then
		echo "    over budget by $((size - budget)) bytes."
		echo "    Either find what grew, or raise the budget here and say why in the commit."
		FAILED=1
	fi

	# Check 2, first reading: `go version -m` prints the build settings the
	# linker recorded in the binary. A read that fails also fails the row.
	if ! go version -m "$OUT/b" >"$OUT/buildinfo" 2>"$OUT/err"; then
		echo "    the build settings could not be read out of the binary:"
		sed 's/^/        /' "$OUT/err"
		FAILED=1
	elif ! tr -d '\r' <"$OUT/buildinfo" |
		grep -qE '(^|[[:space:]])CGO_ENABLED=0([[:space:]]|$)'; then
		echo "    the binary does not record CGO_ENABLED=0, so it is not the static one."
		FAILED=1
	fi
done

# BUDGETS has seven rows. Fewer means an edit removed one.
armed "budget rows" "$BUILT" 7

# Check 2, second reading: the dependency graph of each row, which names the
# package that links cgo. Each row has its own graph, because build
# constraints and tags change which packages are in it. A `go list` that fails
# also fails the row, because its empty output would look like a graph without
# cgo.
echo ""
echo "checking the binary is static..."
GRAPHS=0
for row in $BUDGETS; do
	[ -n "$row" ] || continue
	GRAPHS=$((GRAPHS + 1))

	arch=$(echo "$row" | cut -d: -f1)
	variant=$(echo "$row" | cut -d: -f2)

	tags=$(tags_for "$variant")

	if ! CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go list -deps -tags "$tags" "$PKG" \
		>"$OUT/deps" 2>"$OUT/err"; then
		echo "✗ go list could not resolve the $arch/$variant graph, so its static claim went unchecked:"
		sed 's/^/    /' "$OUT/err"
		FAILED=1
		continue
	fi

	# The real graph has several hundred packages. Under 100 means `go list`
	# stopped early.
	armed "packages in the $arch/$variant dependency graph" "$(grep -c . "$OUT/deps")" 100
	if grep -q '^runtime/cgo$' "$OUT/deps"; then
		echo "✗ $arch/$variant links runtime/cgo under CGO_ENABLED=0, so the binary is not static"
		FAILED=1
	fi
done

armed "dependency graphs" "$GRAPHS" 7

if [ "$FAILED" -eq 1 ]; then
	echo ""
	echo "binary budget gates failed. The server ships as one static binary, from a Raspberry Pi to a server."
	exit 1
fi

echo "✓ binary budget gates passed"
