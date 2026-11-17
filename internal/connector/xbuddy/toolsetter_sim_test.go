package xbuddy_test

import (
	"testing"
	"testing/synctest"
	"time"

	"github.com/prusa3d/prusa-connect/internal/connector/xbuddy"
	"github.com/prusa3d/prusa-connect/internal/printer/simulator"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// TestAToolSetterReachesASimulatedPrinterOfEitherShape sends the three tool
// setters to a simulated XL that speaks the numbered shape and to one that
// speaks the tree shape. The simulator reads the key the way the firmware
// does, so a key of the wrong base or the old flat kwargs are refused.
func TestAToolSetterReachesASimulatedPrinterOfEitherShape(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		tree bool
	}{
		{name: "numbered"},
		{name: "tree", tree: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				rig := start(t, "XL", withAutoRegistration,
					withPrinter(func(cfg *simulator.Config) { cfg.ToolTree = tc.tree }))
				defer rig.stop(t)

				session := rig.connect(t)

				// The setter waits for the INFO, and /tool_count is written
				// from it.
				for described := false; !described; {
					select {
					case patch, open := <-session.State():
						if !open {
							t.Fatal("the state channel closed before the INFO was read")
						}

						described = patch.Path == connectorapi.ToolCountPath
					case <-rig.ctx.Done():
						t.Fatal("no INFO arrived")
					}
				}

				for _, cmd := range []connectorapi.Command{
					{Verb: xbuddy.VerbSetToolDiameter, Args: map[string]string{"tool": "0", "nozzle_diameter": "0.6"}},
					{Verb: xbuddy.VerbSetToolHardened, Args: map[string]string{"tool": "1", "hardened": "true"}},
					{Verb: xbuddy.VerbSetToolHighFlow, Args: map[string]string{"tool": "4", "high_flow": "false"}},
				} {
					result, err := session.Do(rig.ctx, cmd)
					if err != nil {
						t.Fatalf("%s: %v", cmd.Verb, err)
					}

					if !result.OK {
						t.Errorf("the printer refused %s: %s", cmd.Verb, result.Raw)
					}
				}
			})
		})
	}
}

// TestANozzleSetOnASimulatedPrinterIsWhatItReportsNext sets a high-flow
// hardened 0.6 nozzle on the one head of a simulated CORE One of either shape.
// The printer reports the new nozzle in the INFO it sends after the setter,
// so the host's state tree carries it without asking.
func TestANozzleSetOnASimulatedPrinterIsWhatItReportsNext(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		tree bool
	}{
		{name: "numbered"},
		{name: "tree", tree: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				rig := start(t, "CoreOne", withAutoRegistration,
					withPrinter(func(cfg *simulator.Config) { cfg.ToolTree = tc.tree }))
				defer rig.stop(t)

				session := rig.connect(t)
				head := connectorapi.HeadID(0)
				want := map[string]string{
					connectorapi.ToolPath(head, connectorapi.FeatureNozzleDiameter): "0.6",
					connectorapi.ToolPath(head, connectorapi.FeatureNozzleHighFlow): "true",
					connectorapi.ToolPath(head, connectorapi.FeatureNozzleHardened): "true",
				}
				got := map[string]string{}

				// A minute of the bubble's clock, which telemetry moves along.
				waitFor := func(done func() bool) {
					limit := time.After(time.Minute)
					for !done() {
						select {
						case patch, open := <-session.State():
							if !open {
								t.Fatal("the state channel closed")
							}

							got[patch.Path] = string(patch.Value)
						case <-limit:
							t.Fatalf("the nozzle never reached the state tree: %v", got)
						}
					}
				}

				waitFor(func() bool { _, read := got[connectorapi.ToolCountPath]; return read })

				for _, cmd := range []connectorapi.Command{
					{Verb: xbuddy.VerbSetToolDiameter, Args: map[string]string{"tool": "0", "nozzle_diameter": "0.6"}},
					{Verb: xbuddy.VerbSetToolHighFlow, Args: map[string]string{"tool": "0", "high_flow": "true"}},
					{Verb: xbuddy.VerbSetToolHardened, Args: map[string]string{"tool": "0", "hardened": "true"}},
				} {
					result, err := session.Do(rig.ctx, cmd)
					if err != nil || !result.OK {
						t.Fatalf("%s: %v %s", cmd.Verb, err, result.Raw)
					}
				}

				waitFor(func() bool {
					for path, value := range want {
						if got[path] != value {
							return false
						}
					}

					return true
				})

				refused, err := session.Do(rig.ctx, connectorapi.Command{
					Verb: xbuddy.VerbSetToolHighFlow, Args: map[string]string{"tool": "1", "high_flow": "true"},
				})
				if err == nil && refused.OK {
					t.Error("a CORE One took a setter for a second head it does not have")
				}
			})
		})
	}
}
