#!/bin/sh
# Build, stamp, pack and publish the two client packages and the standards
# package into an npm registry.
#
# Two callers, one implementation. `scripts/check-client-sdk-install.sh` calls
# it against the throwaway fixture at the tree's own VERSION, and `make
# registry-publish` calls it against the developer's registry at a dev version.
# Before this script the two would have been two copies of the same eleven
# steps, and the copy the lane does not run is the one that rots.
#
#   scripts/registry-publish.sh --registry URL --version V --pack-dir DIR \
#                               [--dist-tag TAG] [--token T]
#
# --registry   where to publish. A trailing slash is added when it is missing,
#              because npm's per-registry auth key is written from the URL and
#              the two forms are different keys
# --version    the version all three packages go out as. Empty means the
#              tree's VERSION with `-dev.<short sha>` after it
# --pack-dir   where the three tarballs are written. The caller reads them
#              back: the lane compares their integrity with what the
#              consumer's lockfile recorded
# --dist-tag   the dist-tag the publish writes. `latest` by default; `make
#              registry-publish` passes `dev`, so `pnpm add @prusa/...@dev`
#              resolves to the last local publish
# --token      the auth token. Any value satisfies npm, which refuses to
#              publish before it sends anything when no token is configured,
#              and the registry accepts an unauthenticated publish of the
#              @prusa scope. Nothing here ever asks for a password
#
# It prints the three names and versions it published on the last line.
#
# The manifests are restored before this script returns, whatever it returns.
# Stamping a dev version writes packages/typescript/package.json,
# packages/svelte/package.json and packages/standards/package.json, and a
# tree left holding `0.1.0-dev.abc1234` would fail `make client-sdk-check`
# and would be committed by the next person who staged everything. The
# originals are copied aside first and copied back from a trap.
#
# The whole run is one function, called on the last line, so the shell parses
# this file to its end before executing any of it. sh reads a script while it
# runs, and an edit landing mid-run moves the read offset under the reader.
main() {

ROOT=$(cd "$(dirname "$0")/.." && pwd)
cd "$ROOT" || exit 1

SDK_DIR=packages/typescript
UI_DIR=packages/svelte
STANDARDS_DIR=packages/standards

REGISTRY=""
VERSION=""
PACK_DIR=""
DIST_TAG=latest
TOKEN=fixture

while [ $# -gt 0 ]; do
	case $1 in
	--registry)
		REGISTRY=$2
		shift 2
		;;
	--version)
		VERSION=$2
		shift 2
		;;
	--pack-dir)
		PACK_DIR=$2
		shift 2
		;;
	--dist-tag)
		DIST_TAG=$2
		shift 2
		;;
	--token)
		TOKEN=$2
		shift 2
		;;
	*)
		echo "FAIL  $1 is not an argument this script takes."
		exit 1
		;;
	esac
done

if [ -z "$REGISTRY" ]; then
	echo "FAIL  --registry names no registry."
	exit 1
fi

case $REGISTRY in
*/) ;;
*) REGISTRY="$REGISTRY/" ;;
esac

# The per-registry auth key npm reads is the URL with no scheme and no
# trailing slash, so it is derived here rather than written out by a caller
# that would have to keep the two in step.
AUTH_KEY=$(printf '%s' "$REGISTRY" | sed -e 's|^https\{0,1\}://||' -e 's|/$||')
AUTH_ARG="--//${AUTH_KEY}/:_authToken=${TOKEN}"

CORE_VERSION=$(tr -d '[:space:]' < VERSION)

if [ -z "$VERSION" ]; then
	SHORT_SHA=$(git rev-parse --short HEAD 2> /dev/null) || SHORT_SHA=""
	if [ -z "$SHORT_SHA" ]; then
		echo "FAIL  no --version was given and git could not name the commit this tree is on."
		exit 1
	fi
	VERSION="${CORE_VERSION}-dev.${SHORT_SHA}"
fi

TARBALL_NAME="prusa-connect-${VERSION}.tgz"
UI_TARBALL_NAME="prusa-connect-ui-${VERSION}.tgz"
STANDARDS_TARBALL_NAME="prusa-frontend-standards-${VERSION}.tgz"

