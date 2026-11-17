#!/bin/sh
# The OpenID Connect sign-in, end to end, against real providers.
#
# For each provider, in order:
#
#   1. starts the provider in a container, with a client and two people
#      already in it (docker/<provider>-fixture)
#   2. reads the subject the provider names each person by
#   3. starts a core that has never run, in a folder of its own
#   4. drives a real Chromium through the first-run wizard to the sign-in at
#      the provider, through the restart, a sign-in and a sign-out of the
#      administrator, and a sign-in of a person with no rights
#      (frontend/test/oidc)
#   5. stops the core and removes the provider
#
# With no provider named, CI runs all three. A run outside CI (no
# GITHUB_ACTIONS) runs only the lightest provider that works on this machine:
# Authelia on Linux, which is one small Go binary, and Keycloak elsewhere. The
# core trusts Authelia's certificate through SSL_CERT_FILE, and Go reads that
# only on Linux. Authentik needs a server, a worker, Postgres and Redis, so it
# is never the local default. Name providers to run only those, or name `all`:
#
#   sh scripts/check-oidc-providers.sh keycloak
#   sh scripts/check-oidc-providers.sh all
#
# The core is built with the web app in it, as a release is, because the
# wizard and the sign-in page are what a person sees. The provider and the core
# are both on 127.0.0.1, so the browser and the core reach the issuer at one
# address.
#
# Needs go, pnpm, node, curl, docker and a Playwright Chromium.
#
# The whole run is one function, called on the last line, so the shell parses
# this file to its end before executing any of it.
main() {

ROOT=$(cd "$(dirname "$0")/.." && pwd)
cd "$ROOT" || exit 1

APP_DIR=frontend
COMPOSE="docker compose -f docker-compose.local.yml"

# Everything this run writes goes under bin/, which git ignores.
WORK_ROOT=bin/oidc-providers

# The address every provider fixture has registered as the redirect target.
# Not 8080, and not the address of another lane here.
CORE_ADDR=127.0.0.1:18097
BASE_URL=http://$CORE_ADDR

ALL_PROVIDERS="keycloak authentik authelia"
PROVIDERS=$*
if [ "$PROVIDERS" = all ]; then
	PROVIDERS=$ALL_PROVIDERS
elif [ -z "$PROVIDERS" ] && [ -n "${GITHUB_ACTIONS:-}" ]; then
	PROVIDERS=$ALL_PROVIDERS
elif [ -z "$PROVIDERS" ] && [ "$(uname -s)" = Linux ]; then
	PROVIDERS=authelia
	echo "Running authelia, the lightest provider here. OIDC_PROVIDERS=all runs every provider."
elif [ -z "$PROVIDERS" ]; then
	PROVIDERS=keycloak
	echo "Running keycloak: authelia needs Linux, and authentik is the heaviest. OIDC_PROVIDERS=all runs every provider."
fi

CI=true
export CI

for tool in go pnpm node curl; do
	if ! command -v "$tool" > /dev/null 2>&1; then
		echo "FAIL  $tool is not on PATH, and this lane needs it."
		exit 1
	fi
done

# Without Docker there is no provider to sign in at. A workstation skips the
# lane, and a CI runner, which is meant to run it, fails.
if ! docker info > /dev/null 2>&1; then
	if [ -n "${GITHUB_ACTIONS:-}" ]; then
		echo "FAIL  docker does not answer. This lane runs each provider in a container."
		exit 1
	fi

	echo "SKIP  docker does not answer, and this lane runs each provider in a container."
	exit 0
fi

WORK=$(mktemp -d)
CORE_PID=
SERVICES=

cleanup() {
	if [ -n "$CORE_PID" ]; then
		kill "$CORE_PID" 2> /dev/null
		wait "$CORE_PID" 2> /dev/null
	fi
	if [ -n "$SERVICES" ]; then
		# shellcheck disable=SC2086 # the services are separate words
		$COMPOSE rm -sfv $SERVICES > /dev/null 2>&1
	fi
	rm -rf "$WORK"
}
trap cleanup EXIT INT TERM

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

echo "=== The sign-in at real OpenID Connect providers ==="

rm -rf "$WORK_ROOT"
mkdir -p "$WORK_ROOT"

# ── The core, with the web app in it ─────────────────────────────────
CORE=$WORK_ROOT/prusa-connect$(go env GOEXE)

step "the web app builds into the core" make embed-app
step "the core builds with the web app and the diagnostics" \
	go build -tags "full diagnostics" -o "$CORE" ./cmd/prusa-connect

PASSED=
for provider in $PROVIDERS; do
	check_provider "$provider"
done

if [ -z "$PASSED" ]; then
	echo "FAIL  no provider ran"
	exit 1
fi

echo "OK    the sign-in passed at:$PASSED"

}

