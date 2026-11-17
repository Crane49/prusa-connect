package xbuddy_test

import (
	"context"
	"log/slog"
	"maps"
	"testing"

	"github.com/prusa3d/prusa-connect/internal/connector/xbuddy"
	"github.com/prusa3d/prusa-connect/internal/printer/capability"
	"github.com/prusa3d/prusa-connect/internal/printer/profile"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// rawGCodeKeys is the verb and its argument, which a profile subtracts
// together: leaving the argument behind would describe an argument of a verb
// that is not there, and the merge drops it anyway.
func rawGCodeKeys(verb connectorapi.Capability) []connectorapi.Capability {
	return []connectorapi.Capability{verb, connectorapi.ArgumentKey(verb, "gcode")}
}

// withoutKeys returns the registry with those keys taken out of every profile,
// which is what a deployment does when it decides a model does not offer raw
// gcode.
func withoutKeys(t *testing.T, keys []connectorapi.Capability) profile.Registry {
	t.Helper()

	builtin, err := profile.Builtin()
	if err != nil {
		t.Fatalf("loading the built-in profiles: %v", err)
	}

	out := profile.Registry{Schema: builtin.Schema}

	for _, candidate := range builtin.Profiles {
		narrowed := maps.Clone(candidate.Capabilities)
		for _, key := range keys {
			delete(narrowed, key)
		}

		candidate.Capabilities = narrowed
		out.Profiles = append(out.Profiles, candidate)
	}

	return out
}

// A profile that leaves the verb out is what turns raw gcode off for a model, a
// firmware band or a deployment, and the intersection-only merge is the whole
// of the mechanism (ADR-20260825-z65 rule 1).
//
// It is asserted through the driver's own session rather than by reading the
// resolved set, because the set being right is not the claim: the claim is that
// the command is refused before anything reaches the wire.
func TestAProfileThatWithholdsRawGCodeTurnsItOff(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		withheld []connectorapi.Capability
		refused  []connectorapi.Capability
		kept     []connectorapi.Capability
	}{
		"both verbs withheld": {
			withheld: append(rawGCodeKeys(xbuddy.VerbSendGCode), rawGCodeKeys(xbuddy.VerbForceGCode)...),
			refused:  []connectorapi.Capability{xbuddy.VerbSendGCode, xbuddy.VerbForceGCode},
		},
		"only the out-of-band frame withheld": {
			withheld: rawGCodeKeys(xbuddy.VerbForceGCode),
			refused:  []connectorapi.Capability{xbuddy.VerbForceGCode},
			kept:     []connectorapi.Capability{xbuddy.VerbSendGCode},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			baseline := driverBaseline()
			resolver := capability.NewResolver(withoutKeys(t, tc.withheld), slog.New(slog.DiscardHandler))
			resolution := resolver.Resolve(t.Context(), baseline, identityOf("MK4"), nil, 1)

			if !resolution.ProfileMatched {
				t.Fatal("no profile matched MK4, so this measures the coverage tier and not a profile")
			}

			for _, verb := range tc.refused {
				if !baseline.Has(verb) {
					t.Fatalf("%s is not in the baseline, so withholding it from a profile proves nothing", verb)
				}

				if resolution.Capabilities.Has(verb) {
					t.Errorf("%s survived a profile that does not declare it, so the merge added a capability", verb)
				}
			}

			for _, verb := range tc.kept {
				if !resolution.Capabilities.Has(verb) {
					t.Errorf("%s was withheld along with the verb the profile actually dropped", verb)
				}
			}

			driver := &stubSession{caps: resolution.Capabilities}
			session := capability.Wrap(driver, resolution)

			for _, verb := range tc.refused {
				_, err := session.Do(t.Context(), connectorapi.Command{
					Verb: verb,
					Args: map[string]string{"gcode": "M117 hello"},
				})
				if err == nil {
					t.Errorf("%s was carried out for a printer whose profile withholds it", verb)

					continue
				}

				if !fault.HasCode(err, connectorapi.CapabilityNotSupported.Code()) {
					t.Errorf("%s was refused with %v, and a withheld capability is a capability refusal", verb, err)
				}
			}

			if len(driver.reached) != 0 {
				t.Errorf("the driver was reached with %v for a capability the host did not resolve", driver.reached)
			}

			for _, verb := range tc.kept {
				if _, err := session.Do(t.Context(), connectorapi.Command{
					Verb: verb,
					Args: map[string]string{"gcode": "M117 hello"},
				}); err != nil {
					t.Errorf("%s was refused for a printer whose profile keeps it: %v", verb, err)
				}
			}

			if len(driver.reached) != len(tc.kept) {
				t.Errorf("the driver was reached %d times and the profile keeps %d verbs",
					len(driver.reached), len(tc.kept))
			}
		})
	}
}

// stubSession stands in for the driver so that the refusal is measured at the
// host's own check. It fails loudly if it is reached, which is the assertion:
// the driver is never called for a capability the host did not resolve.
type stubSession struct {
	connectorapi.Session

	caps connectorapi.Set
	// reached records that Do was called, which a refused command must not do.
	reached []connectorapi.Capability
}

func (s *stubSession) Capabilities() (connectorapi.Set, uint64) { return s.caps, 1 }

func (s *stubSession) Do(_ context.Context, cmd connectorapi.Command) (connectorapi.Result, error) {
	s.reached = append(s.reached, cmd.Verb)

	return connectorapi.Result{OK: true}, nil
}
