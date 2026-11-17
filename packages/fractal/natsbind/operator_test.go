package natsbind_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/prusa3d/fractal-link/natsbind"

	"github.com/prusa3d/prusa-connect/sdk/fractal/fractaltest"
)

// TestApprovingANodeNobodyAskedAboutIsRefused checks that Approve returns
// ErrUnknownNode for a Node ID with no waiting join request, on a node with
// Config.AskFirst set.
func TestApprovingANodeNobodyAskedAboutIsRefused(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	cfg, _ := baseConfig(t, nodeOptions{id: "hq", accepts: true, askFirst: true})
	built := buildNode(ctx, t, cfg)
	if err := built.binding.Approve(ctx, "printer-9"); !errors.Is(err, natsbind.ErrUnknownNode) {
		t.Errorf("Approve returned %v, want ErrUnknownNode", err)
	}
}

// TestRevokingOnANodeThatTakesNothingIsRefused checks that Revoke returns an
// error on a node that does not accept joins.
func TestRevokingOnANodeThatTakesNothingIsRefused(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	cfg, _ := baseConfig(t, nodeOptions{id: "printer-1"})
	built := buildNode(ctx, t, cfg)
	if err := built.binding.Revoke(ctx, "printer-9"); err == nil {
		t.Error("Revoke returned nil on a node that does not accept joins")
	}
}

// TestADataDirectoryThatCannotBeMadeStopsTheNode checks that New returns an
// error when it cannot create the data directory.
func TestADataDirectoryThatCannotBeMadeStopsTheNode(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	blocked := filepath.Join(t.TempDir(), "in-the-way")
	if err := os.WriteFile(blocked, []byte("a file, not a directory"), 0o600); err != nil {
		t.Fatalf("write the file: %v", err)
	}
	cfg, _ := baseConfig(t, nodeOptions{id: "printer-1", dataDir: filepath.Join(blocked, "data")})
	if _, err := natsbind.New(ctx, cfg, fractaltest.NewRecorder(true)); err == nil {
		t.Error("New built a node whose data directory cannot be created")
	}
}