# ── One provider ─────────────────────────────────────────────────────
#
# Each provider sets the services that run it, the issuer, the client, and the
# two people. The subjects are read from the provider where it chooses them.
describe_provider() {
	case $1 in
		keycloak)
			SERVICES=keycloak-fixture
			ISSUER=http://127.0.0.1:${PCL_KEYCLOAK_FIXTURE_PORT:-58180}/realms/pcl
			CLIENT_SECRET=pcl-keycloak-fixture-secret
			ID_TOKEN_ALG=RS256
			;;
		authentik)
			SERVICES="authentik-postgres-fixture authentik-worker-fixture authentik-fixture"
			AUTHENTIK=http://127.0.0.1:${PCL_AUTHENTIK_FIXTURE_PORT:-58190}
			ISSUER=$AUTHENTIK/application/o/prusa-connect/
			CLIENT_SECRET=pcl-authentik-fixture-secret
			# No signing key, which is authentik's default for a provider.
			ID_TOKEN_ALG=HS256
			;;
		authelia)
			SERVICES=authelia-fixture
			ISSUER=https://127.0.0.1:${PCL_AUTHELIA_FIXTURE_PORT:-58191}
			CLIENT_SECRET=pcl-authelia-fixture-secret
			# The client is set to sign its ID tokens with a P-256 key.
			ID_TOKEN_ALG=ES256
			;;
		*)
			echo "FAIL  $1 is not a provider this lane knows. It knows: $ALL_PROVIDERS"
			exit 1
			;;
	esac

	CLIENT_ID=prusa-connect
	# The certificate the core trusts the issuer's TLS by, for a provider that
	# serves TLS on a certificate of its own. Empty for the others.
	ISSUER_CA=
	ADMIN_USER=pcl-admin
	ADMIN_PASSWORD=pcl-admin-password
	ADMIN_NAME="Ada Admin"
	VIEWER_USER=pcl-viewer
	VIEWER_PASSWORD=pcl-viewer-password
	VIEWER_NAME="Vera Viewer"
}

# The subject each person has at the provider. Keycloak takes the ids the
# realm file gives them. authentik's default subject is a hash of the person
# and the install, so it is read over authentik's API, from the preview of the
# ID token the provider would issue.
read_subjects() {
	case $1 in
		keycloak)
			ADMIN_SUB=6c1f0b2a-5d4e-4f8a-9b3c-2e1d0a9f8e71
			VIEWER_SUB=9a2e4c6b-1f3d-4b5a-8c7e-0d9f1e2a3b4c
			;;
		authentik)
			ADMIN_SUB=$(authentik_subject "$ADMIN_USER") || return 1
			VIEWER_SUB=$(authentik_subject "$VIEWER_USER") || return 1
			;;
		authelia)
			ADMIN_SUB=3f6d2a1b-8c4e-4b7a-9e2d-5a1c0b9f8e72
			VIEWER_SUB=7b1e9d3c-2a5f-4c8b-a6e4-0f3d2c1b9a85
			authelia_identifier "$ADMIN_USER" "$ADMIN_SUB" || return 1
			authelia_identifier "$VIEWER_USER" "$VIEWER_SUB" || return 1
			;;
	esac

	[ -n "$ADMIN_SUB" ] && [ -n "$VIEWER_SUB" ]
}

