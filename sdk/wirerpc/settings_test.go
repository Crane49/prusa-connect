package wirerpc_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

// settings is what a module of this shape reads out of its config block.
type settings struct {
	Greeting string `json:"greeting"`
	Rounds   int    `json:"rounds"`
}

// paramsOf is one plugin.configure call, as a host sends it.
func paramsOf(t *testing.T, config string) json.RawMessage {
	t.Helper()

	encoded, err := json.Marshal(wirerpc.Settings{Config: json.RawMessage(config)})
	if err != nil {
		t.Fatalf("render the params: %v", err)
	}

	return encoded
}

// TestTheSettingsAreWhatTheHostSent: a module reads the merged block it was
// given, key for key.
func TestTheSettingsAreWhatTheHostSent(t *testing.T) {
	t.Parallel()

	held, err := wirerpc.ReadSettings[settings](paramsOf(t, `{"greeting":"ahoj","rounds":3}`))
	if err != nil {
		t.Fatalf("read the settings: %v", err)
	}

	if held.Greeting != "ahoj" || held.Rounds != 3 {
		t.Fatalf("the settings read as %+v", held)
	}
}

// TestAKeyTheModuleDoesNotReadRefusesTheStart: the host has already held the
// block to the keys the manifest declared, and the module holds it to the shape
// behind each one. A key it does not carry is a refusal and never a value
// quietly dropped.
func TestAKeyTheModuleDoesNotReadRefusesTheStart(t *testing.T) {
	t.Parallel()

	_, err := wirerpc.ReadSettings[settings](paramsOf(t, `{"greetng":"ahoj"}`))
	if !errors.Is(err, wirerpc.ErrSettings) {
		t.Fatalf("the refusal is %v, want the one for settings this module cannot read", err)
	}
}

// TestAnEmptyBlockIsTheModuleDefaults: a module nobody wrote a setting for, and
// whose manifest declares no default, is configured with nothing.
func TestAnEmptyBlockIsTheModuleDefaults(t *testing.T) {
	t.Parallel()

	for name, params := range map[string]json.RawMessage{
		"no params at all": nil,
		"an empty block":   paramsOf(t, `{}`),
		"no config member": json.RawMessage(`{}`),
		"a null config":    json.RawMessage(`{"config":null}`),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			held, err := wirerpc.ReadSettings[settings](params)
			if err != nil {
				t.Fatalf("read the settings: %v", err)
			}

			if held != (settings{}) {
				t.Fatalf("the settings read as %+v", held)
			}
		})
	}
}

// TestConfigureHandsTheModuleItsSettings: the handler a module registers is
// called with the block decoded, and answers nothing.
func TestConfigureHandsTheModuleItsSettings(t *testing.T) {
	t.Parallel()

	taken := make(chan settings, 1)

	handler := wirerpc.Configure(func(_ context.Context, held settings) error {
		taken <- held

		return nil
	})

	answer, err := handler(t.Context(), wirerpc.Call{
		Method: wirerpc.MethodConfigure,
		Params: paramsOf(t, `{"greeting":"ahoj"}`),
	})
	if err != nil {
		t.Fatalf("configure: %v", err)
	}

	if answer != nil {
		t.Fatalf("plugin.configure answered %v, want nothing", answer)
	}

	if held := <-taken; held.Greeting != "ahoj" {
		t.Fatalf("the module was given %+v", held)
	}
}

// TestAModuleThatRefusesItsSettingsRefusesTheStart: the error the module's own
// handler returns is what the host reads.
func TestAModuleThatRefusesItsSettingsRefusesTheStart(t *testing.T) {
	t.Parallel()

	refused := errors.New("this deployment needs a greeting")

	handler := wirerpc.Configure(func(context.Context, settings) error { return refused })

	if _, err := handler(t.Context(), wirerpc.Call{Params: paramsOf(t, `{}`)}); !errors.Is(err, refused) {
		t.Fatalf("the refusal is %v, want the module's own", err)
	}
}

