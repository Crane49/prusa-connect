# The registry fixture's image: Verdaccio with this fixture's configuration
# baked in.
#
# An image of its own rather than a mounted file, because the configuration
# has to reach the container without a bind mount. The Docker Desktop on the
# machine this repository is developed on refuses every bind mount with an
# `input/output error` on the host path, and compose's inline `content:`
# copies into the container before it starts, which a read-only root refuses.
# One COPY layer over the pinned image keeps `read_only: true` and keeps the
# configuration a file under config/.
#
# Pinned by digest, on mock-oidc's model. verdaccio/verdaccio:6.10.3 at the
# time of pinning.
FROM verdaccio/verdaccio@sha256:7b067a47ae51fb9dff3dcdce60ec0a2cbd7650c208cb4b9f6d37cb1b09b39d43

# Owned by the uid the image runs as, so the file reads whatever the host's
# umask was when it was checked out.
COPY --chown=10001:65533 config/config.yaml /verdaccio/conf/config.yaml
