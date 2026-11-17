#!/bin/sh
# The lines a project outside this repository needs to install the two client
# packages and the standards package from the local npm registry.
#
#   scripts/registry-use.sh <registry url>          print them
#   scripts/registry-use.sh <registry url> <dir>    write them into <dir>
#
# With a directory it writes <dir>/.npmrc. An .npmrc already there keeps
# every line that is not one of these two, so a project with a registry of its
# own for another scope is not overwritten.
#
# pnpm 11 reads auth and registry settings from .npmrc and nothing else. The
# release-age policy lives in pnpm-workspace.yaml, and a version published
# seconds ago is younger than any window a project sets, so the second half of
# this script is about that file. It is never rewritten in place: a project's
# workspace file may carry a dozen settings this script has no business
# editing, so it is created when it is absent and reported on when it is not.
#
# The whole run is one function, called on the last line, so the shell parses
# this file to its end before executing any of it. sh reads a script while it
# runs, and an edit landing mid-run moves the read offset under the reader.
main() {

REGISTRY=$1
DIR=$2

if [ -z "$REGISTRY" ]; then
	echo "FAIL  this script takes the registry's URL and was given nothing."
	exit 1
fi

case $REGISTRY in
*/) ;;
*) REGISTRY="$REGISTRY/" ;;
esac

# npm's per-registry auth key is the URL with no scheme and no trailing slash.
AUTH_KEY=$(printf '%s' "$REGISTRY" | sed -e 's|^https\{0,1\}://||' -e 's|/$||')

NPMRC_SCOPE="@prusa:registry=${REGISTRY}"
NPMRC_AUTH="//${AUTH_KEY}/:_authToken=local"

print_npmrc() {
	echo "# The @prusa scope resolves from the local registry. Every other"
	echo "# package still comes from the public registry."
	echo "$NPMRC_SCOPE"
	echo "# Reading needs no credential. This line is here so the same project"
	echo "# can publish to the registry too, and the value is not a secret: the"
	echo "# registry accepts an unauthenticated publish of the @prusa scope and"
	echo "# listens on the loopback address alone."
	echo "$NPMRC_AUTH"
}

print_workspace() {
	echo "# The three packages are published locally seconds before they are"
	echo "# installed, so they are excluded from the release-age window."
	echo "minimumReleaseAgeExclude:"
	echo "  - '@prusa/connect'"
	echo "  - '@prusa/connect-ui'"
	echo "  - '@prusa/frontend-standards'"
}

if [ -z "$DIR" ]; then
	echo "Put these in .npmrc, beside the project's package.json:"
	echo ""
	print_npmrc | sed 's/^/  /'
	echo ""
	echo "And these in pnpm-workspace.yaml, if the project sets minimumReleaseAge:"
	echo ""
	print_workspace | sed 's/^/  /'
	echo ""
	echo "Then: pnpm add @prusa/connect@dev @prusa/connect-ui@dev @prusa/frontend-standards@dev"
	exit 0
fi

if [ ! -d "$DIR" ]; then
	echo "FAIL  $DIR is not a directory."
	exit 1
fi

NPMRC="$DIR/.npmrc"

# Every line of an existing .npmrc that is not one of the two this script owns
# is kept, so a second run is idempotent and a project's other settings
# survive. The comments this script writes are dropped along with the lines
# they explain, and written again below.
if [ -f "$NPMRC" ]; then
	KEPT=$(grep -v -e '^@prusa:registry=' -e '^//' -e '^# The @prusa scope resolves' \
		-e '^# package still comes from' -e '^# Reading needs no credential' \
		-e '^# can publish to the registry' -e '^# registry accepts an unauth' \
		-e '^# listens on the loopback' "$NPMRC" || true)
	printf '%s\n' "$KEPT" > "$NPMRC.next"
else
	: > "$NPMRC.next"
fi

print_npmrc >> "$NPMRC.next"
mv "$NPMRC.next" "$NPMRC"
echo "wrote $NPMRC"

WORKSPACE="$DIR/pnpm-workspace.yaml"

if [ ! -f "$WORKSPACE" ]; then
	print_workspace > "$WORKSPACE"
	echo "wrote $WORKSPACE"
elif grep -q 'minimumReleaseAgeExclude' "$WORKSPACE"; then
	echo "$WORKSPACE already excludes a package from the release-age window. Read it and add the three names if any is missing."
elif grep -q 'minimumReleaseAge' "$WORKSPACE"; then
	echo "$WORKSPACE sets minimumReleaseAge and would refuse a version published seconds ago. Add these lines to it:"
	print_workspace | sed 's/^/  /'
else
	echo "$WORKSPACE sets no release-age window, so nothing there blocks a version published seconds ago."
fi

echo ""
echo "OK    $DIR installs the @prusa scope from ${REGISTRY}"
echo "      pnpm add @prusa/connect@dev @prusa/connect-ui@dev @prusa/frontend-standards@dev"

}
main "$@"
