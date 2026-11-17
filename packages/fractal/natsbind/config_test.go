package natsbind_test

import (
	"errors"
	"testing"
	"time"

	"github.com/prusa3d/fractal-link/natsbind"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
	"github.com/prusa3d/prusa-connect/sdk/fractal/shadow"
)

// validConfig returns a Config with only the required fields.
func validConfig() natsbind.Config {
	return natsbind.Config{
		NodeID:      "printer-1",
		DataDir:     "data",
		DedupWindow: time.Minute,
	}
}

// TestASmallestConfigurationIsAccepted checks that Validate accepts a Config
// with only the required fields.
func TestASmallestConfigurationIsAccepted(t *testing.T) {
	t.Parallel()
	if err := validConfig().Validate(); err != nil {
		t.Errorf("Validate refused a Config with only the required fields: %v", err)
	}
}

// TestAConfigurationIsRefused checks that Validate returns the right error for
// each invalid field.
func TestAConfigurationIsRefused(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		change func(*natsbind.Config)
		want   error
	}{
		{"no node id", func(c *natsbind.Config) { c.NodeID = "" }, fractal.ErrEmpty},
		{"a node id outside the grammar", func(c *natsbind.Config) { c.NodeID = "-x" }, fractal.ErrBadFirstCharacter},
		{"no data directory", func(c *natsbind.Config) { c.DataDir = "" }, natsbind.ErrNoDataDir},
		{"no deduplication window", func(c *natsbind.Config) { c.DedupWindow = 0 }, natsbind.ErrNoDedupWindow},
		{
			"accepting children with no advertised address",
			func(c *natsbind.Config) { c.AcceptsChildren = true },
			natsbind.ErrNoLeafRemote,
		},
		{
			"a mode that is neither",
			func(c *natsbind.Config) { c.Verification = "sometimes" },
			natsbind.ErrBadMode,
		},
		{
			"jwt mode with no key source",
			func(c *natsbind.Config) {
				c.AcceptsChildren = true
				c.AdvertisedLeafRemote = "nats://hq:7422"
				c.Verification = natsbind.ModeJWT
			},
			natsbind.ErrNoKeySource,
		},
		{
			"a liveness timeout that is not a multiple",
			func(c *natsbind.Config) { c.LivenessMultiple = 1 },
			natsbind.ErrBadHeartbeat,
		},
		{
			"heartbeats rarer than the ones granted",
			func(c *natsbind.Config) {
				c.HeartbeatInterval = time.Minute
				c.MaxHeartbeatInterval = time.Second
			},
			natsbind.ErrBadHeartbeat,
		},
		{
			"a selection outside its grammar",
			func(c *natsbind.Config) {
				c.Shadow = shadow.Config{Selection: shadow.Selection{Patterns: []fractal.Pattern{"a b"}}}
			},
			fractal.ErrBadCharacter,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			cfg := validConfig()
			c.change(&cfg)
			err := cfg.Validate()
			if err == nil {
				t.Fatal("the configuration was accepted")
			}
			if !errors.Is(err, c.want) {
				t.Errorf("the refusal is %v, want %v", err, c.want)
			}
		})
	}
}

// TestACertificateNeedsItsKey checks the one pair that has to be given
// together or not at all.
func TestACertificateNeedsItsKey(t *testing.T) {
	t.Parallel()
	cfg := validConfig()
	cfg.TLS = &natsbind.TLSMaterial{CertFile: "node.pem"}
	if err := cfg.Validate(); err == nil {
		t.Error("a certificate with no key was accepted")
	}
	cfg.TLS = &natsbind.TLSMaterial{CertFile: "node.pem", KeyFile: "node.key"}
	if err := cfg.Validate(); err != nil {
		t.Errorf("a certificate with its key was refused: %v", err)
	}
	if !cfg.TLS.Serves() {
		t.Error("a node with a certificate and a key does not serve tls")
	}
	var none *natsbind.TLSMaterial
	if none.Serves() {
		t.Error("a node with no tls material serves tls")
	}
}

// TestAModeNamesItself checks the closed set of verification modes.
func TestAModeNamesItself(t *testing.T) {
	t.Parallel()
	for _, mode := range []natsbind.Mode{natsbind.ModeTOFU, natsbind.ModeJWT} {
		if !mode.Valid() {
			t.Errorf("%s is not a mode", mode)
		}
		if mode.String() == "" {
			t.Errorf("%v has no written form", mode)
		}
	}
	if natsbind.Mode("maybe").Valid() {
		t.Error("a word that is not a mode is one")
	}
}
