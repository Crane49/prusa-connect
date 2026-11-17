package sqlstore

import (
	"errors"
	"fmt"
)

// Engine names the SQL backend behind a store. Both are supported the same
// way: every statement is written once, with '?' placeholders, and Q renders
// it for the engine. It is one type for every store over the host's database,
// so the composition root hands each the engine it opened without translating.
type Engine uint8

// The engines this build speaks.
const (
	EngineSQLite Engine = iota + 1
	EnginePostgres
)

// ErrUnsupportedEngine reports a store built without a known engine. The
// refusal comes at construction and not as a wrong query at the first request.
var ErrUnsupportedEngine = errors.New("sqlstore: unsupported sql engine")

// Check refuses an engine outside the closed set.
func (e Engine) Check() error {
	if e != EngineSQLite && e != EnginePostgres {
		return fmt.Errorf("%w: %d", ErrUnsupportedEngine, e)
	}

	return nil
}

// Q renders a statement for this engine.
func (e Engine) Q(query string) string {
	if e != EnginePostgres {
		return query
	}

	return Renumber(query)
}
