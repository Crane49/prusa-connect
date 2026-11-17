# Docker services

These rules apply when you add or change anything under `docker/`, any
`docker-compose*.yml`, or any `Dockerfile`. They are rules, not suggestions.

## File layout

Compose is the tool that starts a set of containers from yaml files. A
composition is one such set.

| path | holds |
|---|---|
| `docker-compose.yml` | the base composition. It includes service files and defines nothing itself |
| `docker-compose.<purpose>.yml` | another composition: local overrides, a test fixture. It includes and defines nothing |
| `docker/<service>/<service>.yml` | the service. **No host port bindings** |
| `docker/<service>/<service>.local.yml` | local overrides only: host ports, smaller resources, louder logging |
| `docker/<service>/Dockerfile` | when the service needs an image of its own |
| `docker/<service>/config/` | configuration, baked into the image by the Dockerfile beside it |

A host port never appears in a base file. Two developers running different
ports must not have to edit tracked yaml, so every port reads from `.env` with
a default.

## Every container comes from compose

A container for this repository comes from its compose files through a make
target: `fixtures-up`, `test-adapters`, `test-engines`, `test-race-linux`,
`docs-up`, `docker-clean`. A fixture is a container a test needs, such as a
throwaway database. `docker run`, `docker cp` into a container you started
yourself, and an image you built yourself are not used. A run that needs
something the fixtures lack adds a fixture under `docker/`.

No fixture bind-mounts anything. A bind mount maps a folder on your machine
into the container. Docker Desktop on the machine this repository is developed
on refuses every bind mount with `input/output error`, and a compose `configs:`
entry backed by a file is a bind mount too. A fixture that needs a
configuration file bakes it into an image under `docker/<fixture>/`, pinned by
digest, as `docker/mosquitto-fixture` and `docker/mock-oidc` do.

## A service name says what the service is

`db`, `cache`, `api`, `test`, `tmp` and `data` are rejected in review, as
ADR-20260824-kkr rule 8 rejects them for packages. A name that could fit any service
in any project names nothing.

Say the role and, where it matters, that it is disposable. `postgres-fixture`
is a Postgres nothing persists to, and the name alone separates it from the
Postgres a real install writes to.

## Every service block carries all of these

If one is missing, the change is not finished.

| key | why |
|---|---|
| `image` pinned to a tag, never `latest` | a floating tag makes a build unreproducible and a rollback impossible |
| `container_name`, **omitted when the service scales** | `up --scale=N` fails on a fixed name |
| `restart` | |
| `env_file` pointing at the root `.env` | one file, no secrets in yaml |
| `healthcheck` | without one, `depends_on: condition: service_healthy` does nothing and status is a guess |
| `logging` with `max-size` and `max-file` | the default driver grows until the disk is full |
| `labels` naming what it does and its role | |
| `security_opt: [no-new-privileges:true]` | |
| `cap_drop: [ALL]`, adding back only what the process needs | |
| `deploy.resources` with both `limits` and `reservations` | a container with no ceiling can stop the whole host |
| `networks` | container DNS, never a host IP |

`read_only: true` with an explicit `tmpfs` for scratch space wherever the image
tolerates it. Never `privileged: true`, never `pid: host`, never
`network: host`. Run as a non-root user whenever the base image allows it.

## Health checks

Every service has one. `start_period` is the time Docker waits before it
counts a failed probe. Set it to the longest first-boot path the service has,
measured on a cold start and rounded up. Too short restarts a service that is
still starting. Too long hides a failure.

| service | probe |
|---|---|
| postgres | `pg_isready -U $$POSTGRES_USER -d $$POSTGRES_DB` |
| redis or valkey | `redis-cli ping \| grep -q PONG` |
| an HTTP service | a request to its own health endpoint, using something the image already has |
| this application | `/prusa_connect healthcheck`, which needs no curl in the image |

## Volumes

Prefer a named volume where the image creates the directory and owns it. Docker
fills an empty named volume from the image path and copies the ownership with
it. A bind mount keeps the host directory's ownership instead, and needs a chown
before a non-root process can write.

**A mount is not done until a real write has been proved under the container's
actual uid.**

```bash
docker exec <container> sh -c 'touch /data/perm-test && rm /data/perm-test'
```

Exit 0 or it is not finished. `chmod 777` is the forbidden answer. Find the uid
the image runs as, set it explicitly, and fix ownership in the image or in an
entrypoint. On Windows and macOS the file-sharing layer hides this problem, and
it appears on the Linux CI runner.

## Base image versions

An image's data layout is part of its major version and changes without
warning. Read the upstream release notes before you upgrade, and rebuild from
empty instead of reusing a directory the previous major wrote.

Postgres 18 is an example. It wants a single mount at
`/var/lib/postgresql`, not at `/var/lib/postgresql/data`, and mounting the old
path makes the container exit at boot.

## The rebuild cycle

Never destroy a database, cache or queue to update an application image.

```bash
docker compose -f <composition> stop <service>
docker compose -f <composition> build <service>
docker compose -f <composition> up -d --no-deps <service>
docker compose -f <composition> ps
```

When a service starts unhealthy, read `docker logs <name>` before anything
else. Do not destroy the volume to retry from clean unless asked.

## Gates

A gate is a check that refuses a change. `make docker-check` builds the server
image and proves it runs as a non-root user, then builds every fixture image
that has a Dockerfile and the race image. A change under `docker/` runs it
before being pushed. `make docker-clean` removes what this project's runs left
behind, selected by the compose project label, and touches nothing of another
project's.
