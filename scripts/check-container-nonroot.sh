#!/usr/bin/env bash
# check-container-nonroot.sh
#
# Verifies that a built image runs as a non-root user AND that the runtime user
# can actually write everywhere the server writes. Used by `make docker-check`
# and by the "Docker / Hardened image" CI job.
#
# Why both halves:
#   A container compromise on uid 0 has full Linux capabilities inside the
#   namespace and, depending on the host config, can escape it. So the image
#   declares a USER. But a USER directive that nobody proved against the write
#   paths is how "hardened" images end up crash-looping on first boot instead:
#   the server opens sqlite databases under /data, stages printer streams under
#   /files, keeps operator uploads under /storage and spills large multipart
#   uploads to /tmp. If uid 10001 cannot write one of those, the image is broken
#   in a way no static check would have caught.
#
#   Docker seeds an empty NAMED volume from the image path and carries the
#   image's ownership across with it, which is why proving the paths in the bare
#   image is enough for the compose stack. A BIND mount gets no such treatment -
#   the host directory keeps its own ownership and the operator must chown it.
#
# Checks performed:
#   1. image config has a USER directive, and it is not root / 0 / empty
#   2. running the image actually yields a non-zero uid
#      (catches a USER name that resolves to uid 0)
#   3. every directory the Dockerfile creates is owned by the runtime uid and
#      writable by it, over the set read out of the Dockerfile itself
#   4. /tmp is writable, which is a different claim and says so
#   5. a read-only root filesystem still leaves /config/config.d writable when
#      it carries a volume, and refuses the write when it does not
#
# exit 0: image runs as non-root and can write where it needs to
# exit 1: image runs as root, or a runtime directory is not writable
# exit 2: usage error

set -euo pipefail

# Git Bash on Windows rewrites anything that looks like an absolute POSIX path
# into a Windows one before exec, so `--entrypoint /bin/sh` reaches docker as
# `C:/Program Files/Git/usr/bin/sh` and the container fails to start. Harmless
# on the Linux CI runner, required for anyone running this locally on Windows.
export MSYS_NO_PATHCONV=1

# The directories whose ownership this image is responsible for, read out of the
# Dockerfile's `install -d` line rather than written here. A list typed into a
# script drifts from the file it claims to follow, and a comment asking a reader
# to keep two lists in step is not a check.
#
# /config/config.d is where the core writes the machine-owned resource overlay
# and the authorization graph, beside the operator's read-only file. It is the
# one the read-only root filesystem catches: without it the core stops at
# startup with `mkdir config/config.d: read-only file system`.
DOCKERFILE="$(cd "$(dirname "$0")/.." && pwd)/docker/server/Dockerfile"

if [ ! -f "$DOCKERFILE" ]; then
  echo "error: $DOCKERFILE is missing, so the runtime directories cannot be read." >&2
  exit 2
fi

OWNED_DIRS="$(sed -n 's/^[[:space:]]*install -d .*-m [0-7]* //p' "$DOCKERFILE" | tr -s ' ')"
OWNED_COUNT="$(printf '%s' "$OWNED_DIRS" | tr ' ' '\n' | grep -c '^/' || true)"

# The floor. An `install -d` line this pattern stopped matching leaves an empty
# set, and a loop over an empty set reports a tick over nothing. It is the
# number the Dockerfile creates, so a directory dropped there fails here, and
# internal/arch/container_test.go holds the two numbers equal.
OWNED_FLOOR=4

if [ "$OWNED_COUNT" -lt "$OWNED_FLOOR" ]; then
  echo "FAIL: read $OWNED_COUNT runtime directories out of $DOCKERFILE, under the floor of $OWNED_FLOOR." >&2
  echo "  Either the \`install -d\` line moved, or the pattern above stopped matching it." >&2
  echo "  A list this script could not read is a check it cannot run." >&2
  exit 1
fi

# /tmp is a separate claim and is deliberately not in the set above. The base
# image provides it at uid 0 mode 1777, so a write there succeeds for the sticky
# bit and not because the runtime user owns it. The deployment gives it a volume
# of its own (docker/server/server.yml), because gin spills a multipart upload
# over 32 MiB to os.TempDir(). What has to be true is that the runtime user can
# write there, and that is what is checked.
SHARED_DIRS="/tmp"

# The one path the read-only root filesystem decides, from the header above.
READONLY_PROBE="/config/config.d"

IMAGE="${1:-}"
if [ -z "$IMAGE" ]; then
  echo "usage: $0 <image-ref>" >&2
  echo "example: $0 prusa-connect:ci" >&2
  exit 2
fi

if ! command -v docker >/dev/null 2>&1; then
  echo "error: docker not on PATH." >&2
  exit 2
fi

echo "── checking $IMAGE runs as non-root ──"

# Pull only if it is not already local; a locally built CI image never is.
if ! docker image inspect "$IMAGE" >/dev/null 2>&1; then
  docker pull --quiet "$IMAGE" >/dev/null
fi

# 1. static check: USER directive in image config
CONFIGURED_USER="$(docker image inspect --format '{{.Config.User}}' "$IMAGE")"

if [ -z "$CONFIGURED_USER" ]; then
  cat >&2 <<'EOF'
FAIL: no USER directive in the image. It defaults to root.
  fix: add `USER prusa` near the bottom of the Dockerfile, after the
       `install -d -o prusa -g prusa` that creates the runtime directories.
EOF
  exit 1
fi

case "$CONFIGURED_USER" in
  root | 0 | 0:* | root:*)
    echo "FAIL: image is explicitly configured to run as root ($CONFIGURED_USER)" >&2
    echo "  fix: change the USER directive to the non-root runtime user." >&2
    exit 1
    ;;
