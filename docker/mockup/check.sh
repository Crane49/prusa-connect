#!/bin/sh
# Drive the four servers of the Fractal mockup over their own HTTP APIs, and
# fail with a plain sentence when a step does not happen in time.
#
# Every claim here is read through the API a client reads, never out of a log
# and never with docker exec. The one thing read from a log is the demo
# account's password, which the demo makes new on every run and prints once.
#
# What it proves, in order:
#
#   1. all four servers answer and the demo account signs in
#   2. hq holds brno and garage waiting for a person, each with its public key
#   3. approving both through the API connects both
#   4. brno admitted workshop with no person, because ask_first is false there
#   5. workshop's place in the tree is hq/brno/workshop, so a message from
#      workshop to hq crosses a server in the middle
#   6. the links body of hq has the node path, the listeners and the counters
#   7. the install on every server granted its link the printers of that
#      server, and the printers of workshop appear at hq through the module's
#      own route
#   8. a command sent from hq is refused while node:brno holds no grant on
#      that printer of workshop, and is no longer refused once workshop
#      writes one
#   9. stopping brno makes hq report it as not alive, and starting brno again
#      brings it back
#  10. revoking garage at hq ends its connection, and a restart does not get it
#      back in
#
# The whole run is one function, called on the last line, so the shell parses
# this file to its end before executing any of it. sh reads a script while it
# runs, and an edit landing mid-run moves the read offset under the reader.
main() {

ROOT=$(cd "$(dirname "$0")/../.." && pwd)
cd "$ROOT" || exit 1

COMPOSE=${COMPOSE:-docker-compose.local.yml}

HQ_PORT=${PCL_MOCKUP_HQ_PORT:-18080}
BRNO_PORT=${PCL_MOCKUP_BRNO_PORT:-18081}
WORKSHOP_PORT=${PCL_MOCKUP_WORKSHOP_PORT:-18082}
GARAGE_PORT=${PCL_MOCKUP_GARAGE_PORT:-18083}

# How long one claim may take before this run calls it a failure. A connection
# that opens again after a restart is the slowest of them.
PATIENCE=${PCL_MOCKUP_PATIENCE:-120}

WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT INT TERM

STEPS=0
FAILED=0

# say prints one line for one claim: what was checked and how it ended.
say() {
	printf '%-4s %s\n' "$1" "$2"
}

# fail ends the run with one plain sentence, and prints what the server
# answered so the reason is in front of the reader.
fail() {
	say FAIL "$1"
	if [ -s "$WORK/last.json" ]; then
		echo "      the server answered:"
		sed 's/^/      /' "$WORK/last.json"
	fi
	exit 1
}

# password reads the demo account's password out of one container's log.
password() {
	docker compose -f "$COMPOSE" logs "$1" 2> /dev/null |
		sed -n 's/^.*  password  \(.*\)$/\1/p' |
		tail -n 1 |
		tr -d '\r'
}

# sign_in signs the demo account in against one server and keeps its cookie.
sign_in() {
	name=$1
	port=$2

	pass=$(password "$name")
	if [ -z "$pass" ]; then
		fail "$name printed no account to sign in with. Run make mockup-up first."
	fi

	curl -fsS -c "$WORK/$name.jar" -X POST "http://127.0.0.1:$port/api/v1/auth/login" \
		-H 'content-type: application/json' \
		-d "{\"username\":\"demo\",\"password\":\"$pass\"}" > /dev/null
}

# links reads GET /api/v1/links of one server into $WORK/last.json.
links() {
	name=$1
	port=$2

	curl -fsS -b "$WORK/$name.jar" "http://127.0.0.1:$port/api/v1/links" > "$WORK/last.json"
}

# node_field prints one member of one node under the fractal link, for the node
# id named. It reads the JSON with the one tool every machine here has, so the
# check needs no jq.
#
# It prints the members of that node one per line and picks the one asked for.
node_field() {
	id=$1
	field=$2

	tr ',' '\n' < "$WORK/last.json" |
		sed -n "/\"id\":\"$id\"/,/\"latency_ms\"/p" |
		sed -n "s/.*\"$field\":\([^,}]*\).*/\1/p" |
		head -n 1
}

# holds answers whether the last answer has one string in it.
holds() {
	grep -q "$1" "$WORK/last.json"
}

# until_true runs a check every second until it holds or the patience runs out.
# what is the sentence the failure prints.
until_true() {
	what=$1
	shift

	waited=0
	while [ "$waited" -lt "$PATIENCE" ]; do
		if "$@"; then
			STEPS=$((STEPS + 1))
			say OK "$what"
			return 0
		fi

		waited=$((waited + 1))
		sleep 1
	done

	fail "$what did not happen within ${PATIENCE}s."
}

for tool in docker curl; do
	if ! command -v "$tool" > /dev/null 2>&1; then
		fail "$tool is not on PATH, and this check needs it."
	fi
done

echo "The Fractal mockup, driven through the API of each server."
echo

# 1. Every server answers and the demo account signs in.
for pair in "hq $HQ_PORT" "brno $BRNO_PORT" "workshop $WORKSHOP_PORT" "garage $GARAGE_PORT"; do
	set -- $pair
	sign_in "$1" "$2"
done

STEPS=$((STEPS + 1))
say OK "all four servers answer and the demo account signs in"

# 2. hq holds brno and garage waiting for a person, each with its public key.
waiting_at_hq() {
	links hq "$HQ_PORT" || return 1

	[ "$(node_field brno admission)" = '"pending"' ] || return 1
	[ "$(node_field garage admission)" = '"pending"' ] || return 1

	[ -n "$(node_field brno public_key)" ] || return 1
	[ -n "$(node_field garage public_key)" ] || return 1

	return 0
}

until_true "hq holds brno and garage waiting for a person, with the public key of each" \
	waiting_at_hq

# 3. Approving both through the API connects both.
approve_at_hq() {
	curl -fsS -X POST -b "$WORK/hq.jar" \
		"http://127.0.0.1:$HQ_PORT/api/v1/links/fractal/nodes/$1/approve" > "$WORK/last.json"
}

approve_at_hq brno || fail "hq refused to approve brno."
approve_at_hq garage || fail "hq refused to approve garage."

STEPS=$((STEPS + 1))
say OK "hq approved brno and garage through POST /api/v1/links/fractal/nodes/{id}/approve"

both_connected() {
	links hq "$HQ_PORT" || return 1

	[ "$(node_field brno admission)" = '"admitted"' ] || return 1
	[ "$(node_field garage admission)" = '"admitted"' ] || return 1
	[ "$(node_field brno alive)" = "true" ] || return 1
	[ "$(node_field garage alive)" = "true" ] || return 1

	return 0
}

until_true "brno and garage are admitted and their connections to hq are open" both_connected

# 4. brno admitted workshop with no person, because ask_first is false there.
workshop_at_brno() {
	links brno "$BRNO_PORT" || return 1

	[ "$(node_field workshop admission)" = '"admitted"' ] || return 1
	[ "$(node_field workshop alive)" = "true" ] || return 1

	return 0
}

until_true "brno admitted workshop with no person, because ask_first is false there" \
	workshop_at_brno

# 5. workshop's place in the tree names three servers, so a message from
#    workshop to hq crosses brno.
workshop_path() {
	links workshop "$WORKSHOP_PORT" || return 1

	holds '"node_path":"hq/brno/workshop"'
}

until_true "workshop's place in the tree is hq/brno/workshop, so a message to hq crosses brno" \
	workshop_path

# 6. The links body of hq has the node path, the listeners and the counters.
links hq "$HQ_PORT" || fail "hq did not answer GET /api/v1/links."

holds '"node_path":"hq"' || fail "the links body of hq names no place in the tree."
holds '"port":4222' || fail "the links body of hq names no port for the join listener."
holds '"port":7422' || fail "the links body of hq names no port for the connections below."
holds '"counters"' || fail "the links body of hq has no counters."

STEPS=$((STEPS + 1))
say OK "the links body of hq has its place in the tree, both listeners and the counters"

# 7. Every server lets its link read its printers, and the printers of workshop
#    then appear at hq.
#
# A module reads the printers of its own server as the principal sink:<name of
# the instance>. That principal holds nothing until somebody writes a grant, and
# the install each container runs at its start writes it: `module add -approve`
# grants the module what the permissions of its manifest name. One grant on the
# organization covers the whole fleet of that server. This step reads that grant
# back rather than writing it.
grant() {
	curl -fsS -X POST -b "$WORK/$1.jar" "http://127.0.0.1:$2$3" \
		-H 'content-type: application/json' -d "$4" > "$WORK/last.json"
}

for pair in "brno $BRNO_PORT" "garage $GARAGE_PORT" "workshop $WORKSHOP_PORT"; do
	set -- $pair
	curl -fsS -b "$WORK/$1.jar" "http://127.0.0.1:$2/api/v1/grants/org/local" > "$WORK/last.json" ||
		fail "$1 did not answer for the grants written on its organization."

	holds '"subject":"sink:fractal"' ||
		fail "the install on $1 wrote no grant for its link, so the link reads no printer."
done

STEPS=$((STEPS + 1))
say OK "the install on brno, garage and workshop granted each link the printers of its own server"

# The printers of workshop reach hq through brno, so this waits for the whole
# path: workshop reports upward, brno passes it on, hq writes it into its copy.
MOUNT=/api/v1/modules/com.github.aux.prusa3d.fractal-link

workshop_in_the_listing() {
	curl -fsS -b "$WORK/hq.jar" "http://127.0.0.1:$HQ_PORT$MOUNT/nodes" > "$WORK/last.json" || return 1

	holds '"path":"hq/brno/workshop"'
}

until_true "hq lists workshop under GET <mount>/nodes, the route that shows the servers below" \
	workshop_in_the_listing

printer_at_hq() {
	curl -fsS -b "$WORK/hq.jar" \
		"http://127.0.0.1:$HQ_PORT$MOUNT/nodes/hq/brno/workshop?limit=500" > "$WORK/last.json" || return 1

	holds '"interface":"Printer1"' || return 1
	holds '"status":"OK"' || return 1
	holds '"state":' || return 1
	holds '"interface":"FDMPrinterHead1"' || return 1
	holds '"tempNozzle":' || return 1

	return 0
}

until_true "hq reads a printer of workshop: its interfaces, its state and the temperature of its nozzle" \
	printer_at_hq

# The instance of a printer on the network is the identifier its own server
# gave it with the hyphens removed, because an Interface Instance ID takes
# none. This reads one back out of the listing hq just answered.
INSTANCE=$(sed -n 's/.*"interface":"Printer1","instance":"\([^"]*\)".*/\1/p' "$WORK/last.json" |
	head -n 1)

if [ -z "$INSTANCE" ]; then
	fail "the interfaces of workshop at hq name no printer."
fi

# 8. A command from hq is refused while the server above workshop holds no
#    grant on that printer, and runs once an operator writes one.
#
# On workshop the command runs as node:brno, the server directly above it, and
# not as hq. Every server in the tree decides for itself.
CODE_NOT_ALLOWED='"code":8209'

beep() {
	curl -fsS -X POST -b "$WORK/hq.jar" \
		"http://127.0.0.1:$HQ_PORT$MOUNT/nodes/hq/brno/workshop/interfaces/Printer1/$INSTANCE/methods/Beep" \
		-H 'content-type: application/json' -d '[]' > "$WORK/last.json"
}

refused_without_a_grant() {
	beep || return 1

	holds "$CODE_NOT_ALLOWED"
}

until_true "a command sent from hq is refused, because node:brno holds no grant on that printer of workshop" \
	refused_without_a_grant

# The device identifier at workshop is the instance with its hyphens put back.
# This reads workshop's own device listing rather than guessing where they go.
DEVICE=$(curl -fsS -b "$WORK/workshop.jar" "http://127.0.0.1:$WORKSHOP_PORT/api/v1/devices" |
	tr ',' '\n' |
	sed -n 's/.*"id":"\([0-9a-fA-F-]*\)".*/\1/p' |
	while IFS= read -r id; do
		if [ "$(echo "$id" | tr -d '-')" = "$INSTANCE" ]; then
			echo "$id"
			break
		fi
	done)

if [ -z "$DEVICE" ]; then
	fail "workshop holds no printer under the instance $INSTANCE that hq named."
fi

grant workshop "$WORKSHOP_PORT" "/api/v1/grants/device/$DEVICE" \
	'{"relation":"operator","subject":"node:brno"}' ||
	fail "workshop refused to grant node:brno the operator role on that printer."

ran_with_a_grant() {
	beep || return 1

	if holds "$CODE_NOT_ALLOWED"; then
		return 1
	fi

	return 0
}

until_true "the same command is no longer refused for want of a grant, once workshop grants node:brno the operator role" \
	ran_with_a_grant

# 9. Stopping brno makes hq report it as not alive, and starting brno again
#    brings it back.
docker compose -f "$COMPOSE" stop brno > /dev/null 2>&1 || fail "brno would not stop."

brno_lost() {
	links hq "$HQ_PORT" || return 1

	[ "$(node_field brno alive)" = "false" ]
}

until_true "hq reports brno as not alive after brno stopped" brno_lost

docker compose -f "$COMPOSE" start brno > /dev/null 2>&1 || fail "brno would not start again."

brno_back() {
	links hq "$HQ_PORT" || return 1

	[ "$(node_field brno alive)" = "true" ]
}

until_true "hq reports brno as alive again after brno started" brno_back

# 10. Revoking garage at hq ends its connection, and a restart does not get it
#    back in.
curl -fsS -X POST -b "$WORK/hq.jar" \
	"http://127.0.0.1:$HQ_PORT/api/v1/links/fractal/nodes/garage/revoke" > "$WORK/last.json" ||
	fail "hq refused to revoke garage."

garage_out() {
	links hq "$HQ_PORT" || return 1

	[ "$(node_field garage admission)" = '"revoked"' ] || return 1
	[ "$(node_field garage alive)" = "false" ] || return 1

	return 0
}

until_true "revoking garage at hq ended its connection" garage_out

docker compose -f "$COMPOSE" restart garage > /dev/null 2>&1 ||
	fail "garage would not start again."

garage_stays_out() {
	sleep 20

	links hq "$HQ_PORT" || return 1

	[ "$(node_field garage alive)" = "false" ]
}

until_true "garage did not get back in after a restart, because hq revoked it" \
	garage_stays_out

echo
say OK "$STEPS claims held"

exit "$FAILED"

}
main "$@"
