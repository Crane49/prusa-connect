#!/usr/bin/env bash
# check-container-probes.sh
#
# Runs a built image the way a deployment runs it and asks it every question a
# deployment asks. Used by `make docker-check` and by the "Docker / Hardened
# image" CI job.
#
# internal/arch/container_test.go reads docker/server/Dockerfile,
# docker/server/server.yml and compose/compose.go, and refuses a disagreement
# about the command the image runs. This script asks the running container
# over HTTP.
#
# Checks performed:
#   1. the container starts under read_only with the capability set empty
#   2. GET /api/v1/health answers 200          liveness
#   3. GET /api/v1/readiness answers 200       readiness
#   4. GET /openapi.json answers 200           the published contract
#   5. GET /docs answers 200                   the reference that renders it
#   6. the binary's own probe exits 0 inside the container
#   7. SIGTERM stops it cleanly, inside the shutdown budget
#
# exit 0: every probe answered
# exit 1: one did not
# exit 2: usage error

set -euo pipefail

# Git Bash on Windows rewrites anything that looks like an absolute POSIX path
# into a Windows one before exec, so `/prusa_connect` reaches docker as
# `C:/Program Files/Git/prusa_connect`. Harmless on the Linux CI runner,
# required for anyone running this locally on Windows.
#
# It has one consequence worth writing down: with the rewrite off, `curl -o
# /dev/null` reaches a native curl that has no /dev/null and fails with exit 23,
# writing nothing and saying nothing about why. Every curl below discards its
# body through the shell instead.
export MSYS_NO_PATHCONV=1

IMAGE="${1:-}"
if [ -z "$IMAGE" ]; then
  echo "usage: $0 <image-ref> [host-port]" >&2
  echo "example: $0 prusa-connect:local 18080" >&2
  exit 2
fi

PORT="${2:-18080}"
NAME="prusa-connect-probes-$$"

# The paths a deployment reads, and what each one is for.
PATHS="/api/v1/health /api/v1/readiness /openapi.json /docs"

# How long the core is given to bind and answer. A cold start opens the
# authorization graph and starts every declared resource.
START_BUDGET=60

# What SIGTERM is allowed to take. core.shutdown_timeout is 15s and the drain
# runs inside it, so anything past this is a container the orchestrator will
# eventually kill.
STOP_BUDGET=20

cleanup() {
  docker rm -f "$NAME" >/dev/null 2>&1 || true
  docker volume rm -f "${NAME}-data" "${NAME}-config" >/dev/null 2>&1 || true
}
trap cleanup EXIT

echo "── starting $IMAGE the way a deployment runs it ──"

docker run -d --name "$NAME" \
  -p "127.0.0.1:${PORT}:8080" \
  -v "${NAME}-data:/data" \
  -v "${NAME}-config:/config/config.d" \
  --read-only \
  --cap-drop ALL \
  --security-opt no-new-privileges:true \
  "$IMAGE" >/dev/null

# No configuration file is mounted, deliberately: an instance a fleet controller
# spawns with nothing but an image has to come up on defaults, and a container
# that needs a file to start cannot be spawned by one.
echo "  no config file mounted: the image must come up on defaults"

echo "── waiting for the core to answer ──"

READY=0
for _ in $(seq 1 "$START_BUDGET"); do
  if curl -fsS "http://127.0.0.1:${PORT}/api/v1/health" >/dev/null 2>&1; then
    READY=1
    break
  fi

  if [ "$(docker inspect --format '{{.State.Running}}' "$NAME" 2>/dev/null || echo false)" != "true" ]; then
    echo "FAIL: the container exited before it answered" >&2
    docker logs "$NAME" 2>&1 | tail -20 >&2
    exit 1
  fi

  sleep 1
done

if [ "$READY" != "1" ]; then
  echo "FAIL: nothing answered on /api/v1/health within ${START_BUDGET}s" >&2
  docker logs "$NAME" 2>&1 | tail -20 >&2
  exit 1
fi

echo "── probing the paths a deployment reads ──"

FAILED=0
for path in $PATHS; do
  # The status code is written after the body and read off the last line, so
  # nothing has to name a file to throw the body into.
  CODE="$(curl -s -w '\n%{http_code}' "http://127.0.0.1:${PORT}${path}" 2>/dev/null | tail -n 1)"
  CODE="${CODE:-000}"
  if [ "$CODE" = "200" ]; then
    printf '  %-20s %s\n' "$path" "$CODE"
  else
    printf '  %-20s %s  <- expected 200\n' "$path" "$CODE" >&2
    FAILED=$((FAILED + 1))
  fi
done

if [ "$FAILED" -gt 0 ]; then
  cat >&2 <<'EOF'
FAIL: the container is not serving the core.
  A 404 on every path above is the shipping server answering: the image's
  ENTRYPOINT is the binary and its default subcommand is that server, so the
  Dockerfile needs `CMD ["core"]`. See internal/arch/container_test.go.
EOF
  docker logs "$NAME" 2>&1 | tail -20 >&2
  exit 1
fi

echo "── the binary's own probe, which is what HEALTHCHECK runs ──"

if docker exec "$NAME" /prusa_connect core healthcheck; then
  echo "  core healthcheck: exit 0"
else
  echo "FAIL: the binary's own probe does not agree with the wire" >&2
  exit 1
fi

echo "── SIGTERM ──"

STARTED="$(date +%s)"
docker stop -t "$STOP_BUDGET" "$NAME" >/dev/null
ELAPSED=$(($(date +%s) - STARTED))

CODE="$(docker inspect --format '{{.State.ExitCode}}' "$NAME")"

if [ "$CODE" != "0" ]; then
  echo "FAIL: SIGTERM left exit code $CODE, so the shutdown was not clean" >&2
  docker logs "$NAME" 2>&1 | tail -20 >&2
  exit 1
fi

if [ "$ELAPSED" -ge "$STOP_BUDGET" ]; then
  echo "FAIL: the container took ${ELAPSED}s to stop, so docker killed it" >&2
  exit 1
fi

echo "  stopped in ${ELAPSED}s, exit 0"
echo "PASS: $IMAGE serves the core, answers every published probe and stops on SIGTERM"
