package main

import (
	"errors"
	"testing"
	"time"

	"github.com/prusa3d/fractal-link/natsbind/joinwire"
)

// smallest is the smallest settings of a server that joins one above it: the
// node id, the data directory, the dedup window and the address to join.
const smallest = `{
	"node_id": "workshop",
	"data_dir": "/var/lib/prusa-connect/fractal",
	"dedup_window": "5m",
	"join": {"address": "nats://hq.example:4222"}
}`

// accepting is the smallest settings of a server that accepts other servers
// under it.
const accepting = `{
	"node_id": "hq",
	"data_dir": "/var/lib/prusa-connect/fractal",
	"dedup_window": "5m",
	"accept": {
		"advertise": "tls://hq.example:7422",
		"client_listen": "0.0.0.0:4222",
		"leaf_listen": "0.0.0.0:7422",
		"ask_first": true
	}
}`

// TestTheSmallestSettingsOfAServerThatJoins: Binding copies the settings of a
// server that joins one above it into the transport configuration, and the
// transport accepts them.
func TestTheSmallestSettingsOfAServerThatJoins(t *testing.T) {
	t.Parallel()

	held, err := ParseConfig([]byte(smallest))
	if err != nil {
		t.Fatalf("read the settings: %v", err)
	}

	cfg, err := held.Binding()
	if err != nil {
		t.Fatalf("build the transport configuration: %v", err)
	}

	if cfg.NodeID != "workshop" {
		t.Errorf("the node id is %q, want workshop", cfg.NodeID)
	}

	if cfg.ParentAddr != "nats://hq.example:4222" {
		t.Errorf("the address of the server above is %q", cfg.ParentAddr)
	}

	if cfg.AcceptsChildren {
		t.Error("the settings have no accept block, and AcceptsChildren is true")
	}

	if cfg.DedupWindow != 5*time.Minute {
		t.Errorf("the dedup window is %s, want 5m", cfg.DedupWindow)
	}

	if err = cfg.Validate(); err != nil {
		t.Fatalf("the transport refused what the operator wrote: %v", err)
	}
}

// TestTheSmallestSettingsOfAServerThatAccepts: Binding copies the accept block
// into the transport configuration, with ask_first, and the verification mode
// is tofu when the settings name none.
func TestTheSmallestSettingsOfAServerThatAccepts(t *testing.T) {
	t.Parallel()

	held, err := ParseConfig([]byte(accepting))
	if err != nil {
		t.Fatalf("read the settings: %v", err)
	}

	cfg, err := held.Binding()
	if err != nil {
		t.Fatalf("build the transport configuration: %v", err)
	}

	if !cfg.AcceptsChildren || !cfg.AskFirst {
		t.Error("the settings have an accept block with ask_first, and AcceptsChildren or AskFirst is false")
	}

	if cfg.AdvertisedLeafRemote != "tls://hq.example:7422" {
		t.Errorf("the advertised address is %q", cfg.AdvertisedLeafRemote)
	}

	if cfg.Verification.String() != "tofu" {
		t.Errorf("the verification mode is %q, want tofu when the settings name none",
			cfg.Verification)
	}

	if err = cfg.Validate(); err != nil {
		t.Fatalf("the transport refused what the operator wrote: %v", err)
	}
}

// TestTheKeyAddressReachesTheTransportUnderJWT: under "jwt" the transport gets
// a key source built from accept.jwks_url, and it accepts the configuration. A
// server set to "jwt" could not start without it.
func TestTheKeyAddressReachesTheTransportUnderJWT(t *testing.T) {
	t.Parallel()

	held, err := ParseConfig([]byte(`{
		"node_id": "hq",
		"data_dir": "/data",
		"dedup_window": "5m",
		"accept": {
			"advertise": "tls://hq:7422",
			"verification": "jwt",
			"jwks_url": "https://hq.example/jwks"
		}
	}`))
	if err != nil {
		t.Fatalf("read the settings: %v", err)
	}

	cfg, err := held.Binding()
	if err != nil {
		t.Fatalf("build the transport configuration: %v", err)
	}

	source, ok := cfg.KeySource.(*joinwire.JWKS)
	if !ok {
		t.Fatalf("the key source is %T, want one that fetches a key document", cfg.KeySource)
	}

	if source.URL != "https://hq.example/jwks" {
		t.Errorf("the key source reads %q, want the address the operator wrote", source.URL)
	}

	if validateErr := cfg.Validate(); validateErr != nil {
		t.Errorf("the transport refused a server set to jwt: %v", validateErr)
	}
}

