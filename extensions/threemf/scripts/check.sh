#!/bin/sh
# The 3MF module, built, installed and driven end to end.
#
#   extensions/threemf  a module with a Go half and a browser half, in its own
#                       Go module. It reads .3mf files for the core and shows
#                       a project on the page of its file
#
# What it does, in order:
#
#   1. builds, vets and tests the Go half inside its own folder, which is what
#      somebody who cloned the module alone does
#   2. holds the typed client to the module's OpenAPI fragment
#   3. lints and type-checks the browser half and runs its tests in Chromium
#   4. lays the module out as a release with extensions/scripts/pack-release.sh
#   5. installs it with `prusa-connect module add` into a scratch
#      modules directory and a scratch overlay
#   6. starts the demo, signs in, uploads a 3MF project into its storage and
#      reads it through the module's routes and through the core's stat
#      route, which asks the module about the file
#
# The whole run is one function, called on the last line, so the shell parses
# this file to its end before executing any of it.

main() {

ROOT=$(cd "$(dirname "$0")/../../.." && pwd)
cd "$ROOT" || exit 1

MODULE_DIR=extensions/threemf
CLIENT_DIR=$MODULE_DIR/client
UI_DIR=$MODULE_DIR/ui

# The component library the bundle is built against, and the client SDK the
# typed client links to. A fresh clone has no dist under either.
LIBRARY_DIR=packages/svelte
CLIENT_SDK_DIR=packages/typescript

# Everything this run writes goes under bin/, which git ignores, and that is
# where a build artefact belongs. One run at a time owns it: a second run that
# starts while the first is going stops at the lock below.
WORK_ROOT=bin/threemf
LOCK=$WORK_ROOT.lock
RELEASE=$WORK_ROOT/release
MODULES_DIR=$WORK_ROOT/modules
DEMO_DIR=$WORK_ROOT/demo
OUT_CONFIG=${THREEMF_CONFIG:-$WORK_ROOT/config.yaml}

# The module path the module is installed from, and the namespace the host
# derives from it. The author never writes the namespace: it is the reversed
# module path, and every address of the module is built from it.
THREEMF_MODULE=github.com/prusa3d/threemf
THREEMF_NAMESPACE=com.github.aux.prusa3d.threemf
MOUNT=/api/v1/modules/$THREEMF_NAMESPACE

# The demo binds the first free port from THREEMF_PORT up, 18098 by default. It
# is not 8080, so a run does not collide with a server somebody already has up.
# DEMO_ADDR and BASE_URL are set once a port is chosen, before the demo starts.
FIRST_PORT=${THREEMF_PORT:-18148}

CHECKSUMS_FILE=checksums.txt

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
DEMO_PID=

# cleanup runs on every way out: success, a failed step, Ctrl-C and a kill. It
# stops the demo this run started, removes the scratch files and gives up the
# lock. A signal exits with 128 plus its number, so a caller sees the run as
# interrupted and not as passed.
cleanup() {
	if [ -n "$DEMO_PID" ]; then
		kill "$DEMO_PID" 2> /dev/null
		wait "$DEMO_PID" 2> /dev/null
	fi
	rm -rf "$WORK"
	if [ "$(cat "$LOCK/pid" 2> /dev/null)" = "$$" ]; then
		rm -rf "$LOCK"
	fi
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

# take_lock makes $LOCK, which only one process can do. A lock whose run has
# ended is stale: that run was killed before its cleanup. Its demo may still be
# up, so it is stopped here, before this run deletes the files under it.
take_lock() {
	mkdir -p "$(dirname "$LOCK")"
	if mkdir "$LOCK" 2> /dev/null; then
		echo "$$" > "$LOCK/pid"
		return 0
	fi

	owner=$(cat "$LOCK/pid" 2> /dev/null)
	if [ -n "$owner" ] && kill -0 "$owner" 2> /dev/null; then
		echo "FAIL  another run of this check (pid $owner) is using $WORK_ROOT."
		echo "      Wait for it to end, or stop it."
		exit 1
	fi

	stale=$(cat "$LOCK/demo.pid" 2> /dev/null)
	if [ -n "$stale" ] && ps -p "$stale" -o command= 2> /dev/null | grep -q demo-server; then
		echo "    stopping the demo (pid $stale) that an interrupted run left running"
		kill "$stale" 2> /dev/null
		sleep 1
	fi

	rm -rf "$LOCK"
	if ! mkdir "$LOCK" 2> /dev/null; then
		echo "FAIL  another run of this check took $LOCK first."
		exit 1
	fi
	echo "$$" > "$LOCK/pid"
}

take_lock

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
mkdir -p "$MODULES_DIR" "$DEMO_DIR" "$RELEASE"

# The version the manifest declares. `module add` refuses a release whose
# manifest says another one, so reading it here is the only way the two agree
# without being kept in step by hand.
THREEMF_VERSION=$(sed -n 's/^version:[ 	]*//p' "$MODULE_DIR/module.yaml" | head -1)
if [ -z "$THREEMF_VERSION" ]; then
	echo "FAIL  $MODULE_DIR/module.yaml declares no version"
	exit 1
fi

echo " 0. the threemf module declares version $THREEMF_VERSION"

# ── The Go half, inside its own folder ────────────────────────────────
# The module carries its own go.mod, so the core's `go build ./...` walks past
# it. These three are the build a person who cloned the module alone runs.
step "the Go half builds and vets in its own folder" sh -c \
	"cd '$MODULE_DIR' && go build ./... && go vet ./..."
step "the Go half's rules, routes, manifest and changelog hold" sh -c \
	"cd '$MODULE_DIR' && go test -count=1 ./..."

# ── The two packages the browser half links to ───────────────────────
if [ ! -f "$LIBRARY_DIR/dist/build/module.js" ]; then
	step "the component library builds" sh -c \
		"cd '$LIBRARY_DIR' && pnpm install --frozen-lockfile && pnpm run build"
fi

if [ ! -f "$CLIENT_SDK_DIR/dist/index.js" ]; then
	step "the client SDK builds" sh -c \
		"cd '$CLIENT_SDK_DIR' && pnpm install --frozen-lockfile && pnpm run build"
fi

# ── The typed client ─────────────────────────────────────────────────
# It is generated from the same fragment the module embeds, so -check fails
# when the fragment moved and nobody regenerated.
step "the typed client is what the fragment generates" \
	go run ./cmd/generate-client-sdk -module -check \
	-document "$MODULE_DIR/openapi.json" \
	-namespace "$THREEMF_NAMESPACE" -name threemf-client -out "$CLIENT_DIR"

step "the typed client installs" sh -c "cd '$CLIENT_DIR' && pnpm install --frozen-lockfile"
step "the client types match the fragment" sh -c \
	"cd '$CLIENT_DIR' && pnpm run generate:types && git diff --exit-code src/generated/"
step "the typed client builds" sh -c "cd '$CLIENT_DIR' && pnpm run build"

# ── The browser half ─────────────────────────────────────────────────
step "the browser half installs" sh -c "cd '$UI_DIR' && pnpm install --frozen-lockfile"

# The browser tests run in the Chromium of the browser half's own playwright,
# so that is the copy installed. On a GitHub runner, which is Linux, the
# libraries Chromium needs are installed with it.
PLAYWRIGHT_FLAGS=
if [ "${GITHUB_ACTIONS:-}" = true ] && [ "$(uname -s)" = Linux ]; then
	PLAYWRIGHT_FLAGS=--with-deps
fi
step "Chromium for the browser tests installs" sh -c \
	"cd '$UI_DIR' && pnpm exec playwright install $PLAYWRIGHT_FLAGS chromium"
step "the browser half lints, type-checks and its tests pass in Chromium" sh -c \
	"cd '$UI_DIR' && pnpm run check"
step "the browser half builds into a bundle" sh -c "cd '$UI_DIR' && pnpm run build"

if [ ! -f "$UI_DIR/dist/module.yaml" ] || [ ! -f "$UI_DIR/dist/ui/index.js" ]; then
	echo "FAIL  $UI_DIR/dist is missing module.yaml or ui/index.js"
	exit 1
fi

# ── The release ──────────────────────────────────────────────────────
# The layout `module add` reads: the manifest at the root, this platform's
# binary beside it, the built bundle under ui/, and a checksums.txt over all of
# them. The file name of the binary is <name>_<GOOS>_<GOARCH>, which is what
# the host looks for in the directory the declaration points at.
BINARY="threemf_$(go env GOOS)_$(go env GOARCH)$(go env GOEXE)"

pack() {
	# The output path is relative to the module folder. An absolute one would
	# be this shell's own spelling of it, and under Git Bash on Windows that
	# is a path the Go toolchain cannot open.
	( cd "$MODULE_DIR" && go build -o "../../$WORK_ROOT/$BINARY" . ) || return 1
	sh extensions/scripts/pack-release.sh "$UI_DIR/dist" "$WORK_ROOT/$BINARY" "$RELEASE"
}

step "the release packs" pack

if [ ! -f "$RELEASE/module.yaml" ] || [ ! -f "$RELEASE/ui/index.js" ] || [ ! -f "$RELEASE/$CHECKSUMS_FILE" ]; then
	echo "FAIL  $RELEASE is missing module.yaml, ui/index.js or checksums.txt"
	exit 1
fi

# ── The operator's file ──────────────────────────────────────────────
# It names where the machine-owned overlay goes and where modules are
# installed. Everything else in a demo run is the demo's own.
#
# The storage is the demo's own `local`. The demo owns that block and writes
# it itself, so it is not declared here.
#
# Every path here is relative to the directory the server is started from,
# which is the repository root. An absolute one written here would be this
# shell's own spelling of it, and on Windows that is a path Go cannot open.
cat > "$OUT_CONFIG" <<- YAML
	core:
	  overlay: $DEMO_DIR/resources.json
	  modules: $MODULES_DIR
YAML

CORE=$WORK_ROOT/prusa-connect$(go env GOEXE)
# The binary is not called demo: $DEMO_DIR is $WORK_ROOT/demo, and on Linux,
# where GOEXE is empty, a binary of that name and the directory would be one
# path.
DEMO=$WORK_ROOT/demo-server$(go env GOEXE)

# Both are stamped with the version `make build` stamps. The module's core
# range is held against it, and an unstamped build reads a version Go made up
# from the commit, which no range names.
STAMP="-X github.com/prusa3d/prusa-connect/internal/kernel/version.stamped=$(sh scripts/build-version.sh)"

step "the core and the demo build" sh -c \
	"go build -ldflags '$STAMP' -o '$CORE' ./cmd/prusa-connect && \
	 go build -ldflags '$STAMP' -o '$DEMO' ./cmd/demo"

# -approve switches the instance on, which is what an operator does after
# reading the manifest. This module asks for no permission and no database.
step "the threemf module installs" "$CORE" module add "$THREEMF_MODULE@$THREEMF_VERSION" \
	-config "$OUT_CONFIG" -from "$RELEASE" -approve

step "the listing on disk holds one line for it" sh -c \
	"'$CORE' module list -config '$OUT_CONFIG' | tee '$WORK/list.txt' && \
	 grep -q '$THREEMF_NAMESPACE' '$WORK/list.txt'"

# ── The demo ─────────────────────────────────────────────────────────
# port_free says whether nothing answers on a port of the loopback address.
# curl exits 7 when it cannot connect, and that is the only answer taken as
# free.
port_free() {
	curl -sS -o /dev/null --max-time 2 "http://127.0.0.1:$1/" > /dev/null 2>&1
	[ "$?" -eq 7 ]
}

# pick_port takes the first free port from FIRST_PORT up. Something already
# listening there would answer the readiness probe in place of this run's demo.
pick_port() {
	port=$FIRST_PORT
	while [ "$port" -lt $((FIRST_PORT + 50)) ]; do
		if port_free "$port"; then
			DEMO_ADDR=127.0.0.1:$port
			BASE_URL=http://$DEMO_ADDR
			return 0
		fi
		port=$((port + 1))
	done

	echo "no free port from $FIRST_PORT to $((FIRST_PORT + 49)) on 127.0.0.1" >&2
	return 1
}

step "a free port for the demo" pick_port
echo "    the demo binds $DEMO_ADDR"
# LOG_FROM is how many lines demo.log held when the latest start began. Every
# start appends to the one log, so what that start printed is what follows.
LOG_FROM=0

# start_demo refuses a port that something took since it was chosen, because
# the readiness probe would then pass against that other process.
start_demo() {
	if ! port_free "${DEMO_ADDR##*:}"; then
		echo "FAIL  something else answers on $DEMO_ADDR, so this run cannot start its demo there."
		exit 1
	fi

	LOG_FROM=$(wc -l < "$WORK/demo.log" 2> /dev/null || echo 0)
	"$DEMO" -config "$OUT_CONFIG" -dir "$DEMO_DIR" -addr "$DEMO_ADDR" \
		-printers 1 -flap 0 -cameras=false >> "$WORK/demo.log" 2>&1 &
	DEMO_PID=$!
	echo "$DEMO_PID" > "$LOCK/demo.pid"
}

stop_demo() {
	[ -n "$DEMO_PID" ] || return 0
	kill "$DEMO_PID" 2> /dev/null
	wait "$DEMO_PID" 2> /dev/null
	DEMO_PID=
	rm -f "$LOCK/demo.pid"
}

# ready polls the liveness path until the core answers, so nothing below races
# the start.
ready() {
	waited=0
	while [ "$waited" -lt 120 ]; do
		# The process is asked first. A demo that failed to bind has ended,
		# and an answer on the port would then come from another process.
		if ! kill -0 "$DEMO_PID" 2> /dev/null; then
			echo "the demo ended before it served" >&2
			cat "$WORK/demo.log" >&2
			return 1
		fi

		if curl -fsS "$BASE_URL/api/v1/health" > /dev/null 2>&1 && kill -0 "$DEMO_PID" 2> /dev/null; then
			return 0
		fi

		waited=$((waited + 1))
		sleep 1
	done

	cat "$WORK/demo.log" >&2
	return 1
}

# The demo prints its account and a new password on every run, so the password
# is read off what the latest start printed rather than written down here. It
# prints that line once the fleet has dialled in, which is after the core is
# answering, so this waits for the line rather than reading the log once.
read_password() {
	waited=0
	while [ "$waited" -lt 60 ]; do
		PASSWORD=$(tail -n "+$((LOG_FROM + 1))" "$WORK/demo.log" | sed -n 's/^ *password  *//p' | head -1)
		if [ -n "$PASSWORD" ]; then
			return 0
		fi

		waited=$((waited + 1))
		sleep 1
	done

	cat "$WORK/demo.log" >&2
	return 1
}

# The demo prints the account before it has finished writing the password it
# printed, so the first sign-in after a start can be refused. This waits for
# the account rather than calling that race a failure.
sign_in() {
	waited=0
	while [ "$waited" -lt 60 ]; do
		rm -f "$WORK/cookies"

		if curl -fsS -c "$WORK/cookies" -X POST "$BASE_URL/api/v1/auth/login" \
			-H 'content-type: application/json' \
			-d "{\"username\":\"demo\",\"password\":\"$PASSWORD\"}" > /dev/null 2>&1; then
			return 0
		fi

		waited=$((waited + 1))
		sleep 1
	done

	echo "the demo account did not sign in with the password the demo printed" >&2
	cat "$WORK/demo.log" >&2

	return 1
}

# serving waits for the module's own routes to answer. The module's process
# takes a moment to answer its handshake and to migrate its database, so this
# waits for the state rather than calling a race a failure.
serving() {
	waited=0
	while [ "$waited" -lt 60 ]; do
		if curl -fsS -b "$WORK/cookies" "$BASE_URL$MOUNT/openapi.json" > "$WORK/overview.json" 2>/dev/null; then
			return 0
		fi

		waited=$((waited + 1))
		sleep 1
	done

	cat "$WORK/overview.json" >&2
	cat "$WORK/demo.log" >&2
	return 1
}

start_demo
step "the demo serves" ready
step "the demo prints an account to sign in with" read_password
step "the demo account signs in" sign_in
step "the module's fragment answers" serving

step "the listing publishes the bundle under the same namespace" sh -c \
	"curl -fsS -b '$WORK/cookies' '$BASE_URL/api/v1/resources' > '$WORK/resources.json' && \
	 grep -q '\"entry_url\":\"$MOUNT/ui/ui/index.js\"' '$WORK/resources.json'"

# A project goes into the demo's storage through the core, as the person.
upload() {
	tries=0
	while :; do
		tries=$((tries + 1))
		status=$(curl -sS -o "$WORK/upload.json" -w '%{http_code}' -b "$WORK/cookies" \
			-X PUT "$BASE_URL/api/v1/storages/local/content?path=check/multibed.3mf" \
			-H 'content-type: application/octet-stream' \
			--data-binary @"$MODULE_DIR/threemf/testdata/prusaslicer-2.9.6-multibed.3mf")
		[ "$status" = 200 ] || [ "$status" = 201 ] && return 0
		if [ "$status" != 503 ] || [ "$tries" -ge 10 ]; then
			echo "the upload answered $status:" >&2
			cat "$WORK/upload.json" >&2
			return 1
		fi
		sleep 1
	done
}

step "a project is uploaded into the storage" upload

FILE_QUERY="storage=local&path=check/multibed.3mf"

step "the project route answers the beds and the objects" sh -c \
	"curl -fsS -b '$WORK/cookies' '$BASE_URL$MOUNT/project?$FILE_QUERY' > '$WORK/project.json' && \
	 grep -q '\"dialect\":\"prusaslicer2\"' '$WORK/project.json' && \
	 grep -q '\"beds\":3' '$WORK/project.json'"

step "the picture and an object's mesh answer" sh -c \
	"curl -fsS -o /dev/null -b '$WORK/cookies' '$BASE_URL$MOUNT/project/thumbnail?$FILE_QUERY&index=0' && \
	 curl -fsS -b '$WORK/cookies' '$BASE_URL$MOUNT/project/mesh?$FILE_QUERY&object=0' | grep -q '\"triangles\"'"

# The core asks the module about the file. The first answer may come after
# the stat waited its part, so the stat is asked until it names the module.
analysed() {
	waited=0
	while [ "$waited" -lt 30 ]; do
		curl -fsS -b "$WORK/cookies" "$BASE_URL/api/v1/storages/local/stat?path=check/multibed.3mf" \
			> "$WORK/stat.json" 2>/dev/null &&
			grep -q "\"provider\":\"$THREEMF_NAMESPACE\"" "$WORK/stat.json" && return 0
		waited=$((waited + 1))
		sleep 1
	done

	cat "$WORK/stat.json" >&2
	return 1
}

step "the core's stat route names the module that read the file" analysed

stop_demo

echo "OK    the threemf module passed $STEPS steps"
echo
echo "      installed into  $MODULES_DIR"
echo "      the config      $OUT_CONFIG"
echo
echo "      run it:  make demo DEMO_ARGS=\"-config $OUT_CONFIG -dir $DEMO_DIR\""

}
main "$@"