for tool in pnpm node npm; do
	if ! command -v "$tool" > /dev/null 2>&1; then
		echo "FAIL  $tool is not on PATH, and publishing needs it."
		exit 1
	fi
done

WORK=$(mktemp -d)
STAMPED=""

# The manifests come back whatever happens, including a Ctrl+C halfway through
# a publish. `cp -p` brings back the modification time as well as the bytes:
# pnpm's `verifyDepsBeforeRun: error` refuses every later `pnpm run` and
# `pnpm exec` in a package whose package.json is newer than its last install,
# even when the content is the same.
restore_manifests() {
	if [ -n "$STAMPED" ]; then
		cp -p "$WORK/sdk-package.json" "$SDK_DIR/package.json"
		cp -p "$WORK/ui-package.json" "$UI_DIR/package.json"
		cp -p "$WORK/standards-package.json" "$STANDARDS_DIR/package.json"
		STAMPED=""
	fi
}

cleanup() {
	restore_manifests
	rm -rf "$WORK"
}
trap cleanup EXIT INT TERM

if [ -z "$PACK_DIR" ]; then
	PACK_DIR="$WORK/tarballs"
fi
mkdir -p "$PACK_DIR"

STEPS=0
step() {
	WHAT=$1
	shift
	STEPS=$((STEPS + 1))

	printf '%2d. %s...\n' "$STEPS" "$WHAT"

	if "$@" > "$WORK/step.log" 2>&1; then
		return 0
	else
		RC=$?
	fi

	echo ""
	echo "FAIL  $WHAT (exit $RC)"
	sed 's/^/      /' "$WORK/step.log"
	exit 1
}

build_sdk() {
	(cd "$SDK_DIR" && pnpm run build)
}

build_ui() {
	(cd "$UI_DIR" && pnpm run build)
}

# The library's peer on the SDK is an exact version, so a consumer installing
# both gets one copy of the client and the library draws against the same one.
# A peer left at another version would resolve from somewhere that is not this
# publish.
stamp() {
	cp -p "$SDK_DIR/package.json" "$WORK/sdk-package.json" || return 1
	cp -p "$UI_DIR/package.json" "$WORK/ui-package.json" || return 1
	cp -p "$STANDARDS_DIR/package.json" "$WORK/standards-package.json" || return 1
	STAMPED=yes

	(cd "$SDK_DIR" && npm pkg set version="$VERSION") || return 1
	(cd "$UI_DIR" && npm pkg set version="$VERSION") || return 1
	(cd "$UI_DIR" && npm pkg set "peerDependencies.@prusa/connect=$VERSION") || return 1
	(cd "$STANDARDS_DIR" && npm pkg set version="$VERSION")
}

# The manifests go back as soon as the tarballs are written, and not at the
# end. Every step after this one reads a tarball rather than the tree, and
# pnpm's `verifyDepsBeforeRun: error` refuses to run publint against a
# package.json that has moved since the last install: with the stamp still in
# place, the two lint steps below fail on the version this script wrote
# itself.
unstamp() {
	restore_manifests
}

pack_sdk() {
	(cd "$SDK_DIR" && npm pack --pack-destination "$PACK_DIR") && test -f "$PACK_DIR/$TARBALL_NAME"
}

# pnpm rather than npm here: the library's build writes declarations through a
# script of its own, and the project is a pnpm one throughout.
pack_ui() {
	(cd "$UI_DIR" && pnpm pack --pack-destination "$PACK_DIR") && test -f "$PACK_DIR/$UI_TARBALL_NAME"
}

pack_standards() {
	(cd "$STANDARDS_DIR" && pnpm pack --pack-destination "$PACK_DIR") && test -f "$PACK_DIR/$STANDARDS_TARBALL_NAME"
}

lint_manifest() {
	(cd "$SDK_DIR" && pnpm exec publint --strict "$PACK_DIR/$TARBALL_NAME")
}

# The package is ESM only, so the check uses the profile that ignores the
# CommonJS resolutions.
lint_types() {
	(cd "$SDK_DIR" && pnpm exec attw --profile esm-only "$PACK_DIR/$TARBALL_NAME")
}

