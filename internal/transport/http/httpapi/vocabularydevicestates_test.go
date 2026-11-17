package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/kernel/devicestate"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/extension"
)

// placedStates is a declaration an adapter could have made, placed the way the
// host places one. The state words are the declaration's and no core table
// names them (ADR-20260831-c7b rule 2).
func placedStates(t *testing.T) devicestate.Installed {
	t.Helper()

	installed, err := devicestate.Place(devicestate.Declaration{
		Namespace: extension.CoreNamespace,
		Kind:      ref.KindPrinter,
		Connector: "core/reference",
		Instances: []string{"bench", "shop"},
		States: connectorapi.StateSet{
			States: []connectorapi.DeviceState{
				{Name: "IDLE", DisplayName: "Idle", Accepts: []connectorapi.Capability{
					"core.command.start_print", "core.command.beep",
				}},
				{Name: "PRINTING", DisplayName: "Printing", Accepts: []connectorapi.Capability{}},
			},
			Transitions: []connectorapi.Transition{
				{From: "IDLE", To: "PRINTING", Verb: "core.command.start_print"},
				{From: "PRINTING", To: "IDLE"},
			},
		},
	})
	require.NoError(t, err)

	return installed
}

// TestTheDeviceStateProjectionCarriesTheDeclaration exercises the mapping
// against a set placed here. A build with no declaration answers an empty list,
// so a comparison against the shipping default would pass on two empty lists.
func TestTheDeviceStateProjectionCarriesTheDeclaration(t *testing.T) {
	t.Parallel()

	entries := deviceStateEntries(placedStates(t))
	require.Len(t, entries, 1)

	require.Equal(t, ref.KindPrinter.String(), entries[0].Kind)
	require.Equal(t, extension.CoreNamespace.String(), entries[0].Namespace)
	require.Equal(t, "core/reference", entries[0].Connector)
	require.Equal(t, []string{"bench", "shop"}, entries[0].Connectors)
	require.True(t, entries[0].Guarded)
	require.Equal(t, []deviceStateEntry{
		{State: "IDLE", DisplayName: "Idle", Accepts: []string{"core.command.start_print", "core.command.beep"}},
		{State: "PRINTING", DisplayName: "Printing", Accepts: []string{}},
	}, entries[0].States)
	require.Equal(t, []deviceStateTransition{
		{From: "IDLE", To: "PRINTING", Verb: "core.command.start_print"},
		{From: "PRINTING", To: "IDLE"},
	}, entries[0].Transitions)
}

// TestADeploymentWithNoDeclarationAnswersAnEmptySet is the shipping answer. The
// route is published whatever an operator installed, so a client reads an empty
// list rather than a 404 and can tell "nothing is declared" from "this build
// does not serve this".
func TestADeploymentWithNoDeclarationAnswersAnEmptySet(t *testing.T) {
	t.Parallel()

	var body struct {
		Vocabulary string                 `json:"vocabulary"`
		Format     string                 `json:"format"`
		Items      []deviceStateKindEntry `json:"items"`
	}

	rec := call(t, newTestServer(t).Handler(), http.MethodGet, vocabularyDeviceStatesPath, "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))

	require.Equal(t, devicestate.VocabularyName, body.Vocabulary)
	require.Equal(t, devicestate.VocabularyFormat, body.Format)
	require.Empty(t, body.Items)
}

// TestTheServedDeviceStatesAreThePlacedSet is ADR-20260825-bhe rule 11 over the
// per-install half: the endpoint answers with what the host placed at install
// and reads no second table.
func TestTheServedDeviceStatesAreThePlacedSet(t *testing.T) {
	t.Parallel()

	server, err := New(Config{
		Version:      "test",
		Principals:   fixedPrincipal("operator"),
		Resources:    seededStore(),
		Presets:      seededPresets(),
		Endpoints:    seededEndpoints(),
		Grants:       &fakeGrantStore{},
		Sessions:     newFakeSessions(),
		Accounts:     &fakeAccounts{},
		Readiness:    &fakeReadiness{report: readyReport()},
		Devices:      noPrinters{},
		Stores:       noStorages{},
		Events:       noEvents{},
		DeviceStates: placedStates(t),
	})
	require.NoError(t, err)

	var body struct {
		Items []deviceStateKindEntry `json:"items"`
	}

	rec := call(t, server.Handler(), http.MethodGet, vocabularyDeviceStatesPath, "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))

	require.Equal(t, deviceStateEntries(placedStates(t)), body.Items)
}

// TestATransitionTheDeviceMakesAlonePublishesNoVerb holds the wire shape of the
// move a printer makes by itself. A client rendering a control reads the absent
// verb as "nothing to offer here" rather than as an empty verb it could send.
func TestATransitionTheDeviceMakesAlonePublishesNoVerb(t *testing.T) {
	t.Parallel()

	encoded, err := json.Marshal(deviceStateEntries(placedStates(t))[0].Transitions[1])
	require.NoError(t, err)

	require.JSONEq(t, `{"from":"PRINTING","to":"IDLE"}`, string(encoded))
}

// TestAStateThatAcceptsNothingPublishesAnEmptyList holds the difference a
// client reads: an empty list is "nothing is available here", and it must not
// vanish from the wire the way an omitted field would.
func TestAStateThatAcceptsNothingPublishesAnEmptyList(t *testing.T) {
	t.Parallel()

	encoded, err := json.Marshal(deviceStateEntries(placedStates(t))[0].States[1])
	require.NoError(t, err)

	require.JSONEq(t, `{"state":"PRINTING","display_name":"Printing","accepts":[]}`, string(encoded))
}

// unidentifiedPrincipal resolves every request to no subject and no error,
// which is what a Principals for an anonymous deployment answers. The refusal
// then belongs to the handler and not to the middleware, and this is the case
// that reaches it: fixedPrincipal("") answers an error instead, so the
// middleware refuses before a handler runs.
type unidentifiedPrincipal struct{}

func (unidentifiedPrincipal) Principal(*http.Request) (string, error) { return "", nil }

// TestThePerInstallVocabularyRefusesACallerWithNoSubject is the handler's own
// half of the identity gate. A deployment whose Principals resolves nobody
// without failing reaches the handler, and the set is still not served.
func TestThePerInstallVocabularyRefusesACallerWithNoSubject(t *testing.T) {
	t.Parallel()

	server, err := New(Config{
		Version:      "test",
		Principals:   unidentifiedPrincipal{},
		Resources:    seededStore(),
		Presets:      seededPresets(),
		Endpoints:    seededEndpoints(),
		Grants:       &fakeGrantStore{},
		Sessions:     newFakeSessions(),
		Accounts:     &fakeAccounts{},
		Readiness:    &fakeReadiness{report: readyReport()},
		Devices:      noPrinters{},
		Stores:       noStorages{},
		Events:       noEvents{},
		DeviceStates: placedStates(t),
	})
	require.NoError(t, err)

	rec := call(t, server.Handler(), http.MethodGet, vocabularyDeviceStatesPath, "")
	require.Equal(t, http.StatusUnauthorized, rec.Code, rec.Body.String())
	require.NotContains(t, rec.Body.String(), "IDLE",
		"the refusal carries a declared state, so it answers what it refused to answer")
}

// Caller is Principal with no scope.
func (x unidentifiedPrincipal) Caller(r *http.Request) (Caller, error) {
	return Unscoped(x.Principal(r))
}
