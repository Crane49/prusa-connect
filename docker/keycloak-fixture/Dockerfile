# The Keycloak fixture's image: Keycloak with this project's test realm baked
# in, so `start-dev --import-realm` finds it without a bind mount
# (docker-services.md says why there is none).
#
# Pinned by digest. quay.io/keycloak/keycloak 26.8.0 at the time of pinning.
FROM quay.io/keycloak/keycloak:26.8.0@sha256:27b3230fbabb8c9ff3c66c864cf09dcd8a41aca82e5100679d109ef0eeb05741

# Owned by the uid the image runs as, so the import reads the file whatever
# the host's umask was when it was checked out.
COPY --chown=1000:0 config/pcl-realm.json /opt/keycloak/data/import/pcl-realm.json
