//go:build !postgres

package authztest

import "github.com/prusa3d/prusa-connect/internal/kernel/sqlstore"

// postgresEngine reaches nothing in a build without the driver. The suite then
// runs on SQLite alone, and RequiredEnginesEnv turns that into a failure
// wherever both engines are meant to be covered.
func postgresEngine(sqlstore.Schema) (Engine, bool) { return Engine{}, false }