esac

echo "  config USER: $CONFIGURED_USER (ok)"

# 2. runtime check: what uid does the process actually get?
ACTUAL_UID="$(docker run --rm --entrypoint /bin/sh "$IMAGE" -c 'id -u' 2>/dev/null || true)"

if ! echo "$ACTUAL_UID" | grep -qE '^[0-9]+$'; then
  echo "FAIL: could not read the runtime uid from the image." >&2
  echo "  the runtime stage is debian-slim and has /bin/sh; if that changed," >&2
  echo "  update this check rather than dropping it." >&2
  exit 1
fi

if [ "$ACTUAL_UID" = "0" ]; then
  cat >&2 <<EOF
FAIL: container runs as uid 0 (root) despite USER=$CONFIGURED_USER.
  This usually means USER names a user that resolves to uid 0 in /etc/passwd.
  inspect with: docker run --rm --entrypoint cat $IMAGE /etc/passwd
EOF
  exit 1
fi

echo "  runtime uid: $ACTUAL_UID (ok)"

FAILED=0

# The two probes, run in one container each so a slow docker is not paid for
# per directory.
owner_of() { docker run --rm --entrypoint /bin/sh "$IMAGE" -c "stat -c %u $1" 2>/dev/null || true; }
detail_of() { docker run --rm --entrypoint /bin/sh "$IMAGE" -c "ls -ld $1 2>/dev/null || echo missing" || true; }

# 3. the directories the image owns. Writability alone is the weaker claim: a
# world-writable directory passes it while the ownership Docker carries into a
# named volume is wrong. Both are asked.
echo "── checking the runtime user owns and can write$OWNED_DIRS ──"

for dir in $OWNED_DIRS; do
  OWNER="$(owner_of "$dir")"

  if [ "$OWNER" != "$ACTUAL_UID" ]; then
    echo "FAIL: $dir is owned by uid ${OWNER:-unreadable}, not by the runtime uid $ACTUAL_UID" >&2
    echo "  current: $(detail_of "$dir")" >&2
    echo "  Docker seeds a named volume from the image path and carries this ownership" >&2
    echo "  with it, so the deployment inherits whatever is wrong here." >&2
    FAILED=$((FAILED + 1))

    continue
  fi

  if docker run --rm --entrypoint /bin/sh "$IMAGE" \
      -c "touch $dir/.perm-test && rm $dir/.perm-test" >/dev/null 2>&1; then
    echo "  $dir: owned by $ACTUAL_UID and writable (ok)"
  else
    echo "FAIL: uid $ACTUAL_UID owns $dir and cannot write to it" >&2
    echo "  current: $(detail_of "$dir")" >&2
    FAILED=$((FAILED + 1))
  fi
done

# 4. the directories the base image provides. The claim is writability and
# nothing more, and the line says which claim it is.
echo "── checking the runtime user can write$SHARED_DIRS ──"

for dir in $SHARED_DIRS; do
  if docker run --rm --entrypoint /bin/sh "$IMAGE" \
      -c "touch $dir/.perm-test && rm $dir/.perm-test" >/dev/null 2>&1; then
    echo "  $dir: writable by $ACTUAL_UID, owned by uid $(owner_of "$dir") (ok)"
  else
    echo "FAIL: uid $ACTUAL_UID cannot write to $dir" >&2
    echo "  current: $(detail_of "$dir")" >&2
    FAILED=$((FAILED + 1))
  fi
done

# 5. the read-only root filesystem, which is how the deployment runs
# (docker/server/server.yml). Every probe above runs writable, so none of them
# reaches the failure the header names.
#
# Both directions are asked, because only the pair proves the probe ran
# read-only at all. With a volume on the path the write has to succeed, and
# without one it has to fail. A run that could not make the root read-only
# would pass the first and fail the second.
echo "── checking $READONLY_PROBE under a read-only root ──"

if docker run --rm --read-only -v "prusa_connect_probe_$$:$READONLY_PROBE" \
    --entrypoint /bin/sh "$IMAGE" \
    -c "touch $READONLY_PROBE/.perm-test && rm $READONLY_PROBE/.perm-test" >/dev/null 2>&1; then
  echo "  $READONLY_PROBE: writable through its volume (ok)"
else
  echo "FAIL: $READONLY_PROBE is not writable under a read-only root with a volume on it" >&2
  echo "  This is how docker/server/server.yml runs the server, and the core stops at" >&2
  echo "  startup with \`mkdir config/config.d: read-only file system\`." >&2
  FAILED=$((FAILED + 1))
fi

docker volume rm "prusa_connect_probe_$$" >/dev/null 2>&1 || true

if docker run --rm --read-only --entrypoint /bin/sh "$IMAGE" \
    -c "touch $READONLY_PROBE/.perm-test && rm $READONLY_PROBE/.perm-test" >/dev/null 2>&1; then
  echo "FAIL: $READONLY_PROBE took a write under a read-only root with no volume on it." >&2
  echo "  The probe above therefore proves nothing: this run was not read-only." >&2
  FAILED=$((FAILED + 1))
else
  echo "  $READONLY_PROBE: refused with no volume, so the probe above ran read-only (ok)"
fi

if [ "$FAILED" -gt 0 ]; then
  cat >&2 <<'EOF'
  fix: create the directory in the Dockerfile with the runtime user as owner,
       e.g. `install -d -o prusa -g prusa -m 0750 /data`. Do not chmod 777.
EOF
  exit 1
fi

echo "PASS: $IMAGE runs as uid $ACTUAL_UID, owns what it creates, and writes under a read-only root"
