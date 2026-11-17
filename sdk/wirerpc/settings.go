package wirerpc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
)

// ErrSettings reports settings a child cannot read.
var ErrSettings = errors.New("wirerpc: the host sent settings this child cannot read")

// Settings is the params of MethodConfigure: the manifest's defaults with the
// operator's config block written over them, as one JSON object. The host has
// already refused a key the manifest does not declare.
type Settings struct {
	// Config is the settings object. It is an empty object for a module the
	// operator wrote nothing for and whose manifest declares no default.
	Config json.RawMessage `json:"config"`
	// Grants is what the operator granted the module beyond the API: its
	// ports, its data directory and its database. It is absent for a module
	// granted none of them.
	Grants *Grants `json:"grants,omitempty"`
}

// Grants is what the operator approved of a manifest's needs block.
type Grants struct {
	// Listen is each granted port by the name the manifest gave it. A port
	// the manifest asked for and the operator did not approve is absent.
	Listen map[string]ListenGrant `json:"listen,omitempty"`
	// Data is the absolute path of the module's writable directory, and
	// empty for a module that asked for none.
	Data string `json:"data,omitempty"`
	// Database is the module's own database, and nil for a module that asked
	// for none or was not granted one.
	Database *DatabaseGrant `json:"database,omitempty"`
}

// DatabaseEngine names the engine of a module's own database.
type DatabaseEngine string

// The engines a module's own database runs on.
const (
	// DatabaseSQLite is a file in the module's data directory.
	DatabaseSQLite DatabaseEngine = "sqlite"
	// DatabasePostgres is a database on a Postgres server.
	DatabasePostgres DatabaseEngine = "postgres"
)

// DatabaseGrant is the database the operator gave a module. sdk/moduledb opens
// and migrates it.
type DatabaseGrant struct {
	// Engine is sqlite or postgres.
	Engine DatabaseEngine `json:"engine"`
	// DSN is where the database is. For sqlite it is the absolute path of the
	// file. For postgres it is a postgres:// URL with the module's own role
	// and password. It is a credential: never log it.
	DSN string `json:"dsn"`
}

// String names the database without its credential, for a log line.
func (g DatabaseGrant) String() string {
	if g.Engine != DatabasePostgres {
		return string(g.Engine) + " " + g.DSN
	}

	return string(g.Engine) + " " + redactedURL(g.DSN)
}

// Configured is what a child may answer MethodConfigure with. An empty
// answer is the zero value: the child has its settings and reports nothing.
type Configured struct {
	// Database is the state of the module's own database after the child
	// opened and migrated it, and nil for a module with none.
	Database *DatabaseState `json:"database,omitempty"`
}

// DatabaseState is the schema of a module's own database, as the module
// reports it after migrating.
type DatabaseState struct {
	// Version is the highest schema version in the database.
	Version int `json:"version"`
	// Compat is the compat floor the database records: the oldest schema
	// version a release may ship and still open it.
	Compat int `json:"compat"`
	// Applied is every migration this start applied, in order. It is empty
	// when the database was already up to date.
	Applied []string `json:"applied,omitempty"`
}

// ListenGrant is one port a module may listen on.
type ListenGrant struct {
	// Network is udp or tcp, as the manifest named it.
	Network string `json:"network"`
	// Port is the port the operator granted, which may differ from the one
	// the manifest asked for.
	Port int `json:"port"`
}

// Configure builds the handler a child answers MethodConfigure with. It
// decodes the settings into a value of type T and refuses a key T does not
// have. take is called once, before any other call reaches the child. The
// child must also name MethodConfigure in Info.Serves.
func Configure[T any](take func(ctx context.Context, held T) error) Handler {
	return func(ctx context.Context, call Call) (any, error) {
		held, err := ReadSettings[T](call.Params)
		if err != nil {
			return nil, err
		}

		if err = take(ctx, held); err != nil {
			return nil, err
		}

		// An empty result and no error tells the host the child has its
		// settings.
		return nil, nil
	}
}

// ConfigureGranted is Configure for a module that also reads its grants: the
// ports it may listen on, its data directory and its database.
func ConfigureGranted[T any](take func(ctx context.Context, held T, grants Grants) error) Handler {
	return func(ctx context.Context, call Call) (any, error) {
		held, err := ReadSettings[T](call.Params)
		if err != nil {
			return nil, err
		}

		grants, err := ReadGrants(call.Params)
		if err != nil {
			return nil, err
		}

		if err = take(ctx, held, grants); err != nil {
			return nil, err
		}

		return nil, nil
	}
}

// ConfigureReporting is ConfigureGranted for a module that answers with a
// Configured, such as the schema version of its database after migrating.
func ConfigureReporting[T any](take func(ctx context.Context, held T, grants Grants) (Configured, error)) Handler {
	return func(ctx context.Context, call Call) (any, error) {
		held, err := ReadSettings[T](call.Params)
		if err != nil {
			return nil, err
		}

		grants, err := ReadGrants(call.Params)
		if err != nil {
			return nil, err
		}

		answer, err := take(ctx, held, grants)
		if err != nil {
			return nil, err
		}

		if answer.Database == nil {
			return nil, nil
		}

		return answer, nil
	}
}

// ReadConfigured decodes what a child answered MethodConfigure with. An
// empty answer is the zero value.
func ReadConfigured(result json.RawMessage) (Configured, error) {
	var answer Configured

	trimmed := bytes.TrimSpace(result)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return answer, nil
	}

	if err := json.Unmarshal(trimmed, &answer); err != nil {
		return Configured{}, fmt.Errorf("wirerpc: read the answer to plugin.configure: %w", err)
	}

	return answer, nil
}

// ReadGrants decodes the grants of one MethodConfigure call. No grants is the
// zero value: no port, no data directory and no database.
func ReadGrants(params json.RawMessage) (Grants, error) {
	var carried Settings

	if len(bytes.TrimSpace(params)) != 0 {
		if err := json.Unmarshal(params, &carried); err != nil {
			return Grants{}, fmt.Errorf("%w: read the params: %w", ErrSettings, err)
		}
	}

	if carried.Grants == nil {
		return Grants{}, nil
	}

	return *carried.Grants, nil
}

// ReadSettings decodes the params of one MethodConfigure call into a value of
// type T, refusing a key T does not have. A module calls it from its own handler for
// MethodConfigure.
func ReadSettings[T any](params json.RawMessage) (T, error) {
	var (
		carried Settings
		held    T
	)

	if len(bytes.TrimSpace(params)) != 0 {
		if err := json.Unmarshal(params, &carried); err != nil {
			return held, fmt.Errorf("%w: read the params: %w", ErrSettings, err)
		}
	}

	// No settings at all is the zero value of T.
	if len(bytes.TrimSpace(carried.Config)) == 0 {
		return held, nil
	}

	decoder := json.NewDecoder(bytes.NewReader(carried.Config))
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(&held); err != nil {
		return held, fmt.Errorf("%w: %w", ErrSettings, err)
	}

	return held, nil
}

// redactedURL is a connection URL with its password replaced, and only the
// scheme when the URL does not parse.
func redactedURL(dsn string) string {
	parsed, err := url.Parse(dsn)
	if err != nil {
		return "(unreadable connection string)"
	}

	if _, set := parsed.User.Password(); set {
		parsed.User = url.UserPassword(parsed.User.Username(), "xxxxx")
	}

	parsed.RawQuery = ""

	return parsed.String()
}