authentik_subject() {
	user=$(authentik_api "core/users/?username=$1" pk) || return 1
	provider=$(authentik_api "providers/oauth2/?name=$CLIENT_ID" pk) || return 1
	authentik_api "providers/oauth2/$provider/preview_user/?for_user=$user" sub
}

# One field of an answer from authentik's API: of the only result of a
# listing, or of the token an ID token preview carries.
authentik_api() {
	curl -fsS -H "Authorization: Bearer pcl-authentik-fixture-token" \
		"$AUTHENTIK/api/v3/$1" |
		node -e '
			let text = "";
			process.stdin.on("data", (chunk) => (text += chunk));
			process.stdin.on("end", () => {
				const answer = JSON.parse(text);
				const found = answer.preview ?? (answer.results?.length === 1 ? answer.results[0] : undefined);
				const value = found?.[process.argv[1]];
				if (value === undefined) process.exit(1);
				console.log(value);
			});
		' "$2"
}

# Authelia makes a person's subject at their first sign-in. Its own command
# writes it ahead, which is what an administrator does who has to name the
# subject in the wizard before anybody has signed in.
authelia_identifier() {
	$COMPOSE exec -T authelia-fixture authelia storage user identifiers add "$1" \
		--identifier "$2" --config /config/configuration.yml
}

# Authelia serves TLS on a certificate its image made. The core is handed it
# as the one authority it trusts, through SSL_CERT_FILE, which Go reads on
# Linux and not on macOS or Windows.
trust_issuer() {
	case $1 in
		authelia)
			ISSUER_CA=$(pwd)/$WORK_ROOT/authelia.crt
			$COMPOSE cp authelia-fixture:/config/tls/public.crt "$ISSUER_CA"
			;;
	esac
}

start_provider() {
	# shellcheck disable=SC2086 # the services are separate words
	$COMPOSE up -d --build --wait --no-deps $SERVICES
}

# A provider is ready when its discovery document answers. authentik applies
# its blueprint after the server is healthy, so the document can lag the
# health check by a few seconds.
provider_ready() {
	waited=0
	while [ "$waited" -lt 180 ]; do
		if curl -fsS ${ISSUER_CA:+--cacert "$ISSUER_CA"} \
			"${ISSUER%/}/.well-known/openid-configuration" > /dev/null 2>&1; then
			return 0
		fi

		waited=$((waited + 1))
		sleep 1
	done

	return 1
}

start_core() {
	CORE_DIR=$WORK_ROOT/$1
	rm -rf "$CORE_DIR"
	mkdir -p "$CORE_DIR"
	CORE_BIN=$(cd "$(dirname "$CORE")" && pwd)/$(basename "$CORE")

	(
		cd "$CORE_DIR" || exit 1
		if [ -n "$ISSUER_CA" ]; then
			SSL_CERT_FILE=$ISSUER_CA
			export SSL_CERT_FILE
		fi
		exec "$CORE_BIN" -addr "$CORE_ADDR"
	) > "$CORE_DIR/server.log" 2>&1 &
	CORE_PID=$!
}

# The core answers, and has printed the setup code a person types.
core_ready() {
	waited=0
	while [ "$waited" -lt 120 ]; do
		SETUP_CODE=$(sed -n 's/.*setup_code=\([A-Z0-9-]*\).*/\1/p' "$CORE_DIR/server.log" | tail -1)
		if [ -n "$SETUP_CODE" ] && curl -fsS "$BASE_URL/api/v1/health" > /dev/null 2>&1; then
			return 0
		fi

		if ! kill -0 "$CORE_PID" 2> /dev/null; then
			echo "the core ended before it served" >&2
			cat "$CORE_DIR/server.log" >&2

			return 1
		fi

		waited=$((waited + 1))
		sleep 1
	done

	cat "$CORE_DIR/server.log" >&2

	return 1
}

