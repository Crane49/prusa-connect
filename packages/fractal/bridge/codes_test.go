package bridge_test

import (
	"testing"

	"github.com/prusa3d/fractal-link/bridge"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
)

// TestEveryCodeIsARefinementANodeMaySend checks the table against the rules of
// fractal.md: a node may send the code, the refinement is between 1 and 15, and
// the code refines the base its row names.
func TestEveryCodeIsARefinementANodeMaySend(t *testing.T) {
	t.Parallel()

	const maxRefinement = 15

	for _, code := range bridge.Codes() {
		t.Run(code.String(), func(t *testing.T) {
			t.Parallel()

			if !code.Valid() {
				t.Errorf("%s is a code a node must not send", code)
			}

			refinement := code.Refinement()
			if refinement == 0 || refinement > maxRefinement {
				t.Errorf("the refinement of %s is %d, and it must be between 1 and %d",
					code, refinement, maxRefinement)
			}

			base, known := bridge.BaseOf(code)
			if !known {
				t.Fatalf("%s has no base in the table", code)
			}

			if !code.Is(base) {
				t.Errorf("%s does not refine %s", code, base)
			}

			if _, named := base.Name(); !named {
				t.Errorf("%s is not a code fractal.md defines", base)
			}
		})
	}
}

// TestNoTwoCodesShareANumber checks that each case has its own number. Two
// cases on one number would leave the server above unable to tell them apart,
// which is the whole reason the table exists.
func TestNoTwoCodesShareANumber(t *testing.T) {
	t.Parallel()

	seen := map[fractal.Code]bool{}

	for _, code := range bridge.Codes() {
		if seen[code] {
			t.Errorf("%s is in the table twice", code)
		}

		seen[code] = true
	}
}

// TestEveryCodeCarriesAnEnglishSentence checks that a server above which does
// not know the number still has a sentence to show.
func TestEveryCodeCarriesAnEnglishSentence(t *testing.T) {
	t.Parallel()

	for _, code := range bridge.Codes() {
		text, ok := bridge.TextOf(code)
		if !ok || text == "" {
			t.Errorf("%s carries no English sentence", code)
		}
	}
}

// TestACodeTheBridgeDoesNotSendIsNotInTheTable checks both directions of the
// lookup: a code of fractal.md with no refinement is not one of this bridge's.
func TestACodeTheBridgeDoesNotSendIsNotInTheTable(t *testing.T) {
	t.Parallel()

	for _, code := range fractal.DefinedCodes() {
		if _, ok := bridge.TextOf(code); ok {
			t.Errorf("%s is a base code of fractal.md and the table claims it", code)
		}

		if _, ok := bridge.BaseOf(code); ok {
			t.Errorf("%s is a base code of fractal.md and the table gives it a base", code)
		}
	}
}

// TestEveryCaseKeepsItsNumber pins the number and the base of each case. The
// server above reads the number, so a changed number is a change of the wire
// and needs a changelog entry.
func TestEveryCaseKeepsItsNumber(t *testing.T) {
	t.Parallel()

	type wire struct {
		value uint16
		base  fractal.Code
	}

	cases := map[string]struct {
		code fractal.Code
		want wire
	}{
		"param missing":       {bridge.CodeParamMissing, wire{0x4012, fractal.CodeInvalidParams}},
		"value refused":       {bridge.CodeValueRefused, wire{0x4013, fractal.CodeInvalidParams}},
		"command unknown":     {bridge.CodeCommandUnknown, wire{0x4014, fractal.CodeInvalidParams}},
		"url not fetched":     {bridge.CodeURLNotFetched, wire{0x4015, fractal.CodeInvalidParams}},
		"url malformed":       {bridge.CodeURLMalformed, wire{0x4016, fractal.CodeInvalidParams}},
		"object unreachable":  {bridge.CodeObjectUnreachable, wire{0x4044, fractal.CodeObjectNotFound}},
		"state refused":       {bridge.CodeStateRefused, wire{0x2013, fractal.CodeDenied}},
		"no target":           {bridge.CodeNoTargetTemperature, wire{0x2014, fractal.CodeDenied}},
		"head not active":     {bridge.CodeHeadNotActive, wire{0x2015, fractal.CodeDenied}},
		"method unknown":      {bridge.CodeMethodUnknown, wire{0x4041, fractal.CodeObjectNotFound}},
		"device gone":         {bridge.CodeDeviceGone, wire{0x4042, fractal.CodeObjectNotFound}},
		"device unknown":      {bridge.CodeDeviceUnknown, wire{0x4043, fractal.CodeObjectNotFound}},
		"call not allowed":    {bridge.CodeCallNotAllowed, wire{0x2011, fractal.CodeDenied}},
		"printer refused":     {bridge.CodePrinterRefused, wire{0x2012, fractal.CodeDenied}},
		"printer unreachable": {bridge.CodePrinterUnreachable, wire{0x0011, fractal.CodeInternal}},
		"bridge failed":       {bridge.CodeBridgeFailed, wire{0x0012, fractal.CodeInternal}},
	}

	if len(cases) != len(bridge.Codes()) {
		t.Fatalf("the test pins %d cases and the table has %d", len(cases), len(bridge.Codes()))
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			base, _ := bridge.BaseOf(tc.code)
			if got := (wire{uint16(tc.code), base}); got != tc.want {
				t.Errorf("the case is %#04x refining %s, want %#04x refining %s",
					got.value, got.base, tc.want.value, tc.want.base)
			}
		})
	}
}