// TestAModuleReadsWhatItWasGranted: the ports and the directory the operator
// approved reach the module beside its settings.
func TestAModuleReadsWhatItWasGranted(t *testing.T) {
	t.Parallel()

	encoded, err := json.Marshal(wirerpc.Settings{
		Config: json.RawMessage(`{"greeting":"ahoj"}`),
		Grants: &wirerpc.Grants{
			Listen: map[string]wirerpc.ListenGrant{"syslog": {Network: "udp", Port: 5514}},
			Data:   "/srv/module-data/buddy-debug-logs",
			Database: &wirerpc.DatabaseGrant{
				Engine: wirerpc.DatabasePostgres,
				DSN:    "postgres://module_notes:s3cret@db.lan:5432/module_notes?sslmode=require",
			},
		},
	})
	if err != nil {
		t.Fatalf("render the params: %v", err)
	}

	var (
		held   settings
		grants wirerpc.Grants
	)

	handler := wirerpc.ConfigureGranted(func(_ context.Context, s settings, g wirerpc.Grants) error {
		held, grants = s, g

		return nil
	})

	if _, err = handler(t.Context(), wirerpc.Call{Params: encoded}); err != nil {
		t.Fatalf("configure: %v", err)
	}

	if held.Greeting != "ahoj" {
		t.Errorf("the settings read as %+v", held)
	}

	if got := grants.Listen["syslog"]; got.Network != "udp" || got.Port != 5514 {
		t.Errorf("the syslog grant is %+v", got)
	}

	if grants.Data != "/srv/module-data/buddy-debug-logs" {
		t.Errorf("the data directory is %q", grants.Data)
	}

	if grants.Database == nil || grants.Database.Engine != wirerpc.DatabasePostgres ||
		grants.Database.DSN != "postgres://module_notes:s3cret@db.lan:5432/module_notes?sslmode=require" {
		t.Errorf("the database grant is %+v", grants.Database)
	}
}

// TestADatabaseGrantPrintsNoPassword: a module that logs its grant names the
// server and the database and never the password (ADR-20261021-bqc rule 10).
func TestADatabaseGrantPrintsNoPassword(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		grant wirerpc.DatabaseGrant
		want  string
	}{
		{
			wirerpc.DatabaseGrant{Engine: wirerpc.DatabasePostgres, DSN: "postgres://notes:s3cret@db.lan:5432/notes?password=s3cret"},
			"postgres postgres://notes:xxxxx@db.lan:5432/notes",
		},
		{
			wirerpc.DatabaseGrant{Engine: wirerpc.DatabaseSQLite, DSN: "/srv/module-data/notes/database.sqlite"},
			"sqlite /srv/module-data/notes/database.sqlite",
		},
	} {
		if got := tc.grant.String(); got != tc.want {
			t.Errorf("String() = %q, want %q", got, tc.want)
		}
	}
}

// TestAModuleGrantedNothingReadsNoGrants: a host that sends no grants, which
// every host before ADR-20261019-3ns is, leaves the module with no port and no
// directory rather than refusing it.
func TestAModuleGrantedNothingReadsNoGrants(t *testing.T) {
	t.Parallel()

	grants, err := wirerpc.ReadGrants(paramsOf(t, `{}`))
	if err != nil {
		t.Fatalf("read the grants: %v", err)
	}

	if len(grants.Listen) != 0 || grants.Data != "" || grants.Database != nil {
		t.Errorf("grants = %+v, want none", grants)
	}
}

// TestAStagedGrantIsReadByTheRouteTheManifestWrote: the module looks its
// limit up by the method and the path pattern of its manifest, and a route
// the operator did not approve has none (ADR-20261120-xys).
func TestAStagedGrantIsReadByTheRouteTheManifestWrote(t *testing.T) {
	t.Parallel()

	grants, err := wirerpc.ReadGrants(json.RawMessage(`{"config":{},"grants":{"data":"/srv/m",` +
		`"staged":[{"method":"PUT","path":"/files/{name}","max_mib":2048}]}}`))
	if err != nil {
		t.Fatalf("read the grants: %v", err)
	}

	if got, ok := grants.StagedMiB("PUT", "/files/{name}"); !ok || got != 2048 {
		t.Errorf("StagedMiB(PUT /files/{name}) = %d, %t, want 2048, true", got, ok)
	}

	if got, ok := grants.StagedMiB("POST", "/files/{name}"); ok {
		t.Errorf("StagedMiB(POST /files/{name}) = %d, true, want no grant", got)
	}
}
