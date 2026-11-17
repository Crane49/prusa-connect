package xbuddy

import (
	"errors"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// A nozzle diameter of zero is refused before it reaches a frame.
//
// Value.Min is inclusive, so the descriptor's narrowest legal statement about
// this argument still admits zero, and the shipping validator refuses it. The
// vocabulary does not grow an exclusive bound for one argument (ADR-20260825-dee,
// rejected), so the rule is the driver's under rule 8 and this is what holds it.
func TestANozzleDiameterOfZeroNeverReachesAFrame(t *testing.T) {
	t.Parallel()

	set := baseline()

	for _, tc := range []struct {
		diameter string
		refused  bool
	}{
		{diameter: "0", refused: true},
		{diameter: "0.0", refused: true},
		{diameter: "0.25"},
		{diameter: "0.4"},
	} {
		t.Run(tc.diameter, func(t *testing.T) {
			t.Parallel()

			checked, err := set.Validate(connectorapi.Command{
				Verb: VerbSetToolDiameter,
				Args: map[string]string{"tool": "1", "nozzle_diameter": tc.diameter},
			})
			if err != nil {
				t.Fatalf("the descriptor refused %q, so this measures the descriptor and not the driver: %v",
					tc.diameter, err)
			}

			session := &Session{storage: defaultStorage}
			session.toolBase.Store(int32(toolBaseOne))
			_, err = session.build(checked, wireTable[VerbSetToolDiameter])

			if refused := err != nil; refused != tc.refused {
				t.Fatalf("a nozzle diameter of %q is refused = %v, want %v (%v)",
					tc.diameter, refused, tc.refused, err)
			}

			if tc.refused && !errors.Is(err, ErrArgumentUnusable) {
				t.Errorf("the refusal is %v, want one a caller can inspect for ErrArgumentUnusable", err)
			}
		})
	}
}
