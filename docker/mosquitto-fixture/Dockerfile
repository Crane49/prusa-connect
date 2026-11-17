# The broker fixture's image: Mosquitto with this fixture's configuration
# baked in.
#
# An image of its own rather than a mounted file, because the configuration
# has to reach the container without a bind mount. The Docker Desktop on the
# machine this repository is developed on refuses every bind mount with an
# `input/output error` on the host path, and a compose `configs:` entry backed
# by a file is a bind mount under another name. One COPY layer over the pinned
# image keeps the configuration a file under config/.
#
# Pinned by digest, on mock-oidc's model. The tag beside the digest is what the
# suite under adapters/events/mqtt compares against the version the broker
# announces on $SYS/broker/version, so it moves with the digest.
FROM eclipse-mosquitto:2.1.2-alpine@sha256:6f8d8a947c506f8a2290ec65cd4bd2bc7cb4d43fb5f6271f861cb013e2ef9797

# Owned by the uid the image drops to, so the file reads whatever the host's
# umask was when it was checked out.
COPY --chown=1883:1883 config/mosquitto.conf /mosquitto/config/mosquitto.conf