# Every page asks who it is before anybody signs in. That is no warning, and
# a credential that was not there is not a malformed one.
no_warning_for_no_credential() {
	! grep -E 'level=WARN .*(no credential presented|malformed token)' "$CORE_DIR/server.log"
}

stop_core() {
	kill "$CORE_PID" 2> /dev/null
	wait "$CORE_PID" 2> /dev/null
	CORE_PID=
}

stop_provider() {
	# shellcheck disable=SC2086 # the services are separate words
	$COMPOSE rm -sfv $SERVICES > /dev/null 2>&1
	SERVICES=
}

check_provider() {
	echo "--- $1"
	describe_provider "$1"

	if [ "$1" = authelia ] && [ "$(uname -s)" != Linux ]; then
		echo "SKIP  authelia: the core trusts its certificate through SSL_CERT_FILE, which Go reads only on Linux"
		return 0
	fi

	step "$1 starts with a client and two people" start_provider
	if [ "$1" = authelia ]; then
		step "$1 hands over the certificate it serves" trust_issuer "$1"
	fi
	step "$1 publishes its discovery document" provider_ready
	step "$1 names the subject of each person" read_subjects "$1"
	echo "    the administrator is $ADMIN_SUB"

	start_core "$1"
	step "a core that has never run prints a setup code" core_ready

	STEPS=$((STEPS + 1))
	printf '%2d. %s...\n' "$STEPS" "the sign-in at $1 runs in a browser"

	if ! (
		cd "$APP_DIR" &&
			PCL_CORE_URL="$BASE_URL" \
				PCL_SETUP_CODE="$SETUP_CODE" \
				PCL_OIDC_PROVIDER="$1" \
				PCL_OIDC_ISSUER="$ISSUER" \
				PCL_OIDC_CLIENT_ID="$CLIENT_ID" \
				PCL_OIDC_CLIENT_SECRET="$CLIENT_SECRET" \
				PCL_OIDC_ADMIN_USER="$ADMIN_USER" \
				PCL_OIDC_ADMIN_PASSWORD="$ADMIN_PASSWORD" \
				PCL_OIDC_ADMIN_SUB="$ADMIN_SUB" \
				PCL_OIDC_ADMIN_NAME="$ADMIN_NAME" \
				PCL_OIDC_ADMIN_EMAIL="$ADMIN_USER@example.com" \
				PCL_OIDC_VIEWER_USER="$VIEWER_USER" \
				PCL_OIDC_VIEWER_PASSWORD="$VIEWER_PASSWORD" \
				PCL_OIDC_VIEWER_SUB="$VIEWER_SUB" \
				PCL_OIDC_VIEWER_NAME="$VIEWER_NAME" \
				PCL_OIDC_VIEWER_EMAIL="$VIEWER_USER@example.com" \
				pnpm exec vitest run --config test/oidc/vitest.config.ts
	); then
		echo "FAIL  the sign-in at $1 did not finish"
		echo "--- the core's log"
		cat "$CORE_DIR/server.log"
		echo "--- the provider's log"
		# shellcheck disable=SC2086 # the services are separate words
		$COMPOSE logs --tail 200 $SERVICES
		exit 1
	fi

	step "the core checked ID tokens signed with $ID_TOKEN_ALG" \
		grep -q "id_token_alg=$ID_TOKEN_ALG" "$CORE_DIR/server.log"
	step "the core warned about no request that carried no credential" \
		no_warning_for_no_credential

	stop_core
	stop_provider
	PASSED="$PASSED $1"
	echo "OK    $1"
}

main "$@"
