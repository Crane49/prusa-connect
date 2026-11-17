# syntax=docker/dockerfile:1

# The race detector's image: this tree, copied in at build, on the Go that
# go.mod pins, run on Linux the way the CI runner runs it.
#
# The tree is copied rather than mounted. The Docker Desktop on the machine
# this repository is developed on refuses every bind mount with an
# `input/output error` on the host path, so a run that mounted the checkout
# never started there. Copying 50 MB takes seconds, and the layer above it is
# reused until go.mod changes.
#
# Build from the repository root, so the whole module is in the context:
#
#   docker compose -f docker-compose.local.yml build race
#
# Dockerfile.dockerignore beside this file names what stays out of the
# context. The root .dockerignore is not used here: it drops .github, and
# the gates under internal/arch read the workflow files.

# Digest-pinned. golang:1.26.11-trixie at the time of pinning. The pin is for
# the operating system layer only, because the Go version is read out of
# go.mod below and the toolchain that runs is that one, whatever the tag says.
FROM golang:1.26.11-trixie@sha256:fa340cb59ebeb1d53ac1cccfc334201d938fe5f3458003cccb767096d8e032b8

# A fixed uid rather than root, so a test that escapes its sandbox holds no
# capability, and so the two cache volumes are seeded from paths this uid owns.
# Docker fills an empty named volume from the image path and copies the
# ownership with it, which is why every cache directory is created here.
ARG RACE_UID=10001
ARG RACE_GID=10001
RUN set -eux; \
    groupadd --gid "${RACE_GID}" race; \
    useradd --uid "${RACE_UID}" --gid "${RACE_GID}" --no-create-home --shell /usr/sbin/nologin race; \
    install -d -o race -g race -m 0755 /home/race /home/race/.cache/go-build /go/pkg/mod /src

ENV HOME=/home/race GOMODCACHE=/go/pkg/mod GOCACHE=/home/race/.cache/go-build GOTELEMETRY=off
# Two, the shape every race this branch lost a run to was caught at
# (.github/workflows/nightly.yml). The go command reads the same value for its
# own parallelism, so the run compiles two packages at a time and the memory
# bound in race.yml holds.
ENV GOMAXPROCS=2

WORKDIR /src

# The manifests first, so the toolchain layer is reused until go.mod changes.
# sdk/go.mod has to exist before any module-graph command runs, because go.mod
# replaces the sdk module with that directory.
COPY --chown=race:race go.mod go.sum ./
COPY --chown=race:race sdk/go.mod ./sdk/

USER race

# The Go that runs is the one go.mod pins, read here at build time. GOTOOLCHAIN
# names an exact version, so a base image newer than go.mod does not run the
# tests on a Go the repository never chose. When the two differ the toolchain
# is downloaded into the module cache, which is the volume the run mounts, so
# it is fetched once.
RUN GOTOOLCHAIN="go$(sed -n 's/^go //p' go.mod)" go version

COPY --chown=race:race --chmod=0755 docker/race/race.sh /usr/local/bin/race
COPY --chown=race:race . .

ENTRYPOINT ["/usr/local/bin/race"]
