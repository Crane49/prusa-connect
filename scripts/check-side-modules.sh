#!/bin/sh
# Builds and vets every Go module beside the core in its own folder, which is
# what a reader does after cloning one. `make side-modules-check` runs it.
#
# A folder with its own go.mod is not part of the root module, so
# `go build ./...` at the repository root skips it. A go.mod that asks for an
# older Go version than sdk/go.mod fails only when built from its own folder.
#
# The build runs with the folder as the working directory, and with a module
# cache of its own under the user cache directory. With the developer's cache,
# a dependency the folder cannot fetch would still be found. The cache is kept
# between runs. It is outside the repository, so the gates that walk the tree
# do not read it.
#
# The modules are found by a walk, so a new one is checked without an edit
# here. sdk/ is one of them.
#
# Needs go, and the network when the cache is cold.
#
# The whole run is one function, called on the last line, so the shell parses
# this file to its end before executing any of it. sh reads a script while it
# runs, and an edit during a run would otherwise change what runs.
main() {

ROOT=$(cd "$(dirname "$0")/.." && pwd)
cd "$ROOT" || exit 1

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT INT TERM

# GOMODCACHE must be an absolute path. Under Git Bash the path is converted
# with cygpath, because the Go toolchain on Windows cannot open a /c/... path.
CACHE=${XDG_CACHE_HOME:-$HOME/.cache}/pcl-side-modules
mkdir -p "$CACHE"

# `go build -o` discards the binaries only when it is given the exact name of
# the null device on that system, and on Windows that name is NUL.
DISCARD=/dev/null

if command -v cygpath > /dev/null 2>&1; then
	CACHE=$(cygpath -m "$CACHE")
	DISCARD=NUL
fi

echo "=== Every module beside the core ==="
echo "cache: $CACHE"

# Every go.mod except the one of the root module. The pruned folders have
# build output or installed dependencies and no source of this repository,
# except extensions/: each extension builds and vets in its own gate,
# `make -C extensions check`, which the extensions workflow runs.
find . \( -path ./.git -o -name node_modules -o -name vendor \
	-o -name dist -o -path ./bin -o -path ./extensions \) -prune -o -name go.mod -print |
	sed 's|^\./||; s|/\{0,1\}go\.mod$||' |
	grep -v '^$' | sort > "$TMP/modules"

COUNT=$(wc -l < "$TMP/modules" | tr -d ' ')

# A walk that finds fewer modules than FLOOR fails the run, because a loop over
# an empty list would pass. Raise FLOOR when modules are added, and keep it
# under the real count.
FLOOR=5

if [ "$COUNT" -lt "$FLOOR" ]; then
	echo "FAIL  the walk found $COUNT modules beside the core, under the floor of $FLOOR."
	echo "      Either a folder moved, or the walk stopped reaching it. A run that read"
	echo "      nothing proves nothing."
	exit 1
fi

echo "read: $COUNT modules"

FAILED=0

# `go build` and `go vet` run inside each folder. A folder that fails is named
# on its own line before its output.
while read -r module; do
	printf '  %s...' "$module"

	# In a folder with one main package, `go build ./...` writes the binary
	# into that folder, and examples/fractal-tree commits its binary. A
	# directory for -o is refused by a module with no main package, so the
	# binaries are discarded instead.
	if (cd "$module" && GOMODCACHE="$CACHE" go build -o "$DISCARD" ./... &&
		GOMODCACHE="$CACHE" go vet ./...) > "$TMP/out" 2>&1; then
		echo " ok"

		continue
	fi

	echo " FAIL"
	echo "FAIL  $module does not build and vet in its own folder."
	sed 's/^/      /' "$TMP/out"
	echo "      Run it as a reader does:  cd $module && go build ./... && go vet ./..."
	FAILED=1
done < "$TMP/modules"

if [ "$FAILED" -ne 0 ]; then
	echo
	echo "      A module here is what a community author clones and builds. The version"
	echo "      in its go directive has to be at least the one sdk/go.mod asks for, and"
	echo "      every requirement it names has to be in its go.sum."
	exit 1
fi

echo "OK    all $COUNT modules build and vet in their own folder"

}
main "$@"