// TestEverySettingReachesTheTransport: Binding copies each setting in the map
// below into the transport configuration.
func TestEverySettingReachesTheTransport(t *testing.T) {
	t.Parallel()

	held, err := ParseConfig([]byte(`{
		"node_id": "hq",
		"data_dir": "/data",
		"dedup_window": "10m",
		"credential_lifetime": "12h",
		"heartbeat_interval": "2s",
		"max_heartbeat_interval": "6s",
		"liveness_multiple": 4,
		"ack_wait": "45s",
		"stream_max_age": "48h",
		"stream_max_bytes": 1048576,
		"max_payload": 4194304,
		"poll_interval": "2s",
		"tls": {"cert_file":"/c.pem","key_file":"/k.pem","ca_files":["/ca.pem"]},
		"accept": {
			"advertise": "tls://hq:7422",
			"verification": "jwt",
			"jwks_url": "https://hq/jwks",
			"max_pending_nodes": 16,
			"max_joins_in_flight": 4,
			"joins_per_second": 2
		},
		"shadow": {"patterns":["*"]},
		"flush_interval": "250ms",
		"resync_interval": "45s",
		"command_principal": "self"
	}`))
	if err != nil {
		t.Fatalf("read the settings: %v", err)
	}

	cfg, err := held.Binding()
	if err != nil {
		t.Fatalf("build the transport configuration: %v", err)
	}

	for name, ok := range map[string]bool{
		"the credential lifetime":    cfg.CredentialLifetime == 12*time.Hour,
		"the heartbeat interval":     cfg.HeartbeatInterval == 2*time.Second,
		"the max heartbeat interval": cfg.MaxHeartbeatInterval == 6*time.Second,
		"the liveness multiple":      cfg.LivenessMultiple == 4,
		"the ack wait":               cfg.AckWait == 45*time.Second,
		"the stream max age":         cfg.StreamMaxAge == 48*time.Hour,
		"the stream max bytes":       cfg.StreamMaxBytes == 1048576,
		"the max payload":            cfg.MaxPayload == 4194304,
		"the poll interval":          cfg.PollInterval == 2*time.Second,
		"the certificate":            cfg.TLS != nil && cfg.TLS.CertFile == "/c.pem",
		"the trust anchors":          cfg.TLS != nil && len(cfg.TLS.CAFiles) == 1,
		"the verification mode":      cfg.Verification.String() == "jwt",
		"the max pending nodes":      cfg.MaxPendingNodes == 16,
		"the max joins in flight":    cfg.MaxJoinsInFlight == 4,
		"the joins per second":       cfg.JoinsPerSecond == 2,
		"the shadow patterns":        len(cfg.Shadow.Selection.Patterns) == 1,
	} {
		if !ok {
			t.Errorf("%s is not in the transport configuration", name)
		}
	}

	flush, err := held.Flush()
	if err != nil || flush != 250*time.Millisecond {
		t.Errorf("the flush interval is %s (%v)", flush, err)
	}

	resync, err := held.Resync()
	if err != nil || resync != 45*time.Second {
		t.Errorf("the resync interval is %s (%v)", resync, err)
	}

	if held.CommandPrincipal != PrincipalSelf {
		t.Errorf("the command principal is %q, want self", held.CommandPrincipal)
	}
}

