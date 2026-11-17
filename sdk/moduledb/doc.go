// Package moduledb opens a module's own database and migrates it.
//
// The host sends a wirerpc.DatabaseGrant in plugin.configure. Call Open with
// it inside the plugin.configure handler and answer Report.State.
//
// A module embeds migrations/sqlite/*.sql and migrations/postgres/*.sql. Name
// a new file by its UTC date and time, such as 20261027T093015_note_tags.sql.
// One call applies every unrecorded migration in one transaction. An applied
// file must not change, and there are no down migrations.
//
// This package imports only the standard library, so the module registers the
// database drivers, such as modernc.org/sqlite and github.com/lib/pq.
package moduledb
