# The authentik fixture's image: authentik with the sign-in check's blueprint
# baked in, so the worker applies it at start without a bind mount
# (docker-services.md says why there is none). The server and the worker run
# this one image, as authentik's own compose file runs one image for both.
#
# Pinned by digest. ghcr.io/goauthentik/server 2026.8.4 at the time of pinning.
FROM ghcr.io/goauthentik/server:2026.8.4@sha256:e7226ae116dac5aa0a5643803e735b010038f492c401e08e16b3bf1ac54c090c

# Owned by the uid the image runs as.
COPY --chown=1000:1000 config/pcl.yaml /blueprints/custom/pcl.yaml
