#!/bin/sh
# Prepares a running Fractal mockup for a person to click through: hq admits
# brno and garage, and the servers hold folders to share and to browse.
#
#   1. hq approves brno and garage, which it holds as pending
#   2. hq shares projects/fixtures, with one G-code file in it
#   3. workshop shares jigs, with one G-code file in it
#   4. brno holds orders/2026, shared by nobody, for the Shared switch
#
# Run it after make mockup-up. make mockup-check expects the approvals still
# pending, so run the check on a fresh mockup and this after it.
#
# Every step may run twice: a folder that is there and a node already admitted
# are not failures.
#
# The whole run is one function, called on the last line, so the shell parses
# this file to its end before executing any of it.
main() {

ROOT=$(cd "$(dirname "$0")/../.." && pwd)
cd "$ROOT" || exit 1

COMPOSE=${COMPOSE:-docker-compose.local.yml}

HQ_PORT=${PCL_MOCKUP_HQ_PORT:-18080}
BRNO_PORT=${PCL_MOCKUP_BRNO_PORT:-18081}
WORKSHOP_PORT=${PCL_MOCKUP_WORKSHOP_PORT:-18082}
GARAGE_PORT=${PCL_MOCKUP_GARAGE_PORT:-18083}

# The namespace the host gives the module path docker/mockup/start.sh installs.
MOUNT=/api/v1/modules/com.github.aux.prusa3d.fractal-link

WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT INT TERM

printf 'G28 ; home\n' > "$WORK/part.gcode"

# password reads the demo account's password out of one container's log.
password() {
	docker compose -f "$COMPOSE" logs "$1" 2> /dev/null |
		sed -n 's/^.*  password  \(.*\)$/\1/p' |
		tail -n 1 |
		tr -d '\r'
}

# sign_in signs the demo account in against one server and keeps its cookie.
sign_in() {
	pass=$(password "$1")
	if [ -z "$pass" ]; then
		echo "FAIL $1 printed no account to sign in with. Run make mockup-up first."
		exit 1
	fi
	curl -fsS -c "$WORK/$1.jar" -X POST "http://127.0.0.1:$2/api/v1/auth/login" \
		-H 'content-type: application/json' \
		-d "{\"username\":\"demo\",\"password\":\"$pass\"}" > /dev/null ||
		{ echo "FAIL $1 refused the demo account."; exit 1; }
}

# call sends one request as the demo account of one server and prints the
# HTTP status.
call() {
	name=$1
	port=$2
	shift 2
	curl -sS -o /dev/null -w '%{http_code}' -b "$WORK/$name.jar" "$@" |
		sed "s|^|$name |"
	echo
}

# folder creates a folder in the storage local. 409 means it is there.
folder() {
	call "$1" "$2" -X POST "http://127.0.0.1:$2/api/v1/storages/local/folders" \
		-H 'content-type: application/json' -d "{\"path\":\"$3\"}" > /dev/null
}

# file puts the G-code file at a path of the storage local.
file() {
	call "$1" "$2" -X PUT "http://127.0.0.1:$2/api/v1/storages/local/content?path=$3" \
		-H 'content-type: application/octet-stream' --data-binary "@$WORK/part.gcode" > /dev/null
}

# share shares a folder of the storage local with the servers below. The
# path is one segment of the address, so its slashes are sent as %2F.
share() {
	encoded=$(printf '%s' "$3" | sed 's|/|%2F|g')
	status=$(call "$1" "$2" -X PUT "http://127.0.0.1:$2$MOUNT/files/shares/local/$encoded")
	case $status in
		*204) echo "OK   $1 shares $3" ;;
		*) echo "FAIL $1 did not share $3: $status"; exit 1 ;;
	esac
}

sign_in hq "$HQ_PORT"
sign_in brno "$BRNO_PORT"
sign_in workshop "$WORKSHOP_PORT"

for node in brno garage; do
	call hq "$HQ_PORT" -X POST \
		"http://127.0.0.1:$HQ_PORT/api/v1/links/fractal/nodes/$node/approve" > /dev/null
done
echo "OK   hq approved brno and garage"

folder hq "$HQ_PORT" projects
folder hq "$HQ_PORT" projects/fixtures
file hq "$HQ_PORT" projects/fixtures/benchy.gcode
share hq "$HQ_PORT" projects/fixtures

folder workshop "$WORKSHOP_PORT" jigs
file workshop "$WORKSHOP_PORT" jigs/clamp.gcode
share workshop "$WORKSHOP_PORT" jigs

folder brno "$BRNO_PORT" orders
folder brno "$BRNO_PORT" orders/2026
echo "OK   brno holds orders/2026, shared by nobody"

echo
echo "Open Storage on any server and sign in as demo:"
for pair in "hq $HQ_PORT" "brno $BRNO_PORT" "workshop $WORKSHOP_PORT" "garage $GARAGE_PORT"; do
	set -- $pair
	echo "  $1  http://127.0.0.1:$2/storage  password $(password "$1")"
done
}

main "$@"
