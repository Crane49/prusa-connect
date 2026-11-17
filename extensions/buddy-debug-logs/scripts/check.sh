#!/bin/sh
# The Prusa printer logs module, built, installed and driven end to end.
#
#   extensions/buddy-debug-logs  a module with a Go half and a browser half, in
#                            its own Go module, that listens on two UDP ports
#
# What it does, in order:
#
#   1. builds, vets and tests the Go half inside its own folder, which is what
#      somebody who cloned the module alone does
#   2. holds the typed client to the module's OpenAPI fragment
#   3. lints and type-checks the browser half and runs its tests in Chromium
#   4. lays the module out as a release: the manifest at the root, this
#      platform's binary beside it, the built bundle under ui/, and a
#      checksums.txt over all of them
#   5. installs it with `prusa-connect module add`, approving its ports
#      apart and moving them to free ones, into a scratch modules directory
#   6. starts the demo on that configuration, signs in, and reads the status:
#      both ports granted and listening
#   7. sends one syslog line over UDP, in the firmware's own format, and reads
#      it back from the module's routes
#
# The release is laid out by extensions/scripts/pack-release.sh, the same
# script the server image packs its modules with.
#
# The whole run is one function, called on the last line, so the shell parses
# this file to its end before executing any of it.
main() {

ROOT=$(cd "$(dirname "$0")/../../.." && pwd)
cd "$ROOT" || exit 1

MODULE_DIR=extensions/buddy-debug-logs
CLIENT_DIR=$MODULE_DIR/client
UI_DIR=$MODULE_DIR/ui

# The component library the bundle is built against, and the client SDK the
# typed client links to. A fresh clone has no dist under either.
LIBRARY_DIR=packages/svelte
CLIENT_SDK_DIR=packages/typescript

# Everything this run writes goes under bin/, which git ignores, and that is
# where a build artefact belongs. One run at a time owns it: a second run that
# starts while the first is going stops at the lock below.
WORK_ROOT=bin/buddy-debug-logs
LOCK=$WORK_ROOT.lock
RELEASE=$WORK_ROOT/release
MODULES_DIR=$WORK_ROOT/modules
DEMO_DIR=$WORK_ROOT/demo
OUT_CONFIG=${BUDDY_DEBUG_LOGS_CONFIG:-$WORK_ROOT/config.yaml}

# The module path the module is installed from, and the namespace the host
# derives from it. The author never writes the namespace: it is the reversed
# module path, and every address of the module is built from it.
BUDDY_DEBUG_LOGS_MODULE=github.com/prusa3d/buddy-debug-logs
BUDDY_DEBUG_LOGS_NAMESPACE=com.github.aux.prusa3d.buddy-debug-logs
MOUNT=/api/v1/modules/$BUDDY_DEBUG_LOGS_NAMESPACE

# The demo binds the first free port from BUDDY_DEBUG_LOGS_PORT up, 18198 by default. It
# is not 8080, so a run does not collide with a server somebody already has up.
# DEMO_ADDR and BASE_URL are set once a port is chosen, before the demo starts.
FIRST_PORT=${BUDDY_DEBUG_LOGS_PORT:-18198}

# The UDP ports the module is granted. Not the firmware's 13514 and 8514, so
# a run does not collide with a server somebody already has receiving.
SYSLOG_PORT=${BUDDY_DEBUG_LOGS_SYSLOG_PORT:-23514}
METRICS_PORT=${BUDDY_DEBUG_LOGS_METRICS_PORT:-28514}

CHECKSUMS_FILE=checksums.txt

# CI=true keeps pnpm from asking anything, and keeps its output readable.
CI=true
export CI

for tool in go pnpm sha256sum curl bash; do
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
BUDDY_DEBUG_LOGS_VERSION=$(sed -n 's/^version:[ 	]*//p' "$MODULE_DIR/module.yaml" | head -1)
if [ -z "$BUDDY_DEBUG_LOGS_VERSION" ]; then
	echo "FAIL  $MODULE_DIR/module.yaml declares no version"
	exit 1
fi

echo " 0. the Prusa printer logs module declares version $BUDDY_DEBUG_LOGS_VERSION"

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
	-namespace "$BUDDY_DEBUG_LOGS_NAMESPACE" -name buddy-debug-logs-client -out "$CLIENT_DIR"

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
BINARY="buddy-debug-logs_$(go env GOOS)_$(go env GOARCH)$(go env GOEXE)"

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
# installed. The module keeps its lines in its own data directory, which the
# host makes beside the modules directory, so it needs no record backend.
#
# Every path here is relative to the directory the server is started from,
# which is the repository root.
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

step "the core and the demo build" sh -c \
	"go build -o '$CORE' ./cmd/prusa-connect && go build -o '$DEMO' ./cmd/demo"

# -approve grants the directory and the read of the fleet; -approve-listen
# grants the two ports, which an operator approves apart (ADR-20261019-3ns), and
# -listen moves them to the ports this run chose.
step "the Prusa printer logs module installs with its ports" "$CORE" module add "$BUDDY_DEBUG_LOGS_MODULE@$BUDDY_DEBUG_LOGS_VERSION" \
	-config "$OUT_CONFIG" -from "$RELEASE" -approve -approve-listen \
	-listen "syslog=$SYSLOG_PORT,metrics=$METRICS_PORT"

step "the listing on disk holds one line for it" sh -c \
	"'$CORE' module list -config '$OUT_CONFIG' | tee '$WORK/list.txt' && \
	 grep -q '$BUDDY_DEBUG_LOGS_NAMESPACE' '$WORK/list.txt'"

step "the declaration grants the two ports it was given" sh -c \
	"grep -q 'udp/$SYSLOG_PORT' '$DEMO_DIR/resources.json' && grep -q 'udp/$METRICS_PORT' '$DEMO_DIR/resources.json'"

# ── The demo ─────────────────────────────────────────────────────────
port_free() {
	curl -sS -o /dev/null --max-time 2 "http://127.0.0.1:$1/" > /dev/null 2>&1
	[ "$?" -eq 7 ]
}

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

start_demo() {
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

ready() {
	waited=0
	while [ "$waited" -lt 120 ]; do
		if ! kill -0 "$DEMO_PID" 2> /dev/null; then
			echo "the demo ended before it served" >&2
			cat "$WORK/demo.log" >&2
			return 1
		fi

		if curl -fsS "$BASE_URL/api/v1/health" > /dev/null 2>&1; then
			return 0
		fi

		waited=$((waited + 1))
		sleep 1
	done

	cat "$WORK/demo.log" >&2
	return 1
}

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
	return 1
}

# listening waits until the module answers its status with both ports open.
# The process takes a moment to start and to bind them.
listening() {
	waited=0
	while [ "$waited" -lt 60 ]; do
		if curl -fsS -b "$WORK/cookies" "$BASE_URL$MOUNT/status" > "$WORK/status.json" 2> /dev/null &&
			[ "$(grep -o '"listening":true' "$WORK/status.json" | wc -l | tr -d ' ')" = 2 ]; then
			return 0
		fi

		waited=$((waited + 1))
		sleep 1
	done

	cat "$WORK/status.json" >&2
	tail -n 40 "$WORK/demo.log" >&2
	return 1
}

start_demo
step "the demo serves" ready
step "the demo prints an account to sign in with" read_password
step "the demo account signs in" sign_in
step "the module listens on both granted ports" listening

step "the listing publishes the bundle under the same namespace" sh -c \
	"curl -fsS -b '$WORK/cookies' '$BASE_URL/api/v1/resources' > '$WORK/resources.json' && \
	 grep -q '\"entry_url\":\"$MOUNT/ui/ui/index.js\"' '$WORK/resources.json'"

step "the bundle answers" sh -c \
	"curl -fsS -o /dev/null -b '$WORK/cookies' '$BASE_URL$MOUNT/ui/ui/index.js'"

# ── A line over UDP ──────────────────────────────────────────────────
# The format is the firmware's own. bash writes to
# /dev/udp, which sh has no spelling for.
LINE='<11>1 - aa:bb:cc:dd:ee:ff buddy Connect - [metrics@64164 v="5" msg="1" tm="1000"] check line from the gate'

# A line from a MAC matched to no printer goes only to a person who sees every
# printer the module knows. While the module knows no printer, it goes to
# nobody. The demo's printer may not be one the module reads as itself, so
# the gate takes either answer: the line read back as an error, or no line.
# Either way the status must show the line kept on the syslog port.
SYSLOG_KEPT='"stream":"syslog","listener":\{[^}]*\},"counts":\{"received":[1-9][0-9]*,"unreadable":0,"records":[1-9]'

line_arrives() {
	bash -c "printf '%s' '$LINE' > /dev/udp/127.0.0.1/$SYSLOG_PORT" || return 1

	waited=0
	while [ "$waited" -lt 20 ]; do
		if curl -fsS -b "$WORK/cookies" "$BASE_URL$MOUNT/status" > "$WORK/status.json" 2> /dev/null &&
			grep -Eq "$SYSLOG_KEPT" "$WORK/status.json" &&
			curl -fsS -b "$WORK/cookies" "$BASE_URL$MOUNT/logs?mac=aa:bb:cc:dd:ee:ff" > "$WORK/logs.json" 2> /dev/null; then
			if grep -q 'check line from the gate' "$WORK/logs.json" &&
				grep -q '"severity":"error"' "$WORK/logs.json"; then
				return 0
			fi

			if grep -q '^{"lines":\[\]' "$WORK/logs.json"; then
				return 0
			fi
		fi

		waited=$((waited + 1))
		sleep 1
	done

	cat "$WORK/status.json" "$WORK/logs.json" >&2
	return 1
}

step "a syslog line sent over UDP is kept, and read back as an error or hidden" line_arrives

stop_demo

echo "OK    the Prusa printer logs module passed $STEPS steps"
echo
echo "      the release     $RELEASE"
echo "      installed into  $MODULES_DIR"
echo "      the config      $OUT_CONFIG"

}
main "$@"