# The library's tarball is read by publint alone. @arethetypeswrong/cli asks
# whether Node can resolve every entry point, and two answers here are no for
# reasons no change to the package would fix:
#
#   - `./tokens.css` is a stylesheet. No JavaScript resolver finds a type for
#     it, so every resolution it tries fails
#   - svelte2tsx writes relative imports without a file extension into the
#     `.d.ts` files, and Node's ESM resolution needs them written out. That is
#     45 internal resolution errors, one per import, and none of them is a
#     file the tarball is missing
#
# Both are silent for a bundler, which is what reads this package, and `attw`
# reports bundler resolution green for every subpath but the stylesheet.
lint_ui_manifest() {
	(cd "$SDK_DIR" && pnpm exec publint --strict "$PACK_DIR/$UI_TARBALL_NAME")
}

# Standards ships no `types` entry, so it is read by publint alone, the way
# the library's tarball is: attw would only ask an entry point it does not
# have to resolve.
lint_standards_manifest() {
	(cd "$SDK_DIR" && pnpm exec publint --strict "$PACK_DIR/$STANDARDS_TARBALL_NAME")
}

registry_answers() {
	node -e "fetch(process.argv[1]).then(r => process.exit(r.ok ? 0 : 1)).catch(() => process.exit(1))" "${REGISTRY}-/ping"
}

# A registry that already holds this version refuses the publish below, and a
# developer republishing a dev version after an edit is the normal case here.
# Removing it first makes the publish the one this run judges. A registry that
# holds nothing answers 404, which is not a failure.
unpublish_stale() {
	(cd "$SDK_DIR" && npm unpublish --force --registry "$REGISTRY" "$AUTH_ARG" \
		"@prusa/connect@${VERSION}") || true
}

unpublish_stale_ui() {
	(cd "$UI_DIR" && npm unpublish --force --registry "$REGISTRY" "$AUTH_ARG" \
		"@prusa/connect-ui@${VERSION}") || true
}

unpublish_stale_standards() {
	(cd "$STANDARDS_DIR" && npm unpublish --force --registry "$REGISTRY" "$AUTH_ARG" \
		"@prusa/frontend-standards@${VERSION}") || true
}

publish_sdk() {
	(cd "$SDK_DIR" && npm publish --registry "$REGISTRY" "$AUTH_ARG" \
		--tag "$DIST_TAG" "$PACK_DIR/$TARBALL_NAME")
}

# After the SDK, because the peer names the version the line above published
# and an install resolves the peer against what the registry holds.
publish_ui() {
	(cd "$UI_DIR" && npm publish --registry "$REGISTRY" "$AUTH_ARG" \
		--tag "$DIST_TAG" "$PACK_DIR/$UI_TARBALL_NAME")
}

publish_standards() {
	(cd "$STANDARDS_DIR" && npm publish --registry "$REGISTRY" "$AUTH_ARG" \
		--tag "$DIST_TAG" "$PACK_DIR/$STANDARDS_TARBALL_NAME")
}

echo "publishing @prusa/connect@${VERSION}, @prusa/connect-ui@${VERSION} and @prusa/frontend-standards@${VERSION} to ${REGISTRY} under the ${DIST_TAG} dist-tag"

step "the registry answers" registry_answers
step "the SDK builds" build_sdk
step "the component library builds" build_ui
step "the three manifests carry ${VERSION}" stamp
step "npm pack writes $TARBALL_NAME" pack_sdk
step "pnpm pack writes $UI_TARBALL_NAME" pack_ui
step "pnpm pack writes $STANDARDS_TARBALL_NAME" pack_standards
step "the three manifests are back as the tree wrote them" unstamp
step "publint accepts the SDK's tarball" lint_manifest
step "arethetypeswrong accepts the SDK's tarball" lint_types
step "publint accepts the library's tarball" lint_ui_manifest
step "publint accepts the standards package's tarball" lint_standards_manifest
step "an earlier publish of the SDK at this version is removed" unpublish_stale
step "the SDK's tarball is published" publish_sdk
step "an earlier publish of the library at this version is removed" unpublish_stale_ui
step "the library's tarball is published" publish_ui
step "an earlier publish of the standards package at this version is removed" unpublish_stale_standards
step "the standards package's tarball is published" publish_standards

echo "OK    @prusa/connect@${VERSION}, @prusa/connect-ui@${VERSION} and @prusa/frontend-standards@${VERSION} are in ${REGISTRY}"

}
main "$@"
