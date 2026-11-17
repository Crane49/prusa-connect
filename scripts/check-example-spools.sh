#!/bin/sh
# Both proof extensions, built, installed and driven through the API
# (ADR-20260911-4p6 rules 9, 26 and 30):
#
#   examples/spools        a module with a Go half and a browser half, adding a
#                          sidebar item and a page of its own
#   examples/nozzle-watch  a module that is one browser bundle, adding a section
#                          to the device card
#
# And one fixture that must never run:
#
#   test/modules/library-mismatch  a module whose `library` range excludes every
#                          version of the component library this repository
#                          publishes, so the app installs it and refuses it
#                         
#
# What it does, in order:
#
#   1. builds both examples and lays each one out as a release, by calling
#      scripts/pack-example-modules.sh. `make demo` calls the same script, so
#      the layout this gate proves is the layout the demo offers
#   2. vets and tests the spools Go half, and holds its typed client to its
#      OpenAPI fragment
#   3. lints and type-checks each browser half, and runs its tests in Chromium
#   4. installs both with `prusa-connect module add`, into a scratch
#      modules directory and a scratch overlay
#   5. starts the demo on that configuration, signs in, and asserts through the
#      API that the listing shows one line per module, that the spools routes
#      answer, and that the bundle answers under the same namespace
#   6. holds the app to the range the fixture declares: the listing carries it,
#      and the loader refuses the module for it and stops before the import
#   7. drives the app in a real Chromium against that same demo: the spools
#      page is in the navigation and opens, the nozzle section is on the
#      printer card, and the refused fixture is named in Settings with its
#      reason while the rest of the app keeps working
#
# No digest is written by hand anywhere here. `module add` records what it
# installed, which is the whole point of the command: a declaration written by
# hand goes stale the first time somebody rebuilds.
#
# The whole run is one function, called on the last line, so the shell parses
# this file to its end before executing any of it. sh reads a script while it
# runs, and an edit landing mid-run moves the read offset under the reader.
main() {

ROOT=$(cd "$(dirname "$0")/.." && pwd)
cd "$ROOT" || exit 1

MODULE_DIR=examples/spools
CLIENT_DIR=$MODULE_DIR/client
UI_DIR=$MODULE_DIR/ui

# The second proof extension. It has one half, so it has one directory here.
NOZZLE_UI_DIR=examples/nozzle-watch/ui

# The core's own app. The last step drives it in a browser against the demo
# this run starts, which is where an installed module is finally seen.
APP_DIR=frontend
FRACTAL_CLIENT_DIR=packages/fractal/client

# The fixture the app must refuse, and the component library whose loader does
# the refusing.
MISMATCH_DIR=test/modules/library-mismatch
CLIENT_UI_DIR=packages/svelte

# Everything this run writes goes under bin/, which is ignored, and that is
# where a build artefact belongs (go-standards.md).
WORK_ROOT=bin/spools
RELEASE_ROOT=$WORK_ROOT/release
MODULES_DIR=$WORK_ROOT/modules
DEMO_DIR=$WORK_ROOT/demo
OUT_CONFIG=${SPOOLS_CONFIG:-$WORK_ROOT/config.yaml}

# The module path each example is installed from, and the namespace the host
# derives from it. The author never writes the namespace: it is the reversed
# module path, and every address of the module is built from it.
SPOOLS_MODULE=github.com/acme/spools
SPOOLS_NAMESPACE=com.github.aux.acme.spools
NOZZLE_MODULE=github.com/acme/nozzle-watch
NOZZLE_NAMESPACE=com.github.aux.acme.nozzle-watch
MISMATCH_MODULE=github.com/acme/library-mismatch
MISMATCH_NAMESPACE=com.github.aux.acme.library-mismatch

# The versions the two manifests declare. `module add` refuses a release whose
# manifest says another one.
SPOOLS_VERSION=1.0.0
NOZZLE_VERSION=1.0.0
MISMATCH_VERSION=1.0.0

# The address the demo binds for this run. It is not 8080, so a run does not
# collide with a server somebody already has up.
DEMO_ADDR=127.0.0.1:18099
BASE_URL=http://$DEMO_ADDR

# CI=true keeps pnpm from asking anything, and keeps its output readable.
CI=true
export CI

for tool in go pnpm sha256sum curl; do
	if ! command -v "$tool" > /dev/null 2>&1; then
		echo "FAIL  $tool is not on PATH, and this lane needs it."
		exit 1
	fi
done

WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT INT TERM

STEPS=0
step() {
	WHAT=$1
	shift
	STEPS=$((STEPS + 1))

	printf '%2d. %s...\n' "$STEPS" "$WHAT"

	if "$@" > "$WORK/step.log" 2>&1; then
		return 0
	fi

	echo "FAIL  $WHAT"
	cat "$WORK/step.log"
	exit 1
}

rm -rf "$WORK_ROOT"
mkdir -p "$MODULES_DIR" "$DEMO_DIR"

# The release each example ships, in the layout `module add` reads (earlier research
# recommendation 3): the manifest at the root, this platform's binary beside
# it, the built bundle under ui/, and a checksums.txt over all of them.
#
# The packing is scripts/pack-example-modules.sh and not this file, because
# `make demo` packs the same two examples and offers them on the extensions
# page. Two copies of that shell would drift, and the first sign of it would be
# a demo installing a layout no gate ever proved. PACK_ROOT is under the scratch
# root this run just cleared, so every archive and every digest here is new.
SPOOLS_RELEASE=$RELEASE_ROOT/spools
NOZZLE_RELEASE=$RELEASE_ROOT/nozzle-watch
MISMATCH_RELEASE=$RELEASE_ROOT/library-mismatch

echo " 0. both examples build and pack as releases..."
PACK_ROOT=$RELEASE_ROOT
export PACK_ROOT

if ! sh scripts/pack-example-modules.sh; then
	echo "FAIL  the examples did not pack"
	exit 1
fi

# The fixture is packed here rather than by that script, which builds what it
# packs. This one has no build: its bundle is a single line that throws, and
# the demo has no reason to offer a module nobody may switch on.
pack_fixture() {
	rm -rf "$MISMATCH_RELEASE"
	mkdir -p "$MISMATCH_RELEASE"
	cp -R "$MISMATCH_DIR"/. "$MISMATCH_RELEASE"/
	(
		cd "$MISMATCH_RELEASE" || exit 1
		find . -type f ! -name checksums.txt | sed 's|^\./||' | LC_ALL=C sort |
			while IFS= read -r file; do
				sha256sum "$file" || exit 1
			done > checksums.txt
	)
}

step "the refused fixture packs as a release" pack_fixture

step "the Go half vets" go vet "./$MODULE_DIR/"
step "the Go half's routes, fragment and manifest hold" go test -count=1 "./$MODULE_DIR/"

# The typed client. It is generated from the same fragment the module embeds,
# so -check fails when the fragment moved and nobody regenerated.
step "the typed client is what the fragment generates" \
	go run ./cmd/generate-client-sdk -module -check \
	-document "$MODULE_DIR/openapi.json" \
	-namespace "$SPOOLS_NAMESPACE" -name spools-client -out "$CLIENT_DIR"

step "the client types match the fragment" sh -c \
	"cd '$CLIENT_DIR' && pnpm run generate:types && git diff --exit-code src/generated/"

step "the spools browser half lints, type-checks and its tests pass in Chromium" sh -c \
	"cd '$UI_DIR' && pnpm run check"
step "nozzle-watch lints, type-checks and its tests pass in Chromium" sh -c \
	"cd '$NOZZLE_UI_DIR' && pnpm run check"

# built_release refuses a release the host would have nothing to serve out of.
built_release() {
	if [ ! -f "$1/module.yaml" ] || [ ! -f "$1/ui/index.js" ] || [ ! -f "$1/checksums.txt" ]; then
		echo "FAIL  $1 is missing module.yaml, ui/index.js or checksums.txt"
		exit 1
	fi
}

built_release "$SPOOLS_RELEASE"
built_release "$NOZZLE_RELEASE"
built_release "$MISMATCH_RELEASE"

# The operator's file. It names where the machine-owned overlay goes, where
# modules are installed, and the one record backend nozzle-watch's
# `mark-changed` needs: the demo declares no `db` kind of its own (it is a
# fixture for a fleet, not for a module's state), so a deployment that wants
# `host.store` served declares one, the way this file does.
#
# Every path here is relative to the directory the server is started from,
# which is the repository root. An absolute one written here would be this
# shell's own spelling of it, and on Windows that is a path Go cannot open.
cat > "$OUT_CONFIG" <<- YAML
	core:
	  overlay: $DEMO_DIR/resources.json
	  modules: $MODULES_DIR
	resources:
	  db:
	    main:
	      type: sqlite
	      config:
	        path: $WORK_ROOT/state.db
YAML

CORE=$WORK_ROOT/prusa-connect$(go env GOEXE)
# The binary is not called demo: $DEMO_DIR is $WORK_ROOT/demo, and on Linux, where
# GOEXE is empty, a binary of that name and the directory would be one path.
DEMO=$WORK_ROOT/demo-server$(go env GOEXE)

step "the core and the demo build" sh -c 	"go build -o '$CORE' ./cmd/prusa-connect && go build -o '$DEMO' ./cmd/demo"

# One command per module, and one declaration out of each. -approve writes the
# ceiling the manifest asks for and switches the instance on, which is what an
# operator does after reading what the module asked for.
step "the spools module installs" "$CORE" module add "$SPOOLS_MODULE@$SPOOLS_VERSION" 	-config "$OUT_CONFIG" -from "$SPOOLS_RELEASE" -approve
step "nozzle-watch installs" "$CORE" module add "$NOZZLE_MODULE@$NOZZLE_VERSION" 	-config "$OUT_CONFIG" -from "$NOZZLE_RELEASE" -approve

# The fixture installs the same way as the other two. Nothing here knows it
# will be refused: the range is the app's business and the core carries it.
step "the refused fixture installs" "$CORE" module add "$MISMATCH_MODULE@$MISMATCH_VERSION" 	-config "$OUT_CONFIG" -from "$MISMATCH_RELEASE" -approve

step "the listing on disk holds one line per module" sh -c 	"'$CORE' module list -config '$OUT_CONFIG' | tee '$WORK/list.txt' && 	 [ \"\$(grep -c 'com.github.aux.acme' '$WORK/list.txt')\" = 3 ]"

# The demo, on that configuration. It lays its own settings over the file, and
# the overlay it uses is the one `module add` just wrote, so both modules are
# reconciled at its start.
"$DEMO" -config "$OUT_CONFIG" -dir "$DEMO_DIR" -addr "$DEMO_ADDR" 	-printers 1 -flap 0 -cameras=false > "$WORK/demo.log" 2>&1 &
DEMO_PID=$!
trap 'kill "$DEMO_PID" 2> /dev/null; rm -rf "$WORK"' EXIT INT TERM

# ready polls the liveness path until the core answers, so nothing below races
# the start.
ready() {
	waited=0
	while [ "$waited" -lt 120 ]; do
		if curl -fsS "$BASE_URL/api/v1/health" > /dev/null 2>&1; then
			return 0
		fi

		if ! kill -0 "$DEMO_PID" 2> /dev/null; then
			echo "the demo ended before it served" >&2
			cat "$WORK/demo.log" >&2
			return 1
		fi

		waited=$((waited + 1))
		sleep 1
	done

	cat "$WORK/demo.log" >&2
	return 1
}

step "the demo serves" ready

# The demo prints its account and a new password on every run, so the password
# is read off what it printed rather than written down here. It prints that
# line once the fleet has dialled in, which is after the core is answering, so
# this waits for the line rather than reading the log once.
read_password() {
	waited=0
	while [ "$waited" -lt 60 ]; do
		PASSWORD=$(sed -n 's/^ *password  *//p' "$WORK/demo.log" | head -1)
		if [ -n "$PASSWORD" ]; then
			return 0
		fi

		waited=$((waited + 1))
		sleep 1
	done

	cat "$WORK/demo.log" >&2
	return 1
}

step "the demo prints an account to sign in with" read_password

sign_in() {
	curl -fsS -c "$WORK/cookies" -X POST "$BASE_URL/api/v1/auth/login" 		-H 'content-type: application/json' 		-d "{\"username\":\"demo\",\"password\":\"$PASSWORD\"}" > /dev/null
}

step "the demo account signs in" sign_in

# The listing, once both modules are running. The `ui` object appears for an
# enabled and ready instance only (ADR-20260911-4p6 rule 32), and a module's process
# takes a moment to answer its handshake, so this waits for the state rather
# than reading the listing once and calling a race a failure.
listing() {
	waited=0
	while [ "$waited" -lt 60 ]; do
		if curl -fsS -b "$WORK/cookies" "$BASE_URL/api/v1/resources" > "$WORK/resources.json" &&
			grep -q "$SPOOLS_NAMESPACE/ui/ui/index.js" "$WORK/resources.json" &&
			grep -q "$NOZZLE_NAMESPACE/ui/ui/index.js" "$WORK/resources.json" &&
			grep -q "$MISMATCH_NAMESPACE/ui/ui/index.js" "$WORK/resources.json"; then
			return 0
		fi

		waited=$((waited + 1))
		sleep 1
	done

	cat "$WORK/resources.json" >&2
	return 1
}

step "every module is running and publishes a bundle to import" listing

# holds greps the listing for one string, so a failure says which claim broke
# rather than printing a wall of JSON.
holds() {
	step "the listing $1" grep -q "$2" "$WORK/resources.json"
}

# One line for spools, carrying both halves: the routes under the namespace and
# the bundle under the same one (ADR-20260911-4p6 rule 30).
holds "shows spools as one sink instance" '"kind":"sink","name":"spools"'
holds "serves the spools routes under its namespace" "\"namespace\":\"$SPOOLS_NAMESPACE\""
holds "publishes the spools bundle under the same namespace" \
	"\"entry_url\":\"/api/v1/modules/$SPOOLS_NAMESPACE/ui/ui/index.js\""
holds "shows nozzle-watch as one asset instance" '"kind":"asset","name":"nozzle-watch"'
holds "publishes the nozzle-watch bundle" \
	"\"entry_url\":\"/api/v1/modules/$NOZZLE_NAMESPACE/ui/ui/index.js\""

# The range the fixture declares, read out of its manifest so this file and the
# app's own test cannot disagree about which range is being proved.
MISMATCH_RANGE=$(sed -n 's/^  library:[ \t]*"\(.*\)"/\1/p' "$MISMATCH_DIR/module.yaml" | head -1)
if [ -z "$MISMATCH_RANGE" ]; then
	echo "FAIL  $MISMATCH_DIR/module.yaml declares no library range"
	exit 1
fi

holds "shows the fixture as one asset instance" '"kind":"asset","name":"library-mismatch"'
holds "carries the range the fixture declares" "\"library\":\"$MISMATCH_RANGE\""

# The other half of the claim. The core carries the range and the app decides
# on it, so the refusal is read where it is recorded: the loader's own state,
# which is what the settings screen draws (ADR-20260911-4p6 rule 24). The fixture's
# entry throws while it evaluates, so an import would show up as a refusal of
# another kind rather than as a pass.
step "the app refuses the fixture for its range and never imports its entry" sh -c \
	"cd '$CLIENT_UI_DIR' && pnpm exec vitest run src/slots/refused-module.svelte.test.ts --project browser"

# And no second instance anywhere for the browser half.
step "no second instance is declared for the browser half" sh -c \
	"! grep -q 'spools-ui' '$WORK/resources.json'"

step "the spools routes answer" sh -c \
	"curl -fsS -b '$WORK/cookies' '$BASE_URL/api/v1/modules/$SPOOLS_NAMESPACE/spools' > /dev/null"

# The bundle, under the same namespace. Its entity tag is the digest the
# install recorded, so the tag moves only when the module is installed again,
# and the check reads that digest back out of the declaration.
entity_tag() {
	curl -fsS -D "$WORK/headers.txt" -o /dev/null -b "$WORK/cookies" \
		"$BASE_URL/api/v1/modules/$SPOOLS_NAMESPACE/ui/ui/index.js" || return 1

	tag=$(grep -io '[0-9a-f]\{64\}' "$WORK/headers.txt" | head -1)
	if [ -z "$tag" ]; then
		echo "the bundle answered with no digest as its entity tag" >&2
		cat "$WORK/headers.txt" >&2
		return 1
	fi

	grep -q "sha256:$tag" "$DEMO_DIR/resources.json"
}

step "the spools bundle answers with the recorded digest as its entity tag" entity_tag

step "the nozzle-watch bundle answers" sh -c \
	"curl -fsS -o /dev/null -b '$WORK/cookies' \
	   '$BASE_URL/api/v1/modules/$NOZZLE_NAMESPACE/ui/ui/index.js'"

# The other end of the whole chain: the app itself, in a real Chromium, in
# front of the demo that is still running above. Everything until here proves
# the core serves a module; this proves a person sees it. It is the app's own
# lane and not this file's assertions, because what a screen draws belongs
# beside the screen.
#
# The two bundles are imported by the browser over this dev server, so the
# import map is exercised as it is in a build: one Svelte runtime and one
# component library for the app and both modules.
# The app reads the Fractal module through this client's dist folder.
step "the Fractal module's client builds" sh -c \
	"cd '$FRACTAL_CLIENT_DIR' && pnpm install --frozen-lockfile && pnpm run build"

step "the app installs its dependencies" sh -c \
	"cd '$APP_DIR' && pnpm install --frozen-lockfile"

step "the app draws both modules and refuses the fixture" sh -c \
	"cd '$APP_DIR' && PCL_CORE_URL='$BASE_URL' PCL_DEMO_USER=demo \
	   PCL_DEMO_PASSWORD='$PASSWORD' pnpm run test:modules"

kill "$DEMO_PID" 2> /dev/null
wait "$DEMO_PID" 2> /dev/null

echo "OK    both proof extensions and the refused fixture passed $STEPS steps"
echo
echo "      installed into  $MODULES_DIR"
echo "      declared in     $DEMO_DIR/resources.json"
echo "      the config      $OUT_CONFIG"
echo
echo "      run it:  make demo DEMO_ARGS=\"-config $OUT_CONFIG -dir $DEMO_DIR\""

}
main "$@"
