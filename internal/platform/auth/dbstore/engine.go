package dbstore

import "github.com/prusa3d/prusa-connect/internal/kernel/sqlstore"

// Engine names the SQL backend behind a store: the one type every store over
// the host's database shares, in internal/kernel/sqlstore.
type Engine = sqlstore.Engine

// The engines this build speaks.
const (
	EngineSQLite   = sqlstore.EngineSQLite
	EnginePostgres = sqlstore.EnginePostgres
)

// ErrUnsupportedEngine reports a store built without a known engine. The
// refusal comes at construction and not as a wrong query at the first request.
var ErrUnsupportedEngine = sqlstore.ErrUnsupportedEngine

// checkEngine refuses an engine outside the closed set.
func checkEngine(engine Engine) error { return engine.Check() }
