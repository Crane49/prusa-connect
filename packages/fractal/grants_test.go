package main

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

// configure sends one plugin.configure call with files and grants, and
// returns what the handler answered and the settings it passed on.
func configure(t *testing.T, files string, grants *wirerpc.Grants) (Config, error) {
	t.Helper()

	params, err := json.Marshal(wirerpc.Settings{
		Config: json.RawMessage(`{"node_id":"hq","data_dir":"/d","dedup_window":"5m",
			"tls":{"cert_file":"hq.pem","key_file":"hq.key"},"files":` + files + `}`),
		Grants: grants,
	})
	if err != nil {
		t.Fatalf("encode the params: %v", err)
	}

	settings := make(chan Config, 1)
	if _, err = configureWith(settings)(t.Context(), wirerpc.Call{Params: params}); err != nil {
		return Config{}, err
	}

	return <-settings, nil
}

// granted is the grants of a module approved for the port files on 8443 and
// its data directory.
func granted() *wirerpc.Grants {
	return &wirerpc.Grants{
		Listen: map[string]wirerpc.ListenGrant{"files": {Network: "tcp", Port: 8443}},
		Data:   "/var/lib/prusa-connect/module-data/fractal",
	}
}

// TestConfigureKeepsGrants: the settings the node starts with carry the
// grants the host sent.
func TestConfigureKeepsGrants(t *testing.T) {
	t.Parallel()

	held, err := configure(t, `{"base_url":"https://hq.example:8443/fractal"}`, granted())
	if err != nil {
		t.Fatalf("configure: %v", err)
	}

	if want := *granted(); !reflect.DeepEqual(held.Grants, want) {
		t.Errorf("the node starts with the grants %+v, want %+v", held.Grants, want)
	}
}

// TestConfigureRefusesFiles: a files block without what it needs refuses the
// start, and names what is missing.
func TestConfigureRefusesFiles(t *testing.T) {
	t.Parallel()

	noData := granted()
	noData.Data = ""

	udp := granted()
	udp.Listen["files"] = wirerpc.ListenGrant{Network: "udp", Port: 8443}

	for _, row := range []struct {
		name   string
		files  string
		grants *wirerpc.Grants
		want   error
	}{
		{"no grants", `{"base_url":"https://hq.example:8443/f"}`, nil, ErrNoFilesPort},
		{"a udp port", `{"base_url":"https://hq.example:8443/f"}`, udp, ErrNoFilesPort},
		{"no data directory", `{"base_url":"https://hq.example:8443/f"}`, noData, ErrNoDataGrant},
		{"another port", `{"base_url":"https://hq.example:9443/f"}`, granted(), ErrBaseURLPort},
		{"the default port", `{"base_url":"https://hq.example/f"}`, granted(), ErrBaseURLPort},
	} {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()

			if _, err := configure(t, row.files, row.grants); !errors.Is(err, row.want) {
				t.Errorf("configure returned %v, want %v", err, row.want)
			}
		})
	}
}

// TestHTTPSNeedsCertificate: an https base_url needs the certificate of the
// tls block, and an http one, allowed in cleartext, does not.
func TestHTTPSNeedsCertificate(t *testing.T) {
	t.Parallel()

	held := Config{
		Files:  &FilesConfig{BaseURL: "https://hq.example:8443/f"},
		Grants: *granted(),
	}
	if err := held.checkGrants(); !errors.Is(err, ErrFilesNeedTLS) {
		t.Errorf("checkGrants without tls returned %v, want ErrFilesNeedTLS", err)
	}

	held.Files = &FilesConfig{BaseURL: "http://hq.example:8443/f", AllowCleartextParent: true}
	if err := held.checkGrants(); err != nil {
		t.Errorf("checkGrants of a cleartext base_url returned %v", err)
	}
}

// TestNoFilesNoGrantsNeeded: a server without a files block starts with no
// grants, as it did before the extension.
func TestNoFilesNoGrantsNeeded(t *testing.T) {
	t.Parallel()

	params, err := json.Marshal(wirerpc.Settings{
		Config: json.RawMessage(`{"node_id":"hq","data_dir":"/d","dedup_window":"5m"}`),
	})
	if err != nil {
		t.Fatalf("encode the params: %v", err)
	}

	if _, err = configureWith(make(chan Config, 1))(t.Context(), wirerpc.Call{Params: params}); err != nil {
		t.Errorf("configure without a files block returned %v", err)
	}
}
