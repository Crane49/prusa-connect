# The issuer fixture's image: the mock OpenID Connect provider with this
# project's subjects baked in.
#
# An image of its own rather than a mounted file, because the subjects file
# has to reach the container without a bind mount. The Docker Desktop on the
# machine this repository is developed on refuses every bind mount with an
# `input/output error` on the host path. One COPY layer over the pinned image
# keeps `read_only: true` and keeps the subjects a file under config/.
#
# Pinned by digest. ghcr.io/bluecatengineering/mock-oidc-provider v0.3.4 at
# the time of pinning, the same digest mock-oidc.yml carried before the image
# moved here.
FROM ghcr.io/bluecatengineering/mock-oidc-provider@sha256:99f74d931ae5cc8e522c63ac2244302170b7cb9ff5ae8731c7ebb6f101b2becb

# Owned by the uid the service runs as, so the file reads whatever the host's
# umask was when it was checked out.
COPY --chown=65532:65532 config/users.yaml /config/users.yaml
