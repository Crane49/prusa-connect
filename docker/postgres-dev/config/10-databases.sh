#!/bin/sh
# Makes the two databases a developer runs the core and one module against, on
# the first start of an empty volume (ADR-20261021-bqc).
#
# connect is the core's database, reached by the role connect. That role may
# make roles and databases and is not a superuser, so the core can make a
# database for another module itself with the mode provision.
#
# notes is a module's database, reached by the role notes, which may only log in.
# A module given it with the mode postgres opens it with its own password.
#
# Each database refuses a connection from every role but its owner, so neither
# role can open the other's database.
set -eu

psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname postgres \
	-v core_password="${PGDEV_CORE_PASSWORD:-connect}" \
	-v notes_password="${PGDEV_MODULE_PASSWORD:-notes}" <<'SQL'
CREATE ROLE connect LOGIN CREATEDB CREATEROLE NOSUPERUSER NOREPLICATION PASSWORD :'core_password';
CREATE ROLE notes LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION PASSWORD :'notes_password';
CREATE DATABASE connect OWNER connect;
CREATE DATABASE notes OWNER notes;
REVOKE ALL ON DATABASE connect FROM PUBLIC;
REVOKE ALL ON DATABASE notes FROM PUBLIC;
REVOKE CONNECT ON DATABASE postgres FROM PUBLIC;
COMMENT ON DATABASE notes IS 'a module database for development';
SQL
