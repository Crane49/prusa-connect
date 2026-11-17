#!/bin/sh
# The two client packages, installed and run the way a consumer would, before
# any release publishes them.
#
# `make client-sdk-check` and `make client-ui-check` prove each package on the
# tree: it builds, its tests pass against its build. Nothing there reads the
# `exports` map, the `files` list or the shipped `.d.ts` files the way an
# install does, and each of those fails only on the far side of a registry.
# This lane is that far side, held local. It packs `@prusa/connect` from
# packages/typescript and `@prusa/connect-ui` from packages/svelte,
# publishes all three tarballs to the registry fixture
# docker/npm-registry-fixture runs, and installs them back into four consumer
# projects under packages/typescript/testdata/consumers:
#
#   node-esm      plain Node, ESM, no bundler. Built, type-checked, and run
#                 against a server this lane starts: it has to list at least
#                 one device and read at least one event
#   browser-vite  a page bundled by Vite with no framework. Built and
#                 type-checked; the bundle has to carry the SDK's routes
#   svelte        a Svelte 5 component bundled by Vite, no SvelteKit. Checked
#                 by svelte-check, built, same bundle assertion
#   svelte-ui     the same shape with the component library and the
#                 standards package on top. It installs all three packages,
#                 sets Tailwind up the way a module author does, draws the
#                 library's primitives, and holds the drawing in a real
#                 Chromium
#
# The library is published after the SDK, because its peer names that exact
# version and an install resolves the peer against what the registry holds.
#
# Building, packing, reading and publishing the three packages is
# scripts/registry-publish.sh, which `make registry-publish` also calls to
# send the same three packages to the developer's own registry. It runs
# publint over all three tarballs and @arethetypeswrong/cli over the SDK's,
# and it prints which of its steps failed.
#
# Each consumer is copied to a scratch directory first. Its dev tools install
# from the real registry against the committed lockfile, and the SDK is then
# added through the consumer's .npmrc, which sends the @prusa scope to the
# fixture and nothing else there. The fixture proxies nothing, so an install
# that needed anything but the SDK from it fails in the open. The tree is not
# written to: the package.json and lockfile the add rewrites are the copy's.
#
# The server is cmd/demo. It serves a login for the demo account and prints
# that account's password once, in its banner, so this lane signs in the way
# a person does, mints a credential over the same two routes, and hands the
# token to the Node consumer. Readiness is read from /api/v1/health, the one
# route that takes no identity.
#
# A failure names the consumer and the step. The fixture is started and removed
# by the Makefile target, on test-adapters' model; this script expects it to be
# answering on NPM_FIXTURE_PORT.
#
# The whole run is one function, called on the last line, so the shell parses
# this file to its end before executing any of it. sh reads a script while it
# runs, and an edit landing mid-run moves the read offset under the reader.
main() {

ROOT=$(cd "$(dirname "$0")/.." && pwd)
cd "$ROOT" || exit 1

NPM_FIXTURE_PORT=${NPM_FIXTURE_PORT:-54873}
export NPM_FIXTURE_PORT
REGISTRY="http://127.0.0.1:${NPM_FIXTURE_PORT}/"
SERVER_PORT=${SDK_CHECK_SERVER_PORT:-58180}
SERVER_URL="http://127.0.0.1:${SERVER_PORT}"

SDK_DIR=packages/typescript
UI_DIR=packages/svelte
CONSUMERS_DIR=$SDK_DIR/testdata/consumers
CONSUMERS="node-esm browser-vite svelte svelte-ui"

# Both packages have the version in VERSION, and `pnpm pack` names the tarball
# from it. A package.json with another version writes a file under another
# name, and the pack step below fails on it.
VERSION=$(tr -d '[:space:]' < VERSION)
TARBALL_NAME="prusa-connect-${VERSION}.tgz"
UI_TARBALL_NAME="prusa-connect-ui-${VERSION}.tgz"

for tool in docker pnpm node npm go; do
	if ! command -v "$tool" > /dev/null 2>&1; then
		echo "FAIL  $tool is not on PATH, and this lane needs it."
		exit 1
	fi
done

WORK=$(mktemp -d)
DEMO_PID=""

cleanup() {
	if [ -n "$DEMO_PID" ]; then
		kill "$DEMO_PID" 2> /dev/null || true
		wait "$DEMO_PID" 2> /dev/null || true
	fi
	rm -rf "$WORK"
}
trap cleanup EXIT INT TERM

# step <consumer or -> <name> <command...>. The command's output goes to a
# file and is printed only when it fails, so a green run reads as a list of
# steps and a red one shows the one that broke, named.
STEPS=0
step() {
	WHO=$1
	WHAT=$2
	shift 2
	STEPS=$((STEPS + 1))

	if [ "$WHO" = "-" ]; then
		LABEL=$WHAT
	else
		LABEL="$WHO: $WHAT"
	fi

	printf '%2d. %s...\n' "$STEPS" "$LABEL"

	if "$@" > "$WORK/step.log" 2>&1; then
		return 0
	else
		RC=$?
	fi

	echo ""
	echo "FAIL  $LABEL (exit $RC)"
	sed 's/^/      /' "$WORK/step.log"
	if [ -n "$DEMO_PID" ] && [ -s "$WORK/demo.log" ]; then
		echo "      the server's log:"
		sed 's/^/      /' "$WORK/demo.log"
	fi
	exit 1
}

# answers <url> asks whether an HTTP route answers 200. node is the one HTTP
# client every machine this runs on has.
answers() {
	node -e "fetch(process.argv[1]).then(r => process.exit(r.ok ? 0 : 1)).catch(() => process.exit(1))" "$1"
}

# ── The two packages ────────────────────────────────────────────────────

# The library's peer on the SDK is an exact version, so a consumer installing
# both gets one copy of the client and the library draws against the same one.
# A peer left at an older version would resolve from the real registry, or
# from nothing, rather than from the tarball this run published.
#
# This is asked of the tree before anything is published. The publish below
# stamps the version into both manifests itself, so without this check a peer
# that had drifted would be repaired on the way out and the tree would keep
# the defect.
ui_peer_is_this_version() {
	HAVE=$(node -e "const {readFileSync}=require('node:fs');process.stdout.write(JSON.parse(readFileSync(process.argv[1],'utf8')).peerDependencies['@prusa/connect'])" "$UI_DIR/package.json")
	if [ "$HAVE" != "$VERSION" ]; then
		echo "$UI_DIR/package.json peers @prusa/connect at '$HAVE', and this tree publishes $VERSION"
		return 1
	fi
}

# Building, packing, reading and publishing all three packages is
# scripts/registry-publish.sh, because `make registry-publish` sends the same
# three packages to the developer's own registry and two copies of that many
# steps would drift. It writes all three tarballs into WORK, which the
# integrity checks below read back.
#
# The token is a placeholder: npm refuses to publish with no token configured
# before it sends anything, and the fixture accepts an unauthenticated publish
# of the scope, so any value satisfies both.
publish_both() {
	bash scripts/registry-publish.sh --registry "$REGISTRY" --version "$VERSION" \
		--pack-dir "$WORK" --token fixture
}

# ── The server ──────────────────────────────────────────────────────────

build_demo() {
	go build -o "$WORK/demo$(go env GOEXE)" ./cmd/demo
}

# The demo's data directory is under WORK, so a server that had to be killed
# rather than stopped leaves nothing behind once WORK is removed.
start_demo() {
	"$WORK/demo$(go env GOEXE)" -addr "127.0.0.1:${SERVER_PORT}" -dir "$WORK/demo-data" \
		-flap 0 -cameras=false > "$WORK/demo.log" 2>&1 &
	DEMO_PID=$!

	i=0
	while [ "$i" -lt 60 ]; do
		if answers "${SERVER_URL}/api/v1/health"; then
			return 0
		fi
		if ! kill -0 "$DEMO_PID" 2> /dev/null; then
			echo "the demo exited before it served"
			return 1
		fi
		i=$((i + 1))
		sleep 1
	done

	echo "the demo did not answer on ${SERVER_URL} within 60 seconds"
	return 1
}

# mint_token signs the demo account in and mints a credential for the Node
# consumer, over the two routes the demo's banner prints. The password is read
# from that banner: the demo generates a new one on every run and prints it
# once, on the terminal it owns, which here is demo.log. The session lives in
# the cookie the login sets; the consumer then carries the token and nothing
# else, which is the shape a script sends.
#
# The demo prints the banner once the fleet has dialled in, which is after
# /api/v1/health answers, so this waits for the line rather than reading the
# log once.
mint_token() {
	waited=0
	PASSWORD=""
	while [ "$waited" -lt 60 ]; do
		PASSWORD=$(sed -n 's/^  password  //p' "$WORK/demo.log" | head -n 1)
		[ -n "$PASSWORD" ] && break

		waited=$((waited + 1))
		sleep 1
	done

	if [ -z "$PASSWORD" ]; then
		echo "the demo's banner names no password after 60 seconds"
		return 1
	fi

	TOKEN=$(node -e '
const url = process.argv[1];
const password = process.argv[2];
const json = { "content-type": "application/json" };
(async () => {
  const login = await fetch(url + "/api/v1/auth/login", {
    method: "POST",
    headers: json,
    body: JSON.stringify({ username: "demo", password }),
  });
  if (!login.ok) throw new Error("the login answered " + login.status);
  const cookie = login.headers.getSetCookie().map((c) => c.split(";")[0]).join("; ");
  const minted = await fetch(url + "/api/v1/auth/credentials", {
    method: "POST",
    headers: { ...json, cookie },
    body: JSON.stringify({ name: "install-check" }),
  });
  if (minted.status !== 201) throw new Error("minting a credential answered " + minted.status);
  const { token } = await minted.json();
  if (!token) throw new Error("the minted credential carries no token");
  process.stdout.write(token);
})().catch((err) => {
  console.error(err.message);
  process.exit(1);
});
' "$SERVER_URL" "$PASSWORD") || return 1

	[ -n "$TOKEN" ]
}

# ── The consumers ───────────────────────────────────────────────────────

copy_consumer() {
	rm -rf "$WORK/$1"
	cp -R "$CONSUMERS_DIR/$1" "$WORK/$1" && rm -rf "$WORK/$1/node_modules" "$WORK/$1/dist"
}

install_dev_tools() {
	(cd "$WORK/$1" && pnpm install --frozen-lockfile)
}

# The metadata cache is this run's own. pnpm answers an exact version from
# the cached document without asking the registry, and the first run of this
# lane proved it: an unpublish and a republish of the same version left the
# consumer holding the earlier tarball out of the store, and the integrity
# check below is what said so.
install_sdk() {
	(cd "$WORK/$1" && pnpm add --cache-dir "$WORK/pnpm-cache" "@prusa/connect@${VERSION}")
}

# All three packages in one add, at exact versions, the way a module author
# writes them into a project. The library's peer on the SDK is satisfied by
# the copy this line installs beside it.
install_both() {
	(cd "$WORK/$1" && pnpm add --cache-dir "$WORK/pnpm-cache" \
		"@prusa/connect@${VERSION}" "@prusa/connect-ui@${VERSION}" \
		"@prusa/frontend-standards@${VERSION}")
}

# records_this_build <consumer> <package> <tarball>. The lockfile the add wrote
# carries the integrity of what it installed, and the tarball this run
# published has one integrity. The two agreeing is the proof that the package
# under node_modules is this tree's build and not a version from anywhere else.
records_this_build() {
	WANT=$(node -e "const {createHash}=require('node:crypto');const {readFileSync}=require('node:fs');process.stdout.write('sha512-'+createHash('sha512').update(readFileSync(process.argv[1])).digest('base64'))" "$WORK/$3")
	if ! grep -F -A 1 "'$2@${VERSION}':" "$WORK/$1/pnpm-lock.yaml" | grep -qF "$WANT"; then
		echo "the lockfile does not record the published tarball's integrity ($WANT):"
		grep -F -A 1 "'$2@${VERSION}':" "$WORK/$1/pnpm-lock.yaml"
		return 1
	fi
}

installed_is_this_build() {
	records_this_build "$1" "@prusa/connect" "$TARBALL_NAME" &&
		test -f "$WORK/$1/node_modules/@prusa/connect/dist/index.js"
}

ui_installed_is_this_build() {
	records_this_build "$1" "@prusa/connect-ui" "$UI_TARBALL_NAME" &&
		test -f "$WORK/$1/node_modules/@prusa/connect-ui/dist/index.js"
}

# Standards ships no build, so there is no tarball integrity to check back
# against a dist file the way the two built packages are: its own presence,
# with the `.d.ts` an ESLint config author's editor reads, is the proof the
# install resolved it.
standards_eslint_types_installed() {
	test -f "$WORK/$1/node_modules/@prusa/frontend-standards/eslint/module.d.ts"
}

# Every file the installed `exports` map names has to be there. The map has one
# entry per subpath and two or three targets per entry, and a build that wrote
# no chunk for a subpath, or a `files` list that shipped no declarations,
# leaves a package that resolves to nothing the day a module author imports
# that subpath. Nothing on the tree asks this question: `vite build` writes
# what its entry list names, and the entry list is a second copy of the map.
ui_dist_carries_every_subpath() {
	node -e '
const { existsSync, readFileSync } = require("node:fs");
const { join } = require("node:path");

const root = process.argv[1];
const manifest = JSON.parse(readFileSync(join(root, "package.json"), "utf8"));

const named = [];
const missing = [];
const walk = (value) => {
  if (typeof value === "string") {
    named.push(value);
    if (!existsSync(join(root, value))) missing.push(value);
    return;
  }
  for (const nested of Object.values(value)) walk(nested);
};
walk(manifest.exports);

if (missing.length > 0) {
  console.error("the installed package names files it does not carry:");
  for (const path of missing) console.error("  " + path);
  process.exit(1);
}
console.log(named.length + " targets of the exports map are present");
' "$WORK/$1/node_modules/@prusa/connect-ui"
}

type_check() {
	(cd "$WORK/$1" && pnpm run check)
}

build_consumer() {
	(cd "$WORK/$1" && pnpm run build)
}

# A bundler that failed to resolve the package would have stopped the build.
# What it can do silently is leave the import external, and then the page has
# no client in it. The two routes every consumer calls are the proof the
# SDK was bundled.
bundle_carries_sdk() {
	for route in /api/v1/devices /api/v1/events; do
		if ! grep -qF "$route" "$WORK/$1"/dist/assets/*.js; then
			echo "no file under dist/assets names $route, so the bundle does not carry the SDK"
			ls -l "$WORK/$1"/dist/assets
			return 1
		fi
	done
}

# The same question for the library: a line only its own catalogue holds, and a
# token only its theme declares. A bundler that left the library external, or a
# stylesheet that never reached Tailwind, builds a page with neither and stops
# nothing on its own.
# The bundle is known by a catalogue key a primitive reads, not by the words
# the key answers: the words follow the voice of the catalogue and change, the
# key is kept by the i18n rule that no key is renamed.
bundle_carries_library() {
	if ! grep -qF 'primitives.copy' "$WORK/$1"/dist/assets/*.js; then
		echo "no file under dist/assets carries a primitive's catalogue key, so the bundle does not carry the library"
		ls -l "$WORK/$1"/dist/assets
		return 1
	fi
	if ! grep -qF -- '--color-background' "$WORK/$1"/dist/assets/*.css; then
		echo "no stylesheet under dist/assets declares the theme's tokens"
		ls -l "$WORK/$1"/dist/assets
		return 1
	fi
}

# The consumer's own test suite. For svelte-ui that is one browser-mode Vitest
# file in a real Chromium, which asks whether a primitive drew what it was
# handed.
test_consumer() {
	(cd "$WORK/$1" && pnpm run test)
}

# The Node consumer's own verdict: at least one device listed and one event
# read, or exit 1 with a FAIL line. Its output is what the lane reports.
run_consumer() {
	(cd "$WORK/$1" && PCL_URL="$SERVER_URL" PCL_TOKEN="$TOKEN" node dist/main.js | tee "$WORK/run.log") &&
		grep -q '^OK ' "$WORK/run.log"
}

echo "registry ${REGISTRY}, server ${SERVER_URL}, packages @prusa/connect@${VERSION}, @prusa/connect-ui@${VERSION} and @prusa/frontend-standards@${VERSION}"

step - "the library peers the SDK at this version" ui_peer_is_this_version
step - "all three packages are built, read and published to the registry fixture" publish_both
step - "the demo server builds" build_demo
step - "the demo server answers with simulated printers" start_demo
step - "the demo account signs in and mints a token" mint_token

for consumer in $CONSUMERS; do
	step "$consumer" "copied to scratch" copy_consumer "$consumer"
	step "$consumer" "dev tools install from the lockfile" install_dev_tools "$consumer"

	case $consumer in
	svelte-ui)
		step "$consumer" "both packages install from the registry" install_both "$consumer"
		step "$consumer" "the installed SDK is this build" installed_is_this_build "$consumer"
		step "$consumer" "the installed library is this build" ui_installed_is_this_build "$consumer"
		step "$consumer" "the installed standards package carries its ESLint types" standards_eslint_types_installed "$consumer"
		step "$consumer" "the library carries every subpath its exports map names" ui_dist_carries_every_subpath "$consumer"
		;;
	*)
		step "$consumer" "the SDK installs from the registry" install_sdk "$consumer"
		step "$consumer" "the installed package is this build" installed_is_this_build "$consumer"
		;;
	esac

	step "$consumer" "type check" type_check "$consumer"
	step "$consumer" "build" build_consumer "$consumer"

	case $consumer in
	node-esm)
		step "$consumer" "runs against the server" run_consumer "$consumer"
		sed 's/^/      /' "$WORK/run.log"
		;;
	svelte-ui)
		step "$consumer" "the bundle carries the library and the theme" bundle_carries_library "$consumer"
		step "$consumer" "a primitive draws in a real browser" test_consumer "$consumer"
		;;
	*)
		step "$consumer" "the bundle carries the SDK" bundle_carries_sdk "$consumer"
		;;
	esac
done

echo "OK    client package install gates passed: $STEPS steps, $(echo "$CONSUMERS" | wc -w | tr -d ' ') consumers"

}
main "$@"
