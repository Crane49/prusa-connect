#!/bin/sh
# Holds every extension to the same layout, so a new one is shaped like the
# ones before it and extensions/Makefile can run it without knowing its name.
#
# An extension is a folder of extensions/ holding a module.yaml. Each one has:
#
#   module.yaml   the manifest, with a version
#   CHANGELOG.md  whose newest heading is `## <version> - YYYY-MM-DD`
#   README.md     what it is and how to run its gate
#   Makefile      including ../extension.mk and defining `check`
#
# A folder of extensions/ that is neither an extension nor scripts/ is refused
# too, because the walk in extensions/Makefile would never find it.
#
# Usage: scripts/check-shape.sh [extension ...]. With no names it checks every
# extension found.
set -e

cd "$(dirname "$0")/.."

FAILED=0
fail() {
	echo "✗ $1"
	FAILED=1
}

for dir in */; do
	dir=${dir%/}
	case "$dir" in scripts) continue ;; esac
	[ -f "$dir/module.yaml" ] ||
		fail "extensions/$dir holds no module.yaml, so it is not an extension and nothing runs its gates"
done

if [ "$#" -eq 0 ]; then
	set -- $(for m in */module.yaml; do [ -f "$m" ] && dirname "$m"; done)
fi

if [ "$#" -eq 0 ]; then
	echo "✗ no extension found under extensions/"
	exit 1
fi

for ext in "$@"; do
	[ -d "$ext" ] || { fail "extensions/$ext does not exist"; continue; }

	for file in module.yaml CHANGELOG.md README.md Makefile; do
		[ -f "$ext/$file" ] || fail "extensions/$ext has no $file"
	done

	if [ -f "$ext/Makefile" ]; then
		grep -q '^include \.\./extension\.mk$' "$ext/Makefile" ||
			fail "extensions/$ext/Makefile does not include ../extension.mk"
		grep -qE '^check:' "$ext/Makefile" ||
			fail "extensions/$ext/Makefile defines no check target"
	fi

	if [ -f "$ext/module.yaml" ] && [ -f "$ext/CHANGELOG.md" ]; then
		version=$(sed -n 's/^version:[ 	]*//p' "$ext/module.yaml" | head -1)
		# The newest `## ` heading of any kind, as written. It has to be a
		# release heading of the version module.yaml declares, the same form
		# the extension's own changelog test reads.
		newest=$(grep -m 1 '^## ' "$ext/CHANGELOG.md" | tr -d '\r')
		if [ -z "$version" ]; then
			fail "extensions/$ext/module.yaml declares no version"
		elif ! printf '%s\n' "$newest" | grep -qE '^## [0-9]+\.[0-9]+\.[0-9]+ - [0-9]{4}-[0-9]{2}-[0-9]{2}$'; then
			fail "extensions/$ext/CHANGELOG.md starts with '${newest:-no heading}', and the newest heading must read '## $version - YYYY-MM-DD'"
		elif [ "$newest" = "${newest#"## $version "}" ]; then
			fail "extensions/$ext is version $version, and the newest CHANGELOG.md heading is '$newest'"
		fi
	fi
done

if [ "$FAILED" -ne 0 ]; then
	echo "extension shape gates failed. See extensions/README.md."
	exit 1
fi

echo "OK    $# extension(s) have the shape extensions/README.md describes"
