#!/bin/sh
# The materials module, built, installed and driven end to end.
#
# What it does, in order:
#
#   1. builds, vets and tests the Go half
#   2. holds the typed client to the module's OpenAPI fragment
#   3. lints and tests the browser half, then builds the bundle
#   4. lays the module out as a release and installs it with
#      `prusa-connect module add` into a scratch modules directory
#   5. serves a small catalogue ZIP over https (a self-signed certificate made
#      here), starts the demo with the module pointed at it through the
#      development aid of README.md, and over the API: loads the catalogue,
#      waits until the import is idle, adds a spool of a catalogue material,
#      stars another, and checks the list order
#
# The Core is the checkout this folder sits in, two levels up. Its tools build
# the demo and install the release; nothing is written there but its ignored
# build output, and the scratch files live under ./bin.
#
# The whole run is one function, called on the last line, so the shell parses
# this file to its end before executing any of it.
main() {

HERE=$(cd "$(dirname "$0")/.." && pwd)
CORE_DIR=$(cd "$HERE/../.." && pwd)

# Git Bash on Windows answers pwd with /c/..., and the core cannot open that
# path when it reads it from config.yaml, so the paths take the C:/... form.
if command -v cygpath > /dev/null 2>&1; then
	HERE=$(cygpath -m "$HERE")
	CORE_DIR=$(cygpath -m "$CORE_DIR")
fi
cd "$HERE" || exit 1

CLIENT_DIR=$HERE/client
UI_DIR=$HERE/ui
NAME=materials
MODULE=github.com/prusa3d/materials
NAMESPACE=com.github.aux.prusa3d.materials
MOUNT=/api/v1/modules/$NAMESPACE

WORK_ROOT=$CORE_DIR/bin/materials
RELEASE=$WORK_ROOT/release
MODULES_DIR=$WORK_ROOT/modules
DEMO_DIR=$WORK_ROOT/demo
CONFIG=$WORK_ROOT/config.yaml
FIRST_PORT=${MATERIALS_PORT:-18099}

CI=true
export CI

for tool in go pnpm curl openssl python3; do
	if ! command -v "$tool" > /dev/null 2>&1; then
		echo "FAIL  $tool is not on PATH, and this lane needs it."
		exit 1
	fi
done

WORK=$(mktemp -d)
# The module reads catalogue_ca from under $WORK.
if command -v cygpath > /dev/null 2>&1; then
	WORK=$(cygpath -m "$WORK")
fi
DEMO_PID=
FIXTURE_PID=

cleanup() {
	for pid in $DEMO_PID $FIXTURE_PID; do
		kill "$pid" 2> /dev/null
		wait "$pid" 2> /dev/null
	done
	rm -rf "$WORK"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

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

VERSION=$(sed -n 's/^version:[ 	]*//p' module.yaml | head -1)
if [ -z "$VERSION" ]; then
	echo "FAIL  module.yaml declares no version"
	exit 1
fi
echo " 0. the module declares version $VERSION"

# json_field prints one field of a JSON file, by a Python expression over d.
json_field() {
	python3 -I -c 'import json,sys; d=json.load(open(sys.argv[1])); print(eval(sys.argv[2]))' "$1" "$2"
}

# ── The Go half ──────────────────────────────────────────────────────
step "the Go half builds and vets" sh -c "go build ./... && go vet ./..."
step "the Go half's tests pass" go test -count=1 ./...

# ── The two packages the browser half links to ───────────────────────
if [ ! -f "$CORE_DIR/packages/svelte/dist/build/module.js" ]; then
	step "the component library builds" sh -c \
		"cd '$CORE_DIR/packages/svelte' && pnpm install --frozen-lockfile && pnpm run build"
fi

if [ ! -f "$CORE_DIR/packages/typescript/dist/index.js" ]; then
	step "the client SDK builds" sh -c \
		"cd '$CORE_DIR/packages/typescript' && pnpm install --frozen-lockfile && pnpm run build"
fi

# ── The typed client ─────────────────────────────────────────────────
step "the typed client is what the fragment generates" sh -c \
	"cd '$CORE_DIR' && go run ./cmd/generate-client-sdk -module -check \
	 -document '$HERE/openapi.json' -namespace $NAMESPACE -name materials-client -out '$CLIENT_DIR'"
step "the typed client installs" sh -c "cd '$CLIENT_DIR' && pnpm install --frozen-lockfile"
step "the client types match the fragment" sh -c \
	"cd '$CLIENT_DIR' && pnpm run generate:types && git diff --exit-code src/generated/"
step "the typed client builds" sh -c "cd '$CLIENT_DIR' && pnpm run build"

# ── The browser half ─────────────────────────────────────────────────
step "the browser half installs" sh -c "cd '$UI_DIR' && pnpm install --frozen-lockfile"
step "Chromium for the browser tests installs" sh -c \
	"cd '$UI_DIR' && pnpm exec playwright install chromium"
step "the browser half lints, type-checks and its tests pass" sh -c \
	"cd '$UI_DIR' && pnpm run check"
step "the browser half builds into a bundle" sh -c "cd '$UI_DIR' && pnpm run build"

if [ ! -f "$UI_DIR/dist/module.yaml" ] || [ ! -f "$UI_DIR/dist/ui/index.js" ]; then
	echo "FAIL  $UI_DIR/dist is missing module.yaml or ui/index.js"
	exit 1
fi

# ── The release ──────────────────────────────────────────────────────
BINARY="${NAME}_$(go env GOOS)_$(go env GOARCH)$(go env GOEXE)"

pack() {
	go build -o "$WORK_ROOT/$BINARY" . || return 1
	sh "$CORE_DIR/extensions/scripts/pack-release.sh" "$UI_DIR/dist" "$WORK_ROOT/$BINARY" "$RELEASE"
}
step "the release packs" pack

cat > "$CONFIG" <<- YAML
	core:
	  overlay: $DEMO_DIR/resources.json
	  modules: $MODULES_DIR
	  authz:
	    database: $DEMO_DIR/config.d/authz.db
YAML

CORE=$WORK_ROOT/prusa-connect$(go env GOEXE)
DEMO=$WORK_ROOT/demo-server$(go env GOEXE)
STAMP="-X github.com/prusa3d/prusa-connect/internal/kernel/version.stamped=$(cd "$CORE_DIR" && sh scripts/build-version.sh)"

step "the core and the demo build" sh -c \
	"cd '$CORE_DIR' && go build -ldflags '$STAMP' -o '$CORE' ./cmd/prusa-connect && \
	 go build -ldflags '$STAMP' -o '$DEMO' ./cmd/demo"

step "the module installs" "$CORE" module add "$MODULE@$VERSION" -config "$CONFIG" -from "$RELEASE" -approve
step "the install granted a SQLite database" grep -q '"engine": *"sqlite"' "$DEMO_DIR/resources.json"

# ── The catalogue, served over https ─────────────────────────────────
# A self-signed certificate for 127.0.0.1, which the module trusts only
# because the catalogue_ca setting names it.
fixture() {
	openssl req -x509 -newkey rsa:2048 -nodes -days 1 -subj /CN=materials-check \
		-addext 'subjectAltName=IP:127.0.0.1' \
		-keyout "$WORK/key.pem" -out "$WORK/ca.pem" || return 1

	python3 -I - "$WORK" <<- 'PY' || return 1
	import sys, zipfile
	work = sys.argv[1]
	with zipfile.ZipFile(work + "/catalogue.zip", "w") as z:
	    z.writestr("r-main/data/manifest.yaml", "data_hash: e2e-1\nlast_modified: 2026-10-28\n")
	    z.writestr("r-main/data/brands/acme.yaml",
	               "uuid: 863c5f0c-04a5-50c9-b5a2-5a8e81d5821f\nslug: acme\nname: Acme\n")
	    for slug, uuid, name, colour in (
	        ("acme-pla-red", "1b4e28ba-2fa1-11d2-883f-0016d3cca427", "Acme PLA Red", "#cc0000ff"),
	        ("acme-pla-blue", "2b4e28ba-2fa1-11d2-883f-0016d3cca427", "Acme PLA Blue", "#0000ccff"),
	    ):
	        z.writestr("r-main/data/materials/acme/%s.yaml" % slug,
	                   "uuid: %s\nslug: %s\nname: %s\nbrand:\n  slug: acme\nclass: FFF\ntype: PLA\n"
	                   "primary_color:\n  color_rgba: '%s'\n" % (uuid, slug, name, colour))
	PY

	python3 -I - "$WORK" "$FIXTURE_PORT" > "$WORK/fixture.log" 2>&1 <<- 'PY' &
	import http.server, ssl, sys
	work, port = sys.argv[1], int(sys.argv[2])
	class H(http.server.SimpleHTTPRequestHandler):
	    def __init__(self, *a, **k): super().__init__(*a, directory=work, **k)
	ctx = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
	ctx.load_cert_chain(work + "/ca.pem", work + "/key.pem")
	srv = http.server.HTTPServer(("127.0.0.1", port), H)
	srv.socket = ctx.wrap_socket(srv.socket, server_side=True)
	srv.serve_forever()
	PY
	FIXTURE_PID=$!
	waited=0
	while ! curl -fsS --cacert "$WORK/ca.pem" -o /dev/null "https://127.0.0.1:$FIXTURE_PORT/catalogue.zip" 2> /dev/null; do
		waited=$((waited + 1))
		[ "$waited" -lt 20 ] || return 1
		sleep 1
	done
}

port_free() {
	curl -sS -o /dev/null --max-time 2 "http://127.0.0.1:$1/" > /dev/null 2>&1
	[ "$?" -eq 7 ]
}

pick_port() {
	port=$FIRST_PORT
	while [ "$port" -lt $((FIRST_PORT + 50)) ]; do
		if port_free "$port" && port_free "$((port + 100))"; then
			DEMO_ADDR=127.0.0.1:$port
			BASE_URL=http://$DEMO_ADDR
			FIXTURE_PORT=$((port + 100))
			return 0
		fi
		port=$((port + 1))
	done
	echo "no free port from $FIRST_PORT up on 127.0.0.1" >&2
	return 1
}

step "free ports for the demo and the fixture" pick_port
echo "    the demo binds $DEMO_ADDR, the catalogue is served on $FIXTURE_PORT"
step "the catalogue ZIP is served over https" fixture

# ── The demo ─────────────────────────────────────────────────────────

"$DEMO" -config "$CONFIG" -dir "$DEMO_DIR" -addr "$DEMO_ADDR" \
	-printers 1 -flap 0 -cameras=false >> "$WORK/demo.log" 2>&1 &
DEMO_PID=$!

wait_for() {
	# wait_for <seconds> <command...>: retries until the command passes.
	limit=$1
	shift
	waited=0
	while [ "$waited" -lt "$limit" ]; do
		if ! kill -0 "$DEMO_PID" 2> /dev/null; then
			echo "the demo ended before this passed" >&2
			cat "$WORK/demo.log" >&2
			return 1
		fi
		"$@" > /dev/null 2>&1 && return 0
		waited=$((waited + 1))
		sleep 1
	done
	cat "$WORK/demo.log" >&2
	return 1
}

healthy() { curl -fsS "$BASE_URL/api/v1/health"; }

read_password() {
	PASSWORD=$(sed -n 's/^ *password  *//p' "$WORK/demo.log" | head -1)
	[ -n "$PASSWORD" ]
}

sign_in() {
	rm -f "$WORK/cookies"
	curl -fsS -c "$WORK/cookies" -X POST "$BASE_URL/api/v1/auth/login" \
		-H 'content-type: application/json' \
		-d "{\"username\":\"demo\",\"password\":\"$PASSWORD\"}"
}

# get and send write the body to $WORK/out.json, and fail on a status >= 400
# after printing that body, so a refusal's code is read in the log.
call() {
	status=$(curl -sS -b "$WORK/cookies" -o "$WORK/out.json" -w '%{http_code}' "$@") || return 1
	[ "$status" -lt 400 ] && return 0
	echo "$status: $(cat "$WORK/out.json")" >&2
	return 1
}
get() { call "$BASE_URL$MOUNT$1"; }
send() {
	method=$1
	path=$2
	shift 2
	call -X "$method" "$BASE_URL$MOUNT$path" -H 'content-type: application/json' "$@"
}

step "the demo serves" wait_for 120 healthy
step "the demo prints an account" wait_for 60 read_password
step "the demo account signs in" wait_for 60 sign_in

# The module downloads the fixture's catalogue, through its two settings,
# written as the Settings tab writes them: the instance read, its config set,
# and the whole sent back under the tag the read carried. An entry under
# resources: in the configuration file would hide the install's whole
# declaration instead.
point_at_fixture() {
	instance=$BASE_URL/api/v1/resources/sink/$NAME
	call -D "$WORK/headers" "$instance" || return 1
	tag=$(sed -n 's/^[Ee][Tt][Aa][Gg]: *//p' "$WORK/headers" | tr -d '\r')
	python3 -I - "$WORK/out.json" "https://127.0.0.1:$FIXTURE_PORT/catalogue.zip" "$WORK/ca.pem" > "$WORK/spec.json" <<- 'PY' || return 1
	import json, sys
	spec = json.load(open(sys.argv[1]))
	spec.setdefault("config", {}).update(catalogue_url=sys.argv[2], catalogue_ca=sys.argv[3])
	print(json.dumps(spec))
	PY
	call -X PUT "$instance" -H 'content-type: application/json' -H "If-Match: $tag" --data-binary @"$WORK/spec.json"
}
step "the module is pointed at the fixture's catalogue" point_at_fixture
step "the module's routes answer" wait_for 60 get /catalogue

# ── The drive ────────────────────────────────────────────────────────

check_empty() { get /catalogue && [ "$(json_field "$WORK/out.json" "d['loaded']")" = False ]; }
step "the catalogue starts empty" check_empty

start_import() {
	send POST /catalogue/download -d '{}' && [ "$(json_field "$WORK/out.json" "d['import']['phase']")" != failed ]
}
step "the catalogue download starts" start_import

idle() {
	get /catalogue || return 1
	phase=$(json_field "$WORK/out.json" "d['import']['phase']")
	if [ "$phase" = failed ]; then
		cat "$WORK/out.json" >&2
		return 2
	fi
	[ "$phase" = idle ] && [ "$(json_field "$WORK/out.json" "d['loaded']")" = True ]
}
step "the import runs to idle and the catalogue is loaded" wait_for 60 idle

RED=1b4e28ba-2fa1-11d2-883f-0016d3cca427
BLUE=2b4e28ba-2fa1-11d2-883f-0016d3cca427

add_spools() {
	for material in "$RED" "$BLUE"; do
		send POST /spools -d "{\"material_uuid\":\"$material\",\"remaining_weight_g\":800,\"full_weight_g\":1000,\"empty_weight_g\":200,\"notes\":\"\",\"quantity\":1}" || return 1
		[ "$(json_field "$WORK/out.json" "d['added']")" = 1 ] || return 1
	done
}
step "a spool of each catalogue material is added" add_spools

star_blue() { send PUT "/favourites/$BLUE" && get /materials && [ "$(json_field "$WORK/out.json" "d['total']")" = 2 ]; }
step "the second material is starred" star_blue

order() {
	[ "$(json_field "$WORK/out.json" "d['rows'][0]['uuid']")" = "$BLUE" ] &&
		[ "$(json_field "$WORK/out.json" "d['rows'][0]['is_favourite']")" = True ] &&
		[ "$(json_field "$WORK/out.json" "d['stock_spools']")" = 2 ]
}
step "the starred material is listed first" order

echo "OK    the materials module passed $STEPS steps"
echo
echo "      installed into  $MODULES_DIR"
echo "      the config      $CONFIG"

}
main "$@"
