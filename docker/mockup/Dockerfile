# syntax=docker/dockerfile:1

# Four real Prusa Connects with fake printers, for the Fractal mockup.
# One image runs every role: the settings file the entry point installs decides
# which server the container is.
#
# It builds cmd/demo and not cmd/prusa-connect, because the printer
# simulator is reachable from the demo alone. cmd/demo's package comment carries
# the measurement: linking the simulator costs 262,144 bytes and every margin in
# scripts/check-binary-budget.sh is 261,982 bytes, so putting a simulate flag on
# the shipping binary would spend the whole margin of three of the six rows.
# Building this image on the demo costs nothing, because this image is not one
# of those rows.
#
# The shipping binary comes along for two things the demo has no subcommand
# for: the health probe, and `module add`, which installs the Fractal module.

# Go toolchain version, kept in step with the `go` directive in go.mod.
# `scripts/check-go-version.sh` enforces it.
ARG GO_VERSION=1.26.11

# The web app every server of the mockup serves at /, built the way
# docker/server/Dockerfile builds it: the SDK, the component library and the
# Fractal module's client from this checkout, then the app over them.
#
# Digest-pinned to the same node image docker/server/Dockerfile names.
FROM node:24-alpine@sha256:06d7adf3e0ef61a13f1d28682726366f97a15aa1888c934d3ecec86c33918b53 AS app-builder
ENV PNPM_HOME=/pnpm
ENV PATH="$PNPM_HOME:$PATH"
ENV HUSKY=0
ENV CI=true
RUN corepack enable
WORKDIR /app
COPY packages/standards ./packages/standards
COPY packages/typescript ./packages/typescript
RUN cd packages/typescript && pnpm install --frozen-lockfile && pnpm run build
COPY packages/svelte ./packages/svelte
COPY docs/errors/error-codes.json ./docs/errors/error-codes.json
RUN cd packages/svelte && pnpm install --frozen-lockfile --ignore-scripts && pnpm run build
COPY packages/fractal/client ./packages/fractal/client
RUN cd packages/fractal/client && pnpm install --frozen-lockfile && pnpm run build
COPY frontend ./frontend
RUN cd frontend && pnpm install --frozen-lockfile && pnpm run build && test -f dist/index.html

# Digest-pinned to the same golang build docker/race/Dockerfile names. A
# digest wins over the tag, so scripts/check-go-version.sh requires this digest
# to equal the race image's, whose tag must name the release go.mod pins. A Go
# bump therefore moves the digest here too, or the check fails.
FROM golang:${GO_VERSION}-trixie@sha256:fa340cb59ebeb1d53ac1cccfc334201d938fe5f3458003cccb767096d8e032b8 AS builder

WORKDIR /app

COPY go.* ./
# The nested sdk module is resolved through a local replace in go.mod, so its
# manifest has to exist before any module-graph command runs.
COPY sdk/go.mod ./sdk/
RUN go mod download

COPY . ./

# The app goes where //go:embed reads it, and the full tag embeds it, so each
# server serves the app at / on its own port. Each text file is replaced with
# a gzip copy, as in the server image.
COPY --from=app-builder /app/frontend/dist ./internal/transport/http/httpapi/appdist
RUN go run ./cmd/compress-app ./internal/transport/http/httpapi/appdist

# The server carries no link of its own. The Fractal module below is what
# joins a server to the network.
RUN CGO_ENABLED=0 go build -tags full -v -o /prusa_connect_demo ./cmd/demo && \
    CGO_ENABLED=0 go build -v -o /prusa_connect ./cmd/prusa-connect

# The Fractal module. It carries a go.mod of its own, so it is built inside its
# own directory and nothing of it enters the server's dependencies. The file
# name is <name>_<GOOS>_<GOARCH>, which is what the host looks for in the
# release.
#
# The release is laid out here the way scripts/pack-example-modules.sh lays one
# out: the manifest at the root, this platform's binary beside it, and a
# checksums.txt over both. `module add` reads exactly this tree.
RUN cd packages/fractal && \
    CGO_ENABLED=0 go build -v -o "/release/fractal_$(go env GOOS)_$(go env GOARCH)" . && \
    cp module.yaml openapi.json /release/ && \
    cd /release && \
    find . -type f ! -name checksums.txt | sed 's|^\./||' | LC_ALL=C sort | \
    while IFS= read -r file; do sha256sum "$file"; done > checksums.txt

# Digest-pinned: a floating tag means two builds of the same commit can differ.
FROM debian:trixie-slim@sha256:3a39a0592364683e6bab97937b72cad5a8fa6dcbbee90edb3bb48c7f8e94f258

ARG APP_UID=10001
ARG APP_GID=10001

COPY --from=builder /prusa_connect_demo /prusa_connect_demo
COPY --from=builder /prusa_connect /prusa_connect
COPY --from=builder /release /release

# The settings of all four servers and the operator's file, baked rather than
# mounted. Docker Desktop on the machine this repository is developed on
# refuses every bind mount with `input/output error`, so a fixture that needs a
# file carries it in a layer.
COPY docker/mockup/config/ /etc/prusa-connect/
COPY docker/mockup/start.sh /start.sh

RUN set -eux; \
    groupadd --gid "${APP_GID}" prusa; \
    useradd --uid "${APP_UID}" --gid "${APP_GID}" --no-create-home --shell /usr/sbin/nologin prusa; \
    install -d -o prusa -g prusa -m 0750 /data; \
    chmod 0755 /start.sh

WORKDIR /

USER prusa

# Probes GET /api/v1/readiness through the shipping binary, so the image needs
# no curl or wget. The address is given because this image carries no
# /config/config.yaml for the probe to read one from.
HEALTHCHECK --interval=10s --timeout=5s --start-period=60s --retries=6 \
    CMD ["/prusa_connect", "core", "-addr", "127.0.0.1:8080", "healthcheck"]

ENTRYPOINT ["/start.sh"]
