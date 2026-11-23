#!/bin/sh
# The PrusaSlicer 2 Connector module, built, installed and driven end to
# end, the way PrusaSlicer 2 drives a printer with PrusaLink.
#
#   extensions/prusaslicer2-connector  a module with a Go half and a browser half,
#                                   in its own Go module, that answers the
#                                   slicer for each printer of this server
#
# What it does, in order:
#
#   1. builds, vets and tests the Go half inside its own folder, which is what
#      somebody who cloned the module alone does
#   2. lints and type-checks the browser half, runs its tests in Chromium,
#      checks every component has its stories, and builds the Storybook
#   3. lays the module out as a release with extensions/scripts/pack-release.sh
#   4. makes a throwaway CA and a server certificate for 127.0.0.1 with openssl
#   5. runs the demo twice, once over HTTP and once over HTTPS with that
#      certificate. Each run installs the release with
#      `prusa-connect module add` and drives it with scripts/e2e:
#        - the demo account mints an operate key and a read key
#        - the connection test and the storage list, with the key in
#          X-Api-Key, with no key, a key this server never minted, and the
#          address of a printer it does not have
#        - a file over 1 MiB with Print-After-Upload reaches the printer
#        - a file without it lands in the local storage, and the printer gets
#          nothing
#        - the form PrusaSlicer 2.5 and older send starts a print
#        - a key that may only read, a file a printer does not print and a
#          printer that prints are refused
#
# scripts/e2e does the HTTP requests too, because the curl of macOS speaks no
# TLS 1.3 and the demo serves no older version. curl here only asks whether a
# port is free.
#
# The whole run is one function, called on the last line, so the shell parses
# this file to its end before executing any of it.
main() {

ROOT=$(cd "$(dirname "$0")/../../.." && pwd)
cd "$ROOT" || exit 1

MODULE_DIR=extensions/prusaslicer2-connector
UI_DIR=$MODULE_DIR/ui

# The component library the bundle is built against, and the client SDK it
# links to. A fresh clone has no dist under either.
LIBRARY_DIR=packages/svelte
CLIENT_SDK_DIR=packages/typescript

# Everything this run writes goes under bin/, which git ignores, and that is
# where a build artefact belongs. One run at a time owns it: a second run that
# starts while the first is going stops at the lock below.
WORK_ROOT=bin/prusaslicer2-connector
LOCK=$WORK_ROOT.lock
RELEASE=$WORK_ROOT/release
TLS_DIR=$WORK_ROOT/tls

# The module path the module is installed from, and the namespace the host
# derives from it. The author never writes the namespace: it is the reversed
# module path, and every address of the module is built from it.
CONNECTOR_MODULE=github.com/prusa3d/prusaslicer2-connector
CONNECTOR_NAMESPACE=com.github.aux.prusa3d.prusaslicer2-connector

# Each demo binds the first free port from its default up. It is not 8080, so
# a run does not collide with a server somebody already has up.
HTTP_FIRST_PORT=${PRUSASLICER2_CONNECTOR_PORT:-18698}
HTTPS_FIRST_PORT=${PRUSASLICER2_CONNECTOR_TLS_PORT:-18743}

# The demo account's password. The demo is bound to 127.0.0.1 and lives as
# long as this run.
ADMIN_PASSWORD=prusaslicer2-connector-gate

CHECKSUMS_FILE=checksums.txt

# CI=true keeps pnpm from asking anything, and keeps its output readable.
CI=true
export CI

for tool in go pnpm sha256sum curl bash openssl; do
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
	if [ -f "$WORK/demo.log" ]; then
		echo "      the last lines the demo wrote:"
		tail -n 40 "$WORK/demo.log"
	fi
	exit 1
}

rm -rf "$WORK_ROOT"
mkdir -p "$RELEASE" "$TLS_DIR"

# The version the manifest declares. `module add` refuses a release whose
# manifest says another one, so reading it here is the only way the two agree
# without being kept in step by hand.
CONNECTOR_VERSION=$(sed -n 's/^version:[ 	]*//p' "$MODULE_DIR/module.yaml" | head -1)
if [ -z "$CONNECTOR_VERSION" ]; then
	echo "FAIL  $MODULE_DIR/module.yaml declares no version"
	exit 1
fi

echo " 0. the PrusaSlicer 2 Connector module declares version $CONNECTOR_VERSION"

