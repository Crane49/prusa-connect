#!/usr/bin/env bash
# Regenerate every print file of the corpus with PrusaSlicer 2.9.6.
#
# Usage: make.sh <path to the prusa-slicer binary> [<path to PrusaResearch.ini>]
#
# The binary must be PrusaSlicer 2.9.6. The vendor bundle defaults to the
# PrusaResearch.ini that binary ships: Contents/Resources/profiles on macOS,
# resources/profiles beside the binary on Linux and Windows.
#
# corpus.tsv names one file per row: the printer, print and filament profiles,
# the overlay from ini/ and whether a text G-code is written next to the
# bgcode. The script writes <file>.bgcode for every row, and <file>.gcode for a
# row whose text column is 1. Rewrite the golden files afterwards, and read
# the diff:
#
#   go test ./internal/printfile/ -run TestCorpusGolden -update
#   (cd sdk && go test ./printcompat/ -run TestCorpusMatrix -update)
set -euo pipefail

if [ $# -lt 1 ] || [ $# -gt 2 ]; then
	echo "usage: $0 <prusa-slicer binary> [<PrusaResearch.ini>]" >&2
	exit 2
fi

slicer=$1
here=$(cd "$(dirname "$0")" && pwd)
version=$("$slicer" --help 2>/dev/null | head -n 1 || true)

case $version in
*PrusaSlicer-2.9.6\ *) ;;
*)
	echo "$slicer is not PrusaSlicer 2.9.6: it says '$version'" >&2
	exit 1
	;;
esac

bundle=${2:-}
if [ -z "$bundle" ]; then
	bin_dir=$(cd "$(dirname "$slicer")" && pwd)
	for candidate in "$bin_dir/../Resources/profiles/PrusaResearch.ini" \
		"$bin_dir/resources/profiles/PrusaResearch.ini" \
		"$bin_dir/../resources/profiles/PrusaResearch.ini"; do
		if [ -f "$candidate" ]; then
			bundle=$candidate
			break
		fi
	done
fi

if [ ! -f "$bundle" ]; then
	echo "no PrusaResearch.ini found; pass its path as the second argument" >&2
	exit 1
fi

# The data directory holds the shipped vendor bundle and a PrusaSlicer.ini
# that installs every printer model of it with every nozzle variant, so the
# command line may name any of its printer profiles.
datadir=$(mktemp -d)
trap 'rm -rf "$datadir"' EXIT
mkdir -p "$datadir/vendor"
cp "$bundle" "$datadir/vendor/PrusaResearch.ini"
{
	echo "[vendor:PrusaResearch]"
	awk '/^\[printer_model:/ { model = substr($0, 16, length($0) - 16) }
		model != "" && /^variants = / { sub(/^variants = /, ""); print "model:" model " = " $0; model = "" }' \
		"$bundle"
} >"$datadir/PrusaSlicer.ini"

stl="$here/../cube10.stl"
failed=0

slice() {
	local output=$1
	shift
	if ! "$slicer" --datadir "$datadir" "$@" --export-gcode --output "$output" "$stl" >"$datadir/log" 2>&1; then
		echo "FAILED $(basename "$output")" >&2
		tail -n 5 "$datadir/log" >&2
		failed=1
		return
	fi
	echo "wrote  $(basename "$output")"
}

tail -n +2 "$here/corpus.tsv" | while IFS=$'\t' read -r file _family _model _variant nozzle _hf _materials _abrasive _tools _used _checks \
	printer print filaments overlay text _id; do
	layer=layer-thick.ini
	case $nozzle in
	0.25 | 0.3) layer=layer-thin.ini ;;
	esac

	args=(--printer-profile "$printer" --print-profile "$print" --material-profile "$filaments"
		--load "$here/ini/small.ini" --load "$here/ini/$layer")
	if [ "$overlay" != - ]; then
		args+=(--load "$here/ini/$overlay.ini")
	fi

	slice "$here/$file.bgcode" "${args[@]}" --load "$here/ini/binary.ini"
	if [ "$text" = 1 ]; then
		slice "$here/$file.gcode" "${args[@]}" --load "$here/ini/text.ini"
	fi

	if [ "$failed" = 1 ]; then
		exit 1
	fi
done
