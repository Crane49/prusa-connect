#!/bin/sh
# The parts module, built, installed and driven end to end.
#
#   extensions/parts  a module with a Go half and a browser half, in its own
#                     Go module, adding a sidebar item and a page of its own
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
#   5. installs it with `prusa-connect module add`, into a scratch
#      modules directory and a scratch overlay. The approve grants the module
#      a database of its own, a SQLite file in its data directory
#   6. starts the demo on that configuration, signs in, and over the API makes
#      a folder and a part, uploads a file into the storage, and records it as
#      the part's first version. The configuration declares no record backend,
#      as a server set up by the first-run wizard declares none
#   7. stops the demo and starts it again, which stops and starts the module
#      with it, and reads the same part back with its version. That is the
#      whole claim of the database: nothing is kept in the memory of the
#      process
#   8. drives the page in a real Chromium against that same demo
#
# The release is laid out by extensions/scripts/pack-release.sh, the same
# script the server image packs /opt/modules/parts with, so the image carries
# the layout this gate installs.
#
# No digest is written by hand anywhere here. `module add` records what it
# installed, which is the point of the command: a declaration written by hand
# goes stale the first time somebody rebuilds.
#
# The whole run is one function, called on the last line, so the shell parses
# this file to its end before executing any of it. sh reads a script while it
# runs, and an edit landing mid-run moves the read offset under the reader.
main() {

ROOT=$(cd "$(dirname "$0")/../../.." && pwd)
cd "$ROOT" || exit 1

MODULE_DIR=extensions/parts
CLIENT_DIR=$MODULE_DIR/client
UI_DIR=$MODULE_DIR/ui

# The component library the bundle is built against, and the client SDK the
# typed client links to. A fresh clone has no dist under either.
LIBRARY_DIR=packages/svelte
CLIENT_SDK_DIR=packages/typescript

# Everything this run writes goes under bin/, which git ignores, and that is
# where a build artefact belongs. One run at a time owns it: a second run that
# starts while the first is going stops at the lock below.
WORK_ROOT=bin/parts
LOCK=$WORK_ROOT.lock
RELEASE=$WORK_ROOT/release
MODULES_DIR=$WORK_ROOT/modules
DEMO_DIR=$WORK_ROOT/demo
OUT_CONFIG=${PARTS_CONFIG:-$WORK_ROOT/config.yaml}

# The module path the module is installed from, and the namespace the host
# derives from it. The author never writes the namespace: it is the reversed
# module path, and every address of the module is built from it.
PARTS_MODULE=github.com/prusa3d/parts
PARTS_NAMESPACE=com.github.aux.prusa3d.parts
MOUNT=/api/v1/modules/$PARTS_NAMESPACE

# The demo binds the first free port from PARTS_PORT up, 18098 by default. It
# is not 8080, so a run does not collide with a server somebody already has up.
# DEMO_ADDR and BASE_URL are set once a port is chosen, before the demo starts.
FIRST_PORT=${PARTS_PORT:-18098}

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
PARTS_VERSION=$(sed -n 's/^version:[ 	]*//p' "$MODULE_DIR/module.yaml" | head -1)
if [ -z "$PARTS_VERSION" ]; then
	echo "FAIL  $MODULE_DIR/module.yaml declares no version"
	exit 1
fi

echo " 0. the parts module declares version $PARTS_VERSION"

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
	-namespace "$PARTS_NAMESPACE" -name parts-client -out "$CLIENT_DIR"

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
BINARY="parts_$(go env GOOS)_$(go env GOARCH)$(go env GOEXE)"

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
# It declares no record backend, which is what the first-run wizard writes.
# The module keeps its catalogue in a database of its own, which the install
# below grants, so it needs none. The storage is the demo's own `local`, which
# is the default of the module's `storage` setting. The demo owns that block
# and writes it itself, so it is not declared here.
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

# -approve grants what the manifest asks for and switches the instance on,
# which is what an operator does after reading it. This module asks for no
# permission at all, and for a database of its own. With no -database named,
# the database is a SQLite file in the module's data directory.
step "the parts module installs" "$CORE" module add "$PARTS_MODULE@$PARTS_VERSION" \
	-config "$OUT_CONFIG" -from "$RELEASE" -approve

step "the listing on disk holds one line for it" sh -c \
	"'$CORE' module list -config '$OUT_CONFIG' | tee '$WORK/list.txt' && \
	 grep -q '$PARTS_NAMESPACE' '$WORK/list.txt'"

step "the install granted a SQLite database" grep -q '"engine": *"sqlite"' "$DEMO_DIR/resources.json"

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
		if curl -fsS -b "$WORK/cookies" "$BASE_URL$MOUNT/overview" > "$WORK/overview.json" 2>/dev/null; then
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
step "the parts routes answer" serving

# The module reported the schema its database holds, and the listing shows it.
step "the listing shows the schema of the module's database" sh -c \
	"curl -fsS -b '$WORK/cookies' '$BASE_URL/api/v1/resources' > '$WORK/resources.json' && \
	 grep -q '\"engine\":\"sqlite\"' '$WORK/resources.json' && \
	 grep -q '\"schema\":{' '$WORK/resources.json'"

step "the listing publishes the bundle under the same namespace" sh -c \
	"curl -fsS -b '$WORK/cookies' '$BASE_URL/api/v1/resources' > '$WORK/resources.json' && \
	 grep -q '\"entry_url\":\"$MOUNT/ui/ui/index.js\"' '$WORK/resources.json'"

step "the bundle answers" sh -c \
	"curl -fsS -o /dev/null -b '$WORK/cookies' '$BASE_URL$MOUNT/ui/ui/index.js'"

# ── A folder, a part, a file and a version ──────────────────────────
add_part() {
	curl -fsS -b "$WORK/cookies" -X POST "$BASE_URL$MOUNT/folders" \
		-H 'content-type: application/json' \
		-d '{"name":"Garage","description":"Hooks and shelves"}' > "$WORK/folder.json" || return 1

	FOLDER_ID=$(sed -n 's/.*"folder":{[^}]*"id":\([0-9][0-9]*\).*/\1/p' "$WORK/folder.json" | head -1)
	[ -n "$FOLDER_ID" ] || return 1

	curl -fsS -b "$WORK/cookies" -X POST "$BASE_URL$MOUNT/parts" \
		-H 'content-type: application/json' \
		-d "{\"name\":\"Bike hook\",\"folder\":$FOLDER_ID,\"summary\":\"Holds a bicycle by its front wheel\",\"tags\":[\"bike\",\"PETG\"]}" \
		> "$WORK/part.json" || return 1

	PART_ID=$(sed -n 's/.*"part":{"v":1,"id":\([0-9][0-9]*\).*/\1/p' "$WORK/part.json" | head -1)
	[ -n "$PART_ID" ] || return 1

	# The tags came back the way the module keeps them: lower case, sorted.
	grep -q '"tags":\["bike","petg"\]' "$WORK/part.json"
}

step "a folder and a part are added over the API" add_part

echo "    the part was given id $PART_ID"

# The page uploads a file through the core's storage route, as the person,
# and then asks the module to record it. The same two calls, by hand.
add_version() {
	printf 'G28\nG1 X10\n' > "$WORK/hook.gcode"
	FILE_PATH="parts/$PART_ID/check/hook.gcode"

	# The demo answers before its storage is ready, and until then the core
	# answers core.internal/RESOURCE_NOT_READY, a 503 it documents as
	# retryable. The upload is a PUT to a fixed path, so asking again is safe.
	tries=0
	while :; do
		tries=$((tries + 1))
		status=$(curl -sS -o "$WORK/upload.json" -w '%{http_code}' -b "$WORK/cookies" \
			-X PUT "$BASE_URL/api/v1/storages/local/content?path=$FILE_PATH" \
			-H 'content-type: application/octet-stream' --data-binary @"$WORK/hook.gcode")
		[ "$status" = 200 ] || [ "$status" = 201 ] && break
		if [ "$status" != 503 ] || [ "$tries" -ge 10 ]; then
			echo "the upload answered $status after $tries attempt(s):" >&2
			cat "$WORK/upload.json" >&2
			tail -n 40 "$WORK/demo.log" >&2
			return 1
		fi
		sleep 1
	done

	# The module asks the storage, as the person, about the file it records.
	# On a loaded runner that call can miss its deadline, and the module then
	# answers parts/STORE_FAILED, a 503 it documents as retryable: the same
	# request works a moment later. So a 503 is asked again a few times, and
	# any other answer, or a 503 that stays, fails the step with the body and
	# the end of the demo's log, so a real fault is read rather than retried.
	tries=0
	while :; do
		tries=$((tries + 1))
		status=$(curl -sS -o "$WORK/version.json" -w '%{http_code}' -b "$WORK/cookies" \
			-X POST "$BASE_URL$MOUNT/parts/$PART_ID/versions" \
			-H 'content-type: application/json' \
			-d "{\"note\":\"first print\",\"files\":[{\"path\":\"$FILE_PATH\"}]}")
		[ "$status" = 201 ] || [ "$status" = 200 ] && break
		if [ "$status" != 503 ] || [ "$tries" -ge 5 ]; then
			echo "recording the version answered $status after $tries attempt(s):" >&2
			cat "$WORK/version.json" >&2
			tail -n 40 "$WORK/demo.log" >&2
			return 1
		fi
		sleep 1
	done

	# The size is what the storage reported, not what the request said.
	SIZE=$(wc -c < "$WORK/hook.gcode" | tr -d ' ')
	grep -q "\"size\":$SIZE" "$WORK/version.json" && grep -q '"number":1' "$WORK/version.json"
}

step "a file is uploaded and recorded as the first version" add_version

# A path outside the part's own folder is refused before the storage is asked.
outside_refused() {
	status=$(curl -sS -o "$WORK/outside.json" -w '%{http_code}' -b "$WORK/cookies" \
		-X POST "$BASE_URL$MOUNT/parts/$PART_ID/versions" -H 'content-type: application/json' \
		-d '{"files":[{"path":"elsewhere/up/x.gcode"}]}')
	[ "$status" = 400 ] && grep -q '"code":"parts/INVALID"' "$WORK/outside.json"
}

step "a file outside the part's folder is refused" outside_refused

# ── The page, in a real Chromium ─────────────────────────────────────
# Everything above proves the core serves the module. This proves a person sees
# it: the module's own bundle, in a browser, in front of the demo that is still
# running. It opens the part, writes a comment and reads the files.
step "the page draws the part, its files and its comments in a browser" sh -c \
	"cd '$UI_DIR' && PCL_CORE_URL='$BASE_URL' PCL_DEMO_USER=demo \
	   PCL_DEMO_PASSWORD='$PASSWORD' PCL_PART_ID='$PART_ID' pnpm run test:live"

# The server stops, which stops the module with it, and starts again. Nothing
# of the catalogue above survives in the memory of either process.
stop_demo
start_demo
step "the demo serves again after a restart" ready

# The demo prints a new password on every start, and a new password ends every
# session the account held. So the session from before the restart is gone,
# and the check signs in again with the password this start printed.
resume_session() {
	read_password || return 1
	sign_in
}

step "the session is open again" resume_session
step "the parts routes answer again" serving

found_part() {
	curl -fsS -b "$WORK/cookies" "$BASE_URL$MOUNT/parts/$PART_ID" > "$WORK/found.json" || return 1

	grep -q '"name":"Bike hook"' "$WORK/found.json" &&
		grep -q '"note":"first print"' "$WORK/found.json" &&
		grep -q '"body":"written from the page"' "$WORK/found.json"
}

step "the same part, its version and the page's comment are there after the restart" found_part

# The id counter is in the database too, so the part added after the restart
# does not take the id of the part added before it.
next_id() {
	curl -fsS -b "$WORK/cookies" -X POST "$BASE_URL$MOUNT/parts" \
		-H 'content-type: application/json' -d '{"name":"Shelf bracket"}' > "$WORK/second.json" || return 1

	SECOND_ID=$(sed -n 's/.*"part":{"v":1,"id":\([0-9][0-9]*\).*/\1/p' "$WORK/second.json" | head -1)
	[ -n "$SECOND_ID" ] && [ "$SECOND_ID" != "$PART_ID" ]
}

step "a part added after the restart gets a new id" next_id

stop_demo

echo "OK    the parts module passed $STEPS steps"
echo
echo "      installed into  $MODULES_DIR"
echo "      declared in     $DEMO_DIR/resources.json"
echo "      the config      $OUT_CONFIG"
echo
echo "      run it:  make demo DEMO_ARGS=\"-config $OUT_CONFIG -dir $DEMO_DIR\""

}
main "$@"