# ── The Go half, inside its own folder ────────────────────────────────
# The module carries its own go.mod, so the core's `go build ./...` walks past
# it. These three are the build a person who cloned the module alone runs.
step "the Go half builds and vets in its own folder" sh -c \
	"cd '$MODULE_DIR' && go build ./... && go vet ./..."
step "the Go half's version, storage list, uploads and routes hold" sh -c \
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
step "the browser half's Storybook builds" sh -c "cd '$UI_DIR' && pnpm run build-storybook"
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
BINARY="prusaslicer2-connector_$(go env GOOS)_$(go env GOARCH)$(go env GOEXE)"
DRIVER=$WORK_ROOT/e2e$(go env GOEXE)

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

CORE=$WORK_ROOT/prusa-connect$(go env GOEXE)
# The binary is not called demo: each run's demo directory is called demo, and
# on Linux, where GOEXE is empty, a binary of that name and the directory
# would be one path.
DEMO=$WORK_ROOT/demo-server$(go env GOEXE)

# Both are stamped with the version `make build` stamps. The module's core
# range is held against it, and an unstamped build reads a version Go made up
# from the commit, which no range names.
STAMP="-X github.com/prusa3d/prusa-connect/internal/kernel/version.stamped=$(sh scripts/build-version.sh)"

step "the core and the demo build" sh -c \
	"go build -ldflags '$STAMP' -o '$CORE' ./cmd/prusa-connect && \
	 go build -ldflags '$STAMP' -o '$DEMO' ./cmd/demo"

step "the slicer's stand-in builds" sh -c \
	"cd '$MODULE_DIR' && go build -o '../../$DRIVER' ./scripts/e2e"

# ── The certificate ──────────────────────────────────────────────────
# A CA made for this run alone, and a server certificate it signs for the
# address the demo binds. The demo serves the chain and its printers trust
# it; scripts/e2e trusts the CA alone, as a person who installed it would.
certificates() {
	cat > "$TLS_DIR/ca.cnf" <<- 'CNF'
		[req]
		distinguished_name = dn
		prompt = no
		x509_extensions = ca
		[dn]
		CN = prusaslicer2-connector gate CA
		[ca]
		basicConstraints = critical, CA:TRUE
		keyUsage = critical, keyCertSign, cRLSign
		subjectKeyIdentifier = hash
	CNF

	cat > "$TLS_DIR/server.cnf" <<- 'CNF'
		[req]
		distinguished_name = dn
		prompt = no
		[dn]
		CN = 127.0.0.1
		[server]
		basicConstraints = critical, CA:FALSE
		keyUsage = critical, digitalSignature, keyEncipherment
		extendedKeyUsage = serverAuth
		subjectAltName = IP:127.0.0.1, DNS:localhost
		authorityKeyIdentifier = keyid
	CNF

	openssl req -x509 -new -nodes -newkey rsa:2048 -days 2 -config "$TLS_DIR/ca.cnf" \
		-keyout "$TLS_DIR/ca.key" -out "$TLS_DIR/ca.crt" &&
		openssl req -new -nodes -newkey rsa:2048 -config "$TLS_DIR/server.cnf" \
			-keyout "$TLS_DIR/server.key" -out "$TLS_DIR/server.csr" &&
		openssl x509 -req -days 2 -in "$TLS_DIR/server.csr" -CA "$TLS_DIR/ca.crt" -CAkey "$TLS_DIR/ca.key" \
			-CAcreateserial -extfile "$TLS_DIR/server.cnf" -extensions server -out "$TLS_DIR/server.crt" &&
		cat "$TLS_DIR/server.crt" "$TLS_DIR/ca.crt" > "$TLS_DIR/chain.crt" &&
		openssl verify -CAfile "$TLS_DIR/ca.crt" "$TLS_DIR/server.crt"
}

step "a throwaway CA signs a certificate for 127.0.0.1" certificates

# ── The two demos ────────────────────────────────────────────────────
port_free() {
	curl -sS -o /dev/null --max-time 2 "http://127.0.0.1:$1/" > /dev/null 2>&1
	[ "$?" -eq 7 ]
}

