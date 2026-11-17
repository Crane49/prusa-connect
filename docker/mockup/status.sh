#!/bin/sh
# Print every server of the Fractal mockup and the links each one reports, read
# through the API the way any client reads it.
#
# The demo account's password is new on every run and the demo prints it once,
# so this reads it back out of the container's log rather than being told it.
# There is no flag to set it: cmd/demo takes none.
set -eu

COMPOSE=${COMPOSE:-docker-compose.local.yml}
HQ_PORT=${PCL_MOCKUP_HQ_PORT:-18080}
BRNO_PORT=${PCL_MOCKUP_BRNO_PORT:-18081}
WORKSHOP_PORT=${PCL_MOCKUP_WORKSHOP_PORT:-18082}
GARAGE_PORT=${PCL_MOCKUP_GARAGE_PORT:-18083}

# password reads the demo account's password out of one container's log.
password() {
    docker compose -f "$COMPOSE" logs "$1" 2>/dev/null |
        sed -n 's/^.*  password  \(.*\)$/\1/p' |
        tail -n 1 |
        tr -d '\r'
}

# report signs in against one server and prints the links it serves.
report() {
    name=$1
    port=$2

    pass=$(password "$name")
    if [ -z "$pass" ]; then
        echo "FAIL  $name has printed no account yet. Run make mockup-up first"
        return 1
    fi

    jar=$(mktemp)

    curl -sS -c "$jar" -X POST "http://127.0.0.1:$port/api/v1/auth/login" \
        -H "Content-Type: application/json" \
        -d "{\"username\":\"demo\",\"password\":\"$pass\"}" > /dev/null

    echo "=== $name on http://127.0.0.1:$port ==="
    curl -sS -b "$jar" "http://127.0.0.1:$port/api/v1/links"
    echo ""

    rm -f "$jar"
}

report hq "$HQ_PORT"
report brno "$BRNO_PORT"
report workshop "$WORKSHOP_PORT"
report garage "$GARAGE_PORT"
