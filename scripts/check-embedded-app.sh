#!/bin/sh
# Checks that a binary built with the `full` tag has the web application in it
# and serves it at /. `make embedded-app-check` runs it, after `make embed-app`
# has built the application into internal/transport/http/httpapi/appdist.
#
# It builds cmd/demo with the tag, starts it, and reads the page at / without a
# credential. A binary built without the tag, or with an empty appdist, answers
# 404 at / and fails no other check.
#
# Needs go and curl.
#
# The whole run is one function, called on the last line, so the shell parses
# this file to its end before executing any of it. sh reads a script while it
# runs, and an edit during a run would otherwise change what runs.
main() {

ROOT=$(cd "$(dirname "$0")/.." && pwd)
cd "$ROOT" || exit 1

# Not 8080, which a developer's own server may use, and not the address of
# scripts/check-example-spools.sh.
ADDR=127.0.0.1:18098
BASE_URL=http://$ADDR

WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT INT TERM

BINARY=$WORK/demo$(go env GOEXE)

echo "=== The embedded application ==="

echo "1. the demo builds with the full tag..."
if ! go build -tags full -o "$BINARY" ./cmd/demo > "$WORK/build.log" 2>&1; then
	echo "FAIL  the demo did not build"
	cat "$WORK/build.log"
	exit 1
fi

# cmd/demo needs no configuration file. One printer and no cameras are enough,
# because the check only needs a server that answers.
"$BINARY" -dir "$WORK/state" -addr "$ADDR" -printers 1 -flap 0 -cameras=false \
	> "$WORK/demo.log" 2>&1 &
DEMO_PID=$!
trap 'kill "$DEMO_PID" 2> /dev/null; rm -rf "$WORK"' EXIT INT TERM

echo "2. the demo serves..."
waited=0
while :; do
	if curl -fsS "$BASE_URL/api/v1/health" > /dev/null 2>&1; then
		break
	fi

	if ! kill -0 "$DEMO_PID" 2> /dev/null; then
		echo "FAIL  the demo ended before it served"
		cat "$WORK/demo.log"
		exit 1
	fi

	waited=$((waited + 1))
	if [ "$waited" -ge 120 ]; then
		echo "FAIL  the demo did not answer its liveness path in 120 seconds"
		cat "$WORK/demo.log"
		exit 1
	fi

	sleep 1
done

echo "3. / answers the entry document..."
# Without a credential: a visitor who is not signed in gets this page, and the
# application signs in from inside it.
if ! curl -fsS "$BASE_URL/" > "$WORK/index.html" 2> "$WORK/curl.log"; then
	echo "FAIL  / did not answer. A binary with no app in it answers 404 here."
	cat "$WORK/curl.log"
	cat "$WORK/demo.log"
	exit 1
fi

# The built page has both the element the application renders into and its
# module script. A placeholder page could have one of them.
for wanted in '<div id="app">' '<script type="module"'; do
	if ! grep -q "$wanted" "$WORK/index.html"; then
		echo "FAIL  / answered a document with no $wanted in it."
		echo "      The binary carries an appdist with no build in it."
		head -40 "$WORK/index.html"
		exit 1
	fi
done

echo "4. / answers a browser with the stored gzip copy..."
# The build replaces each text file with a gzip copy. A binary built from an
# uncompressed appdist still serves the page, so step 3 cannot see the
# difference and only this header can.
if ! curl -fsS -D "$WORK/headers" -o /dev/null -H "Accept-Encoding: gzip" "$BASE_URL/" 2> "$WORK/curl.log"; then
	echo "FAIL  / did not answer a request that accepts gzip."
	cat "$WORK/curl.log"
	exit 1
fi

if ! grep -qi '^content-encoding: gzip' "$WORK/headers"; then
	echo "FAIL  / answered a browser without Content-Encoding: gzip."
	echo "      The binary carries an appdist that cmd/compress-app did not run over."
	cat "$WORK/headers"
	exit 1
fi

kill "$DEMO_PID" 2> /dev/null
wait "$DEMO_PID" 2> /dev/null

echo "OK    the binary carries the application and serves it at /"

}
main "$@"