# pick_port sets FREE to the first free port from $1 up.
pick_port() {
	port=$1
	while [ "$port" -lt $(($1 + 50)) ]; do
		if port_free "$port"; then
			FREE=$port
			return 0
		fi
		port=$((port + 1))
	done

	echo "no free port from $1 to $(($1 + 49)) on 127.0.0.1" >&2
	return 1
}

stop_demo() {
	[ -n "$DEMO_PID" ] || return 0
	kill "$DEMO_PID" 2> /dev/null
	wait "$DEMO_PID" 2> /dev/null
	DEMO_PID=
	rm -f "$LOCK/demo.pid" "$WORK/demo.log"
}

# drive installs the release into a configuration of its own, starts the demo
# on it, and runs every check of scripts/e2e. $1 is http or https.
drive() {
	MODE=$1
	MODE_DIR=$WORK_ROOT/$MODE
	MODULES_DIR=$MODE_DIR/modules
	DEMO_DIR=$MODE_DIR/demo
	CONFIG=$MODE_DIR/config.yaml
	STATE=$MODE_DIR/state.json
	mkdir -p "$MODULES_DIR" "$DEMO_DIR"

	if [ "$MODE" = https ]; then
		FIRST_PORT=$HTTPS_FIRST_PORT
		SECURED="-ca $TLS_DIR/ca.crt"
		DEMO_TLS="-tls-cert $TLS_DIR/chain.crt -tls-key $TLS_DIR/server.key"
	else
		FIRST_PORT=$HTTP_FIRST_PORT
		SECURED= DEMO_TLS=
	fi

	step "[$MODE] a free port for the demo" pick_port "$FIRST_PORT"
	DEMO_PORT=$FREE
	BASE_URL=$MODE://127.0.0.1:$DEMO_PORT
	echo "    the demo binds 127.0.0.1:$DEMO_PORT"

	# Every path here is relative to the directory the server is started
	# from, which is the repository root.
	cat > "$CONFIG" <<- YAML
		core:
		  overlay: $DEMO_DIR/resources.json
		  modules: $MODULES_DIR
	YAML

	# -approve grants the data folder, the operate check, the two staged
	# upload routes and the Print-After-Upload header.
	step "[$MODE] the module installs" "$CORE" module add \
		"$CONNECTOR_MODULE@$CONNECTOR_VERSION" -config "$CONFIG" -from "$RELEASE" -approve

	step "[$MODE] the listing on disk holds one line for it" sh -c \
		"'$CORE' module list -config '$CONFIG' | tee '$WORK/list.txt' && \
		 grep -q '$CONNECTOR_NAMESPACE' '$WORK/list.txt'"

	# -idle starts no print, so the printer the gate prints on is free.
	# shellcheck disable=SC2086 # DEMO_TLS is two flags or none.
	"$DEMO" -config "$CONFIG" -dir "$DEMO_DIR" -addr "127.0.0.1:$DEMO_PORT" -password "$ADMIN_PASSWORD" \
		-printers 3 -flap 0 -cameras=false -idle $DEMO_TLS >> "$WORK/demo.log" 2>&1 &
	DEMO_PID=$!
	echo "$DEMO_PID" > "$LOCK/demo.pid"

	# shellcheck disable=SC2086 # SECURED is two flags or none.
	set -- -base "$BASE_URL" -password "$ADMIN_PASSWORD" -state "$STATE" $SECURED

	step "[$MODE] the demo serves, its printers connect and the module answers" "$DRIVER" ready "$@"
	step "[$MODE] the demo account mints an operate key and a read key" "$DRIVER" keys "$@"
	step "[$MODE] the connection test and the storage list, with and without a key" "$DRIVER" link "$@"
	step "[$MODE] Upload and Print sends a file over 1 MiB to the printer" "$DRIVER" print "$@"
	step "[$MODE] Upload keeps the file in the local storage and prints nothing" "$DRIVER" keep "$@"
	step "[$MODE] the form of PrusaSlicer 2.5 and older starts a print" "$DRIVER" form "$@"
	step "[$MODE] a read key, a file no printer prints and a busy printer are refused" "$DRIVER" refused "$@"

	stop_demo
}

drive http
drive https

echo "OK    the PrusaSlicer 2 Connector module passed $STEPS steps"
echo
echo "      the release     $RELEASE"
echo "      over HTTP       $WORK_ROOT/http"
echo "      over HTTPS      $WORK_ROOT/https"

}
main "$@"
