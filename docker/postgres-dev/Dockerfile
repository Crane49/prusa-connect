# The developer Postgres's image: Postgres 18 with a start-up script that makes
# the core's database and one module's database, each with a login of its own.
#
# An image of its own rather than a mounted file, because the script has to
# reach the container without a bind mount. docker-services.md says why: the
# Docker Desktop on the machine this repository is developed on refuses every
# bind mount. One COPY layer over the pinned image keeps the script a file
# under config/.
#
# Pinned by digest, on the model of the other images under docker/.
FROM postgres:18.6@sha256:edb0c08dc33befd0262ab3b5cf1c4f3f8cfd8b09e88c05b45adee29d6761ddb6

# The image runs every file in this folder once, on the first start of an empty
# volume, as the superuser it was given. A volume that already holds a cluster
# skips them.
COPY --chmod=0755 config/10-databases.sh /docker-entrypoint-initdb.d/10-databases.sh
