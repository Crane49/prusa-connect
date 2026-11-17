// Package moduledatabase drives the built server, real Postgres servers in
// Docker and examples/database-module through the whole life of a module's
// own database (ADR-20261021-bqc): install, restart, an update with a schema and a
// data migration, an update whose migration fails, a downgrade the compat
// floor refuses, removal, reinstall, purge and a fresh install, on every
// layout an operator may choose.
//
// It needs Docker and takes minutes, so it runs only when
// PCL_MODULE_DATABASE_E2E is 1, which `make module-database-e2e` sets. Every
// container it starts is named pcl-moddb-e2e-<run>-<role> and removed with its
// volume when the run ends.
package moduledatabase