// TestSettingsThisBuildWillNotTakeAreRefusedByName: ParseConfig returns a
// separate error for each mistake, so the operator reads which key to change.
func TestSettingsThisBuildWillNotTakeAreRefusedByName(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		record string
		want   error
	}{
		"no node_id": {
			record: `{"data_dir":"/data","dedup_window":"5m"}`,
			want:   ErrNoNodeID,
		},
		"no data_dir": {
			record: `{"node_id":"hq","dedup_window":"5m"}`,
			want:   ErrNoDataDir,
		},
		"an accept block with no advertise": {
			record: `{"node_id":"hq","data_dir":"/data","dedup_window":"5m","accept":{}}`,
			want:   ErrNoAdvertise,
		},
		"an advertise address of a host and a port": {
			record: `{"node_id":"hq","data_dir":"/data","dedup_window":"5m","accept":{"advertise":"hq:7422"}}`,
			want:   ErrBadAdvertise,
		},
		"an advertise address that is only a host": {
			record: `{"node_id":"hq","data_dir":"/data","dedup_window":"5m","accept":{"advertise":"hq"}}`,
			want:   ErrBadAdvertise,
		},
		"an advertise address with no scheme": {
			record: `{"node_id":"hq","data_dir":"/data","dedup_window":"5m","accept":{"advertise":"//hq:7422"}}`,
			want:   ErrBadAdvertise,
		},
		"an advertise address with no host name": {
			record: `{"node_id":"hq","data_dir":"/data","dedup_window":"5m","accept":{"advertise":"tls://:7422"}}`,
			want:   ErrBadAdvertise,
		},
		"a valid advertise address": {
			record: `{"node_id":"hq","data_dir":"/data","dedup_window":"5m","accept":{"advertise":"tls://hq.example:7422"}}`,
			want:   nil,
		},
		"a duration that does not parse": {
			record: `{"node_id":"hq","data_dir":"/data","dedup_window":"five minutes"}`,
			want:   ErrBadDuration,
		},
		"a flush interval that does not parse": {
			record: `{"node_id":"hq","data_dir":"/data","dedup_window":"5m","flush_interval":"half a second"}`,
			want:   ErrBadDuration,
		},
		"a resync interval that does not parse": {
			record: `{"node_id":"hq","data_dir":"/data","dedup_window":"5m","resync_interval":"a minute"}`,
			want:   ErrBadDuration,
		},
		"a command principal that is neither node nor self": {
			record: `{"node_id":"hq","data_dir":"/data","dedup_window":"5m",
				"command_principal":"user:shift-operator"}`,
			want: ErrBadCommandPrincipal,
		},
		"jwt with no address for the keys": {
			record: `{"node_id":"hq","data_dir":"/data","dedup_window":"5m",
				"accept":{"advertise":"tls://hq:7422","verification":"jwt"}}`,
			want: ErrNoJWKSURL,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if _, err := ParseConfig([]byte(tc.record)); !errors.Is(err, tc.want) {
				t.Fatalf("ParseConfig returned %v, want %v", err, tc.want)
			}
		})
	}
}

// TestAKeyThisBuildDoesNotReadRefusesTheRecord: ParseConfig refuses a key that
// Config does not have, so a misspelt key is an error and is not ignored. It
// also refuses text that is not JSON and a shadow pattern that is not valid.
func TestAKeyThisBuildDoesNotReadRefusesTheRecord(t *testing.T) {
	t.Parallel()

	for name, record := range map[string]string{
		"a misspelt key": `{"node_id":"hq","data_dir":"/d","dedup_window":"5m","dedup_windows":"5m"}`,
		"not json":       `{`,
		"a shadow pattern that is not valid": `{"node_id":"hq","data_dir":"/d","dedup_window":"5m",
			"shadow":{"patterns":["a/b"]}}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if _, err := ParseConfig([]byte(record)); err == nil {
				t.Fatal("ParseConfig accepted settings it must refuse")
			}
		})
	}
}
