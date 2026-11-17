#!/usr/bin/env bash
# check-container-privileged-port.sh
#
# Holds the claim docker/server/server.yml and docker/server/Dockerfile both
# make in prose: this image binds a port below 1024 as uid 10001 with the
# capability set empty, so it needs no NET_BIND_SERVICE and no file capability
# on the binary. Used by `make docker-check` and by the "Docker / Hardened
# image" CI job.
#
# Why this runs the image rather than the compose stack:
#   Binding is a kernel question and not a composition one. No connector asks
#   for a listener by default, so printers dial the API listener and
#   docker/server/server.local.yml publishes 8080 alone. No process in the
#   composition binds a port below 1024, so a curl at :80 against the stack
#   reaches a closed port and reports the runtime nothing.
#
#   The image is run directly with the API listener moved to :80 instead. That
#   is the same syscall on the same uid with the same empty capability set, and
#   it is asked of the kernel rather than of a deployment's port map.
#
# Checks performed:
#   1. the container comes up with --cap-drop ALL and --read-only, on :80
#   2. the process inside it is uid 10001, not root
#   3. :80 answers - a served status code, not curl exit 7 on a closed port
#   4. the device route the printers dial is one of the paths it answers
#
# exit 0: a non-root process with no capabilities served a privileged port
# exit 1: it did not
# exit 2: usage error

set -euo pipefail

# See check-container-probes.sh: Git Bash rewrites absolute POSIX paths before
# exec, so an in-container path reaches docker as a Windows one.
export MSYS_NO_PATHCONV=1

IMAGE="${1:-}"
if [ -z "$IMAGE" ]; then
  echo "usage: $0 <image-ref> [host-port]" >&2
  echo "example: $0 prusa-connect:local 18081" >&2
  exit 2
fi

PORT="${2:-18081}"
NAME="prusa-connect-privport-$$"

# The uid the Dockerfile creates and the image runs as.
EXPECTED_UID=10001

# The privileged port. Below 1024 is the whole point: the traditional boundary
# is what a container runtime's ip_unprivileged_port_start=0 removes, and this
# check is what notices if a runtime ever stops defaulting it that way.
BOUND_PORT=80

# The path a printer dials. A plain GET is not a websocket upgrade, so it is
# refused with a 4xx - which is still an answer from a bound listener. A closed
# port gives curl exit 7 and no status code at all, which is the failure this
# is looking for.
DEVICE_PATH="/p/ws"

START_BUDGET=60

cleanup() {
  docker rm -f "$NAME" >/dev/null 2>&1 || true
  docker volume rm -f "${NAME}-data" "${NAME}-config" >/dev/null 2>&1 || true
}
trap cleanup EXIT

echo "── binding :${BOUND_PORT} in $IMAGE as a non-root process with no capabilities ──"

docker run -d --name "$NAME" \
  -p "127.0.0.1:${PORT}:${BOUND_PORT}" \
  -v "${NAME}-data:/data" \
  -v "${NAME}-config:/config/config.d" \
  --read-only \
  --cap-drop ALL \
  --security-opt no-new-privileges:true \
  "$IMAGE" core --addr ":${BOUND_PORT}" >/dev/null

READY=0
for _ in $(seq 1 "$START_BUDGET"); do
  if curl -fsS "http://127.0.0.1:${PORT}/api/v1/health" >/dev/null 2>&1; then
    READY=1
    break
  fi

  if [ "$(docker inspect --format '{{.State.Running}}' "$NAME" 2>/dev/null || echo false)" != "true" ]; then
    echo "FAIL: the container exited rather than binding :${BOUND_PORT}." >&2
    echo "  A 'permission denied' below is the runtime no longer defaulting" >&2
    echo "  net.ipv4.ip_unprivileged_port_start to 0, which is the case this gate exists for." >&2
    docker logs "$NAME" 2>&1 | tail -20 >&2
    exit 1
  fi

  sleep 1
done

if [ "$READY" != "1" ]; then
  echo "FAIL: nothing answered on :${BOUND_PORT} within ${START_BUDGET}s" >&2
  docker logs "$NAME" 2>&1 | tail -20 >&2
  exit 1
fi

echo "── the uid that holds it ──"

ID_LINE="$(docker exec "$NAME" id)"
echo "  $ID_LINE"

case "$ID_LINE" in
  "uid=${EXPECTED_UID}("*) ;;
  *)
    echo "FAIL: the process holding :${BOUND_PORT} is not uid ${EXPECTED_UID}, so this proves nothing" >&2
    exit 1
    ;;
esac

echo "── the route a printer dials ──"

# The status is written after the body and read off the last line, so nothing
# has to name a file to discard the body into. An empty value means curl never
# got an answer, which is a closed port.
CODE="$(curl -s -w '\n%{http_code}' "http://127.0.0.1:${PORT}${DEVICE_PATH}" 2>/dev/null | tail -n 1)"
CODE="${CODE:-000}"

echo "  ${DEVICE_PATH} -> HTTP ${CODE}"

if [ "$CODE" = "000" ]; then
  echo "FAIL: nothing is served on :${BOUND_PORT}${DEVICE_PATH}" >&2
  docker logs "$NAME" 2>&1 | tail -20 >&2
  exit 1
fi

echo "PASS: uid ${EXPECTED_UID} with no capabilities bound :${BOUND_PORT} and served ${DEVICE_PATH}"
