#!/bin/sh
# The race image's entrypoint: go test under the race detector, on Linux, for
# the packages and tags the caller names.
#
#   PACKAGES  what go test is given. ./... is the tree. A path under ./sdk/ or
#             under ./packages/<name>/ is run inside that module, which is its
#             own module and is not reached by a pattern from the root.
#   TAGS      the build tags. Empty runs untagged. The default is the set the
#             CI unit job runs.
#
# The Go that runs is the one go.mod pins, read here on every run so a bump
# in go.mod is honoured without a rebuild. A download lands in the module
# cache volume, so it happens once.
set -eu

cd /src
GOTOOLCHAIN="go$(sed -n 's/^go //p' go.mod)"
export GOTOOLCHAIN

PACKAGES="${PACKAGES:-./...}"
TAGS="${TAGS-full diagnostics observability postgres}"

echo "Race: $(go version)"
echo "Race: GOMAXPROCS=$GOMAXPROCS, packages $PACKAGES, tags \"$TAGS\""

case "$PACKAGES" in
./sdk | ./sdk/*)
	# The sdk module. The pattern is re-rooted at the module, so ./sdk/...
	# becomes ./... inside it.
	SDK_PACKAGES="./${PACKAGES#./sdk}"
	SDK_PACKAGES="$(printf '%s' "$SDK_PACKAGES" | sed 's|^\.//|./|')"
	[ "$SDK_PACKAGES" = "./" ] && SDK_PACKAGES="./..."
	echo "Race: sdk module, packages $SDK_PACKAGES"
	cd sdk
	exec go test -short -race -count=1 -timeout 30m "$SDK_PACKAGES"
	;;
./packages/*)
	# A module under packages/. The first path element after packages/ names
	# the folder that carries the go.mod, and the rest of the pattern is
	# re-rooted at it the way the sdk branch above re-roots its own.
	MODULE_REST="${PACKAGES#./packages/}"
	MODULE_NAME="${MODULE_REST%%/*}"
	MODULE_PACKAGES="./${MODULE_REST#"$MODULE_NAME"}"
	MODULE_PACKAGES="$(printf '%s' "$MODULE_PACKAGES" | sed 's|^\.//|./|')"
	[ "$MODULE_PACKAGES" = "./" ] && MODULE_PACKAGES="./..."
	echo "Race: packages/$MODULE_NAME module, packages $MODULE_PACKAGES"
	cd "packages/$MODULE_NAME"
	exec go test -short -race -count=1 -timeout 30m "$MODULE_PACKAGES"
	;;
esac

# shellcheck disable=SC2086
go test -short -race -count=1 -timeout 30m -tags "$TAGS" $PACKAGES

# The tree pattern covers every module, the way make test-race does. Each one
# beside the root has its own go.mod, so the root pattern never reaches it.
if [ "$PACKAGES" = "./..." ]; then
	echo "Race: sdk module..."
	(cd sdk && go test -short -race -count=1 -timeout 30m ./...)

	for mod in packages/*/go.mod; do
		[ -f "$mod" ] || continue
		dir="${mod%/go.mod}"
		echo "Race: $dir module..."
		(cd "$dir" && go test -short -race -count=1 -timeout 30m ./...)
	done
fi
