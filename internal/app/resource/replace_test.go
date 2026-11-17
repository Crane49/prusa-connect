package resource_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/resource"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
)

// declaredStorage is a storage instance in the operator's file, of one type,
// switched on or off.
func declaredStorage(storageType string, enabled bool) string {
	switched := "false"
	if enabled {
		switched = "true"
	}

	return `
resources:
  storage:
    local:
      type: ` + storageType + `
      enabled: ` + switched + `
      config:
        root: /var/lib/prusa
`
}

// TestANewTypeForASwitchedOffInstanceStartsNothing is an instance whose type
// is edited in a file, after which the server applies a change. A replace is a
// stop and a start, so a plan that replaced an instance switched off would
// start it. Switched off it stays off, and one that ran is stopped.
func TestANewTypeForASwitchedOffInstanceStartsNothing(t *testing.T) {
	t.Parallel()

	local := mustRef(t, ref.KindStorage, "local")

	for _, step := range []struct {
		name    string
		was     bool
		now     bool
		outcome []resource.Outcome
	}{
		{"off, still off", false, false, nil},
		{"on, now off", true, false, []resource.Outcome{resource.OutcomeStop}},
		{"on, still on", true, true, []resource.Outcome{resource.OutcomeReplace}},
		{"off, now on", false, true, []resource.Outcome{resource.OutcomeReplace}},
	} {
		t.Run(step.name, func(t *testing.T) {
			t.Parallel()

			h := newHarness(t)
			h.writeFile(declaredStorage("filesystem", step.was))
			require.NoError(t, h.store.Reload(t.Context()))

			h.plane.mu.Lock()
			before := len(h.plane.steps)
			h.plane.mu.Unlock()

			h.writeFile(declaredStorage("smb", step.now))
			require.NoError(t, h.store.Reload(t.Context()))

			h.plane.mu.Lock()
			defer h.plane.mu.Unlock()

			var outcomes []resource.Outcome

			for _, planned := range h.plane.steps[before:] {
				if planned.Ref == local {
					outcomes = append(outcomes, planned.Outcome)
				}
			}

			require.Equal(t, step.outcome, outcomes)
		})
	}
}

// overlaySink is a sink in the overlay with the uid the server minted for it,
// switched on or off.
func overlaySink(uid string, enabled bool) string {
	switched := "false"
	if enabled {
		switched = "true"
	}

	return `{
  "version": 1,
  "identities": {"sink/webhook": {"uid": "` + uid + `", "generation": 1, "digest": "typed"}},
  "instances": [
    {"kind": "sink", "name": "webhook", "type": "http", "enabled": ` + switched + `, "required": false,
     "config": {"url": "https://a.example"}}
  ]
}
`
}

// TestANewUIDForASwitchedOffInstanceStartsNothing is the other half of the
// same rule: a uid edited in the overlay is a new instance too, and one
// switched off stays off.
func TestANewUIDForASwitchedOffInstanceStartsNothing(t *testing.T) {
	t.Parallel()

	webhook := mustRef(t, ref.KindSink, "webhook")

	h := newHarness(t)
	h.writeOverlayByHand(overlaySink("01K0000000000000000000000A", false))
	require.NoError(t, h.store.Reload(t.Context()))

	h.plane.mu.Lock()
	before := len(h.plane.steps)
	h.plane.mu.Unlock()

	h.writeOverlayByHand(overlaySink("01K0000000000000000000000B", false))
	require.NoError(t, h.store.Reload(t.Context()))

	h.plane.mu.Lock()
	defer h.plane.mu.Unlock()

	for _, planned := range h.plane.steps[before:] {
		require.NotEqual(t, webhook, planned.Ref, "a new uid for a switched-off instance planned %s", planned.Outcome)
	}
}
