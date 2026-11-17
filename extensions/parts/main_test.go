package main

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/wirerpc"

	"github.com/prusa3d/parts/internal/catalog"
	"github.com/prusa3d/parts/internal/hostfake"
	"github.com/prusa3d/parts/internal/settings"
)

// A setting of the wrong shape refuses the start, and the host shows the
// reason on the instance. The module never runs on a default nobody chose.
func TestASettingOfTheWrongShapeRefusesTheStart(t *testing.T) {
	t.Parallel()

	for name, config := range map[string]string{
		"an empty storage":       `{"storage":"","root":"parts"}`,
		"a root that climbs out": `{"storage":"local","root":"../outside"}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			live := &settings.Live{}
			serves := methods(catalog.New(&hostfake.Records{}, &hostfake.Files{}, live.Get().Place(), at), live,
				&database{})

			params, err := json.Marshal(wirerpc.Settings{Config: json.RawMessage(config)})
			if err != nil {
				t.Fatalf("render the settings: %v", err)
			}

			if _, err = serves[wirerpc.MethodConfigure](context.Background(),
				wirerpc.Call{Method: wirerpc.MethodConfigure, Params: params}); err == nil {
				t.Errorf("the settings %s were accepted", config)
			}
		})
	}
}

// A module granted no database refuses its start, and the host shows why. It
// has nowhere to keep the catalogue.
func TestAStartWithNoDatabaseIsRefused(t *testing.T) {
	t.Parallel()

	live := &settings.Live{}
	serves := methods(catalog.New(&hostfake.Records{}, &hostfake.Files{}, live.Get().Place(), at), live,
		&database{})

	params, err := json.Marshal(wirerpc.Settings{Config: json.RawMessage(`{"storage":"local","root":"parts"}`)})
	if err != nil {
		t.Fatalf("render the settings: %v", err)
	}

	_, err = serves[wirerpc.MethodConfigure](context.Background(),
		wirerpc.Call{Method: wirerpc.MethodConfigure, Params: params})
	if err == nil || !strings.Contains(err.Error(), "needs.database") {
		t.Errorf("a start with no database answered %v", err)
	}
}

// The module migrates its database in plugin.configure and answers the schema
// version, which the host shows the operator.
func TestTheStartReportsTheSchemaOfTheDatabase(t *testing.T) {
	t.Parallel()

	live := &settings.Live{}
	opened := &database{}
	t.Cleanup(opened.close)

	serves := methods(catalog.New(&hostfake.Records{}, &hostfake.Files{}, live.Get().Place(), at), live, opened)

	params, err := json.Marshal(wirerpc.Settings{
		Config: json.RawMessage(`{"storage":"local","root":"parts"}`),
		Grants: &wirerpc.Grants{Database: &wirerpc.DatabaseGrant{
			Engine: wirerpc.DatabaseSQLite, DSN: filepath.Join(t.TempDir(), "database.sqlite"),
		}},
	})
	if err != nil {
		t.Fatalf("render the settings: %v", err)
	}

	answer, err := serves[wirerpc.MethodConfigure](context.Background(),
		wirerpc.Call{Method: wirerpc.MethodConfigure, Params: params})
	if err != nil {
		t.Fatalf("the start answered %v", err)
	}

	configured, ok := answer.(wirerpc.Configured)
	if !ok || configured.Database == nil || configured.Database.Version != 1 {
		t.Errorf("the start answered %#v, and the database holds schema version 1", answer)
	}
}
