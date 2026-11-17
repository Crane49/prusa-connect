#!/bin/sh
# The first-party extensions under extensions/, built and laid out as releases
# for `make demo`, so its extensions page offers them next to the examples.
#
# This is best effort, and it is never a gate. The extensions stay out of the
# core's CI (extensions/README.md), so an extension that does not build here
# prints one WARN line and is skipped. The demo still starts, and offers the
# others. No CI job runs this script.
#
# Every folder under extensions/ holding a module.yaml is an extension, found
# by a walk, so a new extension needs no edit here. Each one is built the way
# its own gate and the server image build it:
#
#   - the browser half: client/ first when there is one, then ui/, each with
#     `pnpm install --frozen-lockfile` and `pnpm run build`. ui/dist is then
#     the release layout, module.yaml at its root and the entry under ui/
#   - the Go half, when the extension has a go.mod: one binary for this
#     platform, laid beside the bundle by extensions/scripts/pack-release.sh,
#     the script the gates and the server image pack with
#   - a bundle alone gets its checksums.txt from
#     extensions/scripts/write-checksums.sh
#
# What it writes, under $PACK_ROOT/extensions:
#
#   parts/               the release directory the demo offers
#   fleet-stats/
#   buddy-debug-logs/
#
# The demo names each folder here in core.modules_local under the module path
# github.com/prusa3d/<folder>, the path the server image documents.
#
# Settings, all read from the environment:
#
#   PACK_ROOT   where the releases go, bin/demo-modules by default
#   PACK_FORCE  1 builds even when a release is newer than its sources
#
# The whole run is one function, called on the last line, so the shell parses
# this file to its end before executing any of it.
main() {

ROOT=$(cd "$(dirname "$0")/.." && pwd)
cd "$ROOT" || exit 1

PACK_ROOT=${PACK_ROOT:-bin/demo-modules}
PACK_FORCE=${PACK_FORCE:-0}
OUT=$PACK_ROOT/extensions

EXT_SCRIPTS=extensions/scripts
LIBRARY_DIR=packages/svelte
CLIENT_SDK_DIR=packages/typescript

# CI=true keeps pnpm from asking anything, and keeps its output readable.
CI=true
export CI

# Every extension has a browser half, so with no pnpm none of them can be
# built. The examples are skipped the same way.
for tool in go pnpm; do
	if ! command -v "$tool" > /dev/null 2>&1; then
		echo "SKIP  $tool is not on PATH, so the extensions were not packed for the demo."
		exit 0
	fi
done

WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT INT TERM

# stale answers whether a release has to be built again: it is not there, or a
# source file is newer than its checksums.txt. A built bundle and an installed
# node_modules are outputs, so neither counts.
stale() {
	stamp=$1/checksums.txt
	shift

	[ "$PACK_FORCE" = 1 ] && return 0
	[ -f "$stamp" ] || return 0

	# sh has no local variables, so this loop names its own: a `dir` here
	# would overwrite the one pack is building.
	for source in "$@"; do
		found=$(find "$source" -type f ! -path '*/node_modules/*' ! -path '*/dist/*' \
			-newer "$stamp" -print 2> /dev/null | head -1)
		[ -n "$found" ] && return 0
	done

	return 1
}

# run runs one build step with its output in a log. It answers false and keeps
# the log when the step fails, so the caller can skip the extension.
run() {
	"$@" > "$WORK/step.log" 2>&1
}

# warn says which step of which extension failed, with the end of its log.
warn() {
	echo "WARN  $1: $2 failed, so the demo does not offer it. The end of the log:"
	tail -n 20 "$WORK/step.log" | sed 's/^/      /'
}

# The two packages every bundle links to. A fresh clone has no dist under
# either, and no bundle builds without them.
if [ ! -f "$LIBRARY_DIR/dist/build/module.js" ]; then
	if ! run sh -c "cd '$LIBRARY_DIR' && pnpm install --frozen-lockfile && pnpm run build"; then
		warn "every extension" "the component library build"
		exit 0
	fi
fi

if [ ! -f "$CLIENT_SDK_DIR/dist/index.js" ]; then
	if ! run sh -c "cd '$CLIENT_SDK_DIR' && pnpm install --frozen-lockfile && pnpm run build"; then
		warn "every extension" "the client SDK build"
		exit 0
	fi
fi

# pack builds one extension into $OUT/<folder>. It answers false after a
# warning, and the caller moves on to the next one.
pack() {
	ext=$1
	name=$(basename "$ext")
	release=$OUT/$name

	if ! stale "$release" "$ext" "$LIBRARY_DIR/src" "$CLIENT_SDK_DIR/src"; then
		echo "OK    $name is newer than its sources, so it was not rebuilt."
		return 0
	fi

	echo "      $name builds..."

	if [ -f "$ext/client/package.json" ]; then
		run sh -c "cd '$ext/client' && pnpm install --frozen-lockfile && pnpm run build" ||
			{ warn "$name" "the typed client build"; return 1; }
	fi

	run sh -c "cd '$ext/ui' && pnpm install --frozen-lockfile && pnpm run build" ||
		{ warn "$name" "the browser bundle build"; return 1; }

	if [ ! -f "$ext/ui/dist/module.yaml" ] || [ ! -f "$ext/ui/dist/ui/index.js" ]; then
		echo "WARN  $name: ui/dist has no module.yaml or ui/index.js, so the demo does not offer it."
		return 1
	fi

	mkdir -p "$OUT"

	if [ -f "$ext/go.mod" ]; then
		# The file name is <name>_<GOOS>_<GOARCH>, which is what the host
		# looks for beside the manifest.
		binary="$WORK/${name}_$(go env GOOS)_$(go env GOARCH)$(go env GOEXE)"

		run sh -c "CGO_ENABLED=0 go -C '$ext' build -o '$binary' ." ||
			{ warn "$name" "the Go build"; return 1; }
		run sh "$EXT_SCRIPTS/pack-release.sh" "$ext/ui/dist" "$binary" "$release" ||
			{ warn "$name" "packing the release"; return 1; }
	else
		rm -rf "$release"
		mkdir -p "$release"
		cp -R "$ext/ui/dist"/. "$release"/
		run sh "$EXT_SCRIPTS/write-checksums.sh" "$release" ||
			{ warn "$name" "writing the checksums"; return 1; }
	fi

	echo "OK    $name is packed in $release"
}

for manifest in extensions/*/module.yaml; do
	[ -f "$manifest" ] || continue
	extension=$(dirname "$manifest")

	# A release left from an earlier run would be offered as if it had built
	# now, so a failed build removes it.
	pack "$extension" || rm -rf "${OUT:?}/$(basename "$extension")"
done

}
main "$@"
