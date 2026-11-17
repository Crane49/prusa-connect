package httpapi

import (
	"encoding/json"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/kernel/devicestate"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/internal/printer/profile"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/event"
	"github.com/prusa3d/prusa-connect/sdk/extension"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// TestTheVocabularyIndexNamesEveryServedSetAndNoOther runs both directions of
// ADR-20260825-bhe rule 8 over the index: a vocabulary this server publishes and the
// index does not name is unfindable, and an index row pointing at a path
// nothing serves is a 404 a client was sent to.
func TestTheVocabularyIndexNamesEveryServedSetAndNoOther(t *testing.T) {
	t.Parallel()

	server := newTestServer(t)

	var served []string

	// A path below a set's own is something one of its entries links, such as
	// a model's drawing, and the index lists sets.
	for path := range server.api.OpenAPI().Paths {
		rest, under := strings.CutPrefix(path, vocabulariesPath+"/")
		if under && !strings.Contains(rest, "/") {
			served = append(served, path)
		}
	}

	require.NotEmpty(t, served, "no vocabulary operation is registered, so this gate would read nothing")

	var listed []string
	for _, entry := range vocabularyIndex() {
		listed = append(listed, entry.Path)
	}

	require.ElementsMatch(t, served, listed,
		"the index and the served operations disagree about which vocabularies this build carries")
}

// TestEveryIndexRowIsComplete refuses a row with an empty cell, which would
// publish a vocabulary nobody can find the page or the path for.
func TestEveryIndexRowIsComplete(t *testing.T) {
	t.Parallel()

	rows := vocabularyIndex()
	require.NotEmpty(t, rows)

	for _, row := range rows {
		require.NotEmptyf(t, row.Name, "a row carries no name")
		require.NotEmptyf(t, row.Path, "%s carries no path", row.Name)
		require.NotEmptyf(t, row.Reference, "%s carries no reference page", row.Name)
		require.NotEmptyf(t, row.Summary, "%s carries no summary", row.Name)
		require.Truef(t, strings.HasSuffix(row.Path, "/"+row.Name) || row.Name == "error-codes",
			"%s is served at %s, which does not spell its name", row.Name, row.Path)
	}
}

// TestEveryVocabularyReadCarriesItsEnvelope is ADR-20260826-c77 rule 18: a vocabulary
// read answers the name of the set, the content version, the shape of an entry
// and the entries themselves. The gate is on the envelope and not on the
// entries, so a vocabulary served without it fails here whatever its rows hold.
//
// A version or a format the set does not declare is answered as an empty
// string, and the member is there all the same. A client that has to tell "this
// set declares no version" from "a member I do not know about" is a client
// branching on a missing key.
//
// The index at /api/v1/vocabularies is not read here. It is the index over the
// sets rather than a set of its own, so it answers items alone and is no row in
// vocabularyIndex().
func TestEveryVocabularyReadCarriesItsEnvelope(t *testing.T) {
	t.Parallel()

	handler := newTestServer(t).Handler()

	rows := vocabularyIndex()
	require.NotEmpty(t, rows, "no vocabulary is served, so this gate would read nothing")

	for _, row := range rows {
		t.Run(row.Name, func(t *testing.T) {
			t.Parallel()

			rec := call(t, handler, http.MethodGet, row.Path, "")
			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

			var body map[string]json.RawMessage

			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))

			require.Equalf(t, strconv.Quote(row.Name), string(body["vocabulary"]),
				"%s answers no vocabulary member, or one that is not the name the index gives it",
				row.Path)
			require.Containsf(t, body, "version",
				"%s answers no version member. A set that declares none answers an empty string", row.Path)
			require.Containsf(t, body, "format",
				"%s answers no format member. A set that declares none answers an empty string", row.Path)
			require.Truef(t, carriesEntries(body),
				"%s answers no list at all. One list is named items, and a response carrying more than "+
					"one names each for what it holds (ADR-20260826-c77 rule 14)", row.Path)
		})
	}
}

// carriesEntries says whether a vocabulary answer holds a list of entries. It
// takes any list, because rule 14 lets a response with more than one name each
// for what it holds: items on most of them, and kinds, keys or actions on the
// two that carry several.
func carriesEntries(body map[string]json.RawMessage) bool {
	for _, value := range body {
		var list []json.RawMessage

		if json.Unmarshal(value, &list) == nil {
			return true
		}
	}

	return false
}

// TestEveryBuildWideVocabularyAnswersWithoutACredential is the decision the
// package comment states, held rather than assumed. A client branching on an
// error code needs the taxonomy before it has a credential.
//
// It runs over the build-wide sets, which is every row but the per-install one
// perInstallVocabularies names. The other direction is the case below.
func TestEveryBuildWideVocabularyAnswersWithoutACredential(t *testing.T) {
	t.Parallel()

	handler := serverWithoutIdentity(t).Handler()

	paths := []string{vocabulariesPath}

	for _, entry := range vocabularyIndex() {
		if perInstallVocabularies[entry.Name] {
			continue
		}

		paths = append(paths, entry.Path)
	}

	require.Greater(t, len(paths), 1, "no build-wide vocabulary is served, so this gate read nothing")

	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			t.Parallel()

			rec := call(t, handler, http.MethodGet, path, "")
			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		})
	}
}

// TestEveryPerInstallVocabularyRefusesAnAnonymousCaller is the other direction.
// A per-install set publishes what an operator installed rather than what the
// build ships, and ADR-20260831-nbj rules 22 and 24 keep an internal device class out
// of anything the core publishes. An anonymous reader of one would have the
// class's name, its states and the words a person sees for them.
func TestEveryPerInstallVocabularyRefusesAnAnonymousCaller(t *testing.T) {
	t.Parallel()

	handler := serverWithoutIdentity(t).Handler()

	checked := 0

	for _, entry := range vocabularyIndex() {
		if !perInstallVocabularies[entry.Name] {
			continue
		}

		checked++

		t.Run(entry.Path, func(t *testing.T) {
			t.Parallel()

			rec := call(t, handler, http.MethodGet, entry.Path, "")
			require.Equal(t, http.StatusUnauthorized, rec.Code, rec.Body.String())
		})
	}

	require.Positive(t, checked, "no per-install vocabulary is served, so this gate read nothing")
}

// perInstallVocabularies names the served sets whose entries come from what an
// operator installed. They are the exception to the package comment's reason for
// serving a vocabulary without a credential, and the list is here so adding one
// is a line in a diff (ADR-20260828-xdp rule 8's split).
//
//nolint:gochecknoglobals // the exception table two cases read, read-only after init.
var perInstallVocabularies = map[string]bool{
	devicestate.VocabularyName: true,
}

// TestTheServedErrorCodesAreTheRegistry is ADR-20260825-bhe rule 11 for the error
// taxonomy: the endpoint and the generated page read one accessor, so a code
// cannot be on one and absent from the other.
func TestTheServedErrorCodesAreTheRegistry(t *testing.T) {
	t.Parallel()

	registered := fault.Global().All()
	require.NotEmpty(t, registered, "no code is registered, so this gate would compare two empty lists")

	var body struct {
		Items []errorCodeEntry `json:"items"`
	}

	decodeVocabulary(t, vocabularyErrorCodesPath, &body)
	require.Len(t, body.Items, len(registered))

	for i, entry := range registered {
		require.Equal(t, entry.Code.String(), body.Items[i].Code)
		require.Equal(t, entry.Status, body.Items[i].Status)
		require.Equal(t, entry.Summary, body.Items[i].Summary)
		require.Equal(t, entry.Action, body.Items[i].Action)
	}
}

// TestTheServedExtensionsAreTheCatalogue is rule 11 for the extension
// catalogue: the endpoint answers with exactly what extension.Global() holds,
// which is what the generated page is built from.
//
// This binary links no adapter, so the catalogue is empty in it and this
// comparison alone would pass on two empty lists.
// TestTheExtensionProjectionCarriesTheDescriptor is the half that is not
// vacuous.
func TestTheServedExtensionsAreTheCatalogue(t *testing.T) {
	t.Parallel()

	registered := extension.Global().All()

	var body struct {
		Items []extensionEntry `json:"items"`
	}

	decodeVocabulary(t, vocabularyExtensionsPath, &body)
	require.Len(t, body.Items, len(registered))

	for i, entry := range registered {
		require.Equal(t, entry.Kind().String(), body.Items[i].Kind)
		require.Equal(t, entry.Qualified(), body.Items[i].Qualified)
		require.Equal(t, entry.Doc().Summary, body.Items[i].Summary)
	}
}

// TestTheExtensionProjectionCarriesTheDescriptor exercises the mapping against
// a descriptor built here, because the catalogue this binary links is empty and
// a projection that dropped every field would still satisfy a comparison of two
// empty lists.
func TestTheExtensionProjectionCarriesTheDescriptor(t *testing.T) {
	t.Parallel()

	descriptor, err := extension.Declare(extension.KindEventSink, connectorapi.Contract, "probe", extension.Doc{
		Summary:     "A descriptor built by a test.",
		Description: "It is never registered, so nothing outside this test sees it.",
		Version:     extension.Version("2.3.4"),
		Contract:    connectorapi.Contract,
		Stability:   extension.StabilityStable,
	})
	require.NoError(t, err)

	entries := extensionEntriesOf([]extension.Descriptor{descriptor}, noRefusal)
	require.Len(t, entries, 1)

	require.Equal(t, descriptor.Kind().String(), entries[0].Kind)
	require.Equal(t, descriptor.Name(), entries[0].Name)
	require.Equal(t, descriptor.Qualified(), entries[0].Qualified)
	require.Equal(t, descriptor.Module(), entries[0].Module)
	require.Equal(t, "A descriptor built by a test.", entries[0].Summary)
	require.Equal(t, "It is never registered, so nothing outside this test sees it.", entries[0].Description)
	require.Equal(t, "stable", entries[0].Stability)
	require.Equal(t, "2.3.4", entries[0].Version)
	require.Empty(t, entries[0].Refused, "a descriptor nothing refused carries no refusal")
}

// noRefusal is the lookup for a catalogue that refused nothing, which is what
// every deployment where the build is coherent looks like.
func noRefusal(extension.Descriptor) string { return "" }

// TestTheExtensionProjectionServesTheConfigKeys: a settings page builds its
// form from the server it is talking to, so the keys each entry reads are on
// the wire, each one saying whether what goes in it is a credential. The
// default of a credential is not served: it would put the author's own
// password in the answer that page reads.
func TestTheExtensionProjectionServesTheConfigKeys(t *testing.T) {
	t.Parallel()

	descriptor, err := extension.Declare(extension.KindEventSink, connectorapi.Contract, "probe", extension.Doc{
		Summary:     "A descriptor built by a test.",
		Description: "It is never registered, so nothing outside this test sees it.",
		Version:     extension.Version("2.3.4"),
		Contract:    connectorapi.Contract,
		Stability:   extension.StabilityStable,
		Options: []extension.Option{
			{Key: "url", Type: extension.OptionURL, Required: true, Summary: "Where each event is posted."},
			{
				Key: "password", Type: extension.OptionSecret,
				Default: "hunter2", Summary: "What it presents upstream.",
			},
			{Key: "timeout", Type: extension.OptionDuration, Default: "5s", Summary: "How long one POST waits."},
		},
	})
	require.NoError(t, err)

	entries := extensionEntriesOf([]extension.Descriptor{descriptor}, noRefusal)
	require.Len(t, entries, 1)

	require.Equal(t, []extensionOption{
		{Key: "url", Type: "url", Required: true, Summary: "Where each event is posted."},
		{Key: "password", Type: "secret", Secret: true, Summary: "What it presents upstream."},
		{Key: "timeout", Type: "duration", Default: "5s", Summary: "How long one POST waits."},
	}, entries[0].Options)
}

// TestARefusedRegistrationIsListedWithItsReason is the other half of the one
// inventory rule. Registering and being admitted are two events, and a
// catalogue that knows only the first lists a device kind the host refused
// exactly as if it were in force, while the verb table does not carry it and
// none of its commands will ever dispatch. The entry stays
// listed, because an operator whose kind has vanished needs to see why, and it
// is listed with the reason.
func TestARefusedRegistrationIsListedWithItsReason(t *testing.T) {
	t.Parallel()

	descriptor, err := extension.Declare(extension.KindDeviceKind, connectorapi.Contract, "shelf", extension.Doc{
		Summary:     "A device kind built by a test.",
		Description: "It is never registered, so nothing outside this test sees it.",
		Version:     extension.Version("1.0.0"),
		Contract:    connectorapi.Contract,
		Stability:   extension.StabilityExperimental,
	})
	require.NoError(t, err)

	const reason = "shelf declares the verb core.command.send_state_info, which is not under its module"

	entries := extensionEntriesOf([]extension.Descriptor{descriptor},
		func(d extension.Descriptor) string {
			if d.String() != descriptor.String() {
				return ""
			}

			return reason
		})
	require.Len(t, entries, 1)
	require.Equal(t, reason, entries[0].Refused)
}

// TestTheServedResourceKindsAreTheDescriptorTable is rule 11 for the resource
// kinds, and rule 8 on the served side: every kind in the closed set is in the
// response, and the response names no kind outside it.
func TestTheServedResourceKindsAreTheDescriptorTable(t *testing.T) {
	t.Parallel()

	var body struct {
		Vocabulary string              `json:"vocabulary"`
		Version    string              `json:"version"`
		Format     string              `json:"format"`
		Items      []resourceKindEntry `json:"items"`
	}

	decodeVocabulary(t, vocabularyResourceKindsPath, &body)

	require.Equal(t, ref.VocabularyName, body.Vocabulary)
	require.Equal(t, ref.VocabularyVersion, body.Version)
	require.Equal(t, ref.VocabularyFormat, body.Format)
	require.Len(t, body.Items, len(ref.Kinds()))
	require.NotEmpty(t, body.Items)

	for i, doc := range ref.KindDocs() {
		require.Equal(t, doc.Kind.String(), body.Items[i].Kind)
		require.Equal(t, doc.Summary, body.Items[i].Summary)
		require.Equal(t, doc.Description, body.Items[i].Description)
		require.Equal(t, doc.Configurable, body.Items[i].Configurable)
	}

	for _, item := range body.Items {
		require.Truef(t, ref.Kind(item.Kind).Valid(), "%s is served and is not in the closed set", item.Kind)
	}
}

// TestTheServedAuthorizationVocabularyIsTheClosedSets is rule 11 for the
// authorization graph, on all four of its halves.
func TestTheServedAuthorizationVocabularyIsTheClosedSets(t *testing.T) {
	t.Parallel()

	var body struct {
		Vocabulary string                  `json:"vocabulary"`
		Version    string                  `json:"version"`
		Format     string                  `json:"format"`
		Actions    []authorizationAction   `json:"actions"`
		Types      []authorizationType     `json:"types"`
		Relations  []authorizationRelation `json:"relations"`
		Shapes     []authorizationShape    `json:"shapes"`
	}

	decodeVocabulary(t, vocabularyAuthorizationPath, &body)

	require.Equal(t, authz.VocabularyName, body.Vocabulary)
	require.Equal(t, authz.VocabularyVersion, body.Version)
	require.Equal(t, authz.VocabularyFormat, body.Format)

	require.Len(t, body.Actions, len(authz.Actions()))
	require.Len(t, body.Types, len(authz.Types()))
	require.Len(t, body.Relations, len(authz.Relations()))
	require.Len(t, body.Shapes, len(authz.Shapes()))
	require.NotEmpty(t, body.Shapes)

	for i, doc := range authz.ActionDocs() {
		require.Equal(t, doc.Action.String(), body.Actions[i].Action)
		require.Equal(t, doc.Domain.String(), body.Actions[i].Domain)
		require.Len(t, body.Actions[i].ConferredBy, len(doc.ConferredBy))
	}

	served := make([]string, 0, len(body.Actions))
	for _, action := range body.Actions {
		served = append(served, action.Action)
	}

	for _, action := range authz.Actions() {
		require.Containsf(t, served, action.String(), "%s is in the closed set and is not served", action)
	}

	for _, action := range served {
		require.Truef(t, authz.Action(action).Valid(), "%s is served and is not in the closed set", action)
	}
}

// TestTheServedCapabilityVocabularyIsTheProfileCoverage is rule 11 for the
// capability keys: the endpoint and the generated page read the same shipped
// profile data.
func TestTheServedCapabilityVocabularyIsTheProfileCoverage(t *testing.T) {
	t.Parallel()

	registry, err := profile.Builtin()
	require.NoError(t, err)

	coverage := registry.Coverage()

	var body capabilitiesBody

	decodeVocabulary(t, vocabularyCapabilitiesPath, &body)

	require.Equal(t, connectorapi.Contract.String(), body.Contract)
	require.NotEmpty(t, body.Keys, "no capability key is served, so this gate would read nothing")

	var declared int

	for _, key := range coverage.Keys() {
		if !coverage.Has(key) {
			continue
		}

		declared++

		require.Truef(t, slices.ContainsFunc(body.Keys, func(entry capabilityKey) bool {
			return entry.Key == key.String()
		}), "%s is declared by a shipped profile and is not served", key)
	}

	require.Len(t, body.Keys, declared)

	for kind := connectorapi.KindFlag; kind.Valid(); kind++ {
		require.Truef(t, slices.ContainsFunc(body.Kinds, func(entry capabilityKind) bool {
			return entry.Kind == kind.String()
		}), "value kind %s is in the closed set and is not served", kind)
	}
}

// TestTheServedEventTypesAreTheVocabulary is rule 11 for the event types, and
// rule 8 on the served side.
func TestTheServedEventTypesAreTheVocabulary(t *testing.T) {
	t.Parallel()

	var body struct {
		Vocabulary string           `json:"vocabulary"`
		Version    string           `json:"version"`
		Namespace  string           `json:"namespace"`
		Items      []eventTypeEntry `json:"items"`
	}

	decodeVocabulary(t, vocabularyEventTypesPath, &body)

	require.Equal(t, event.VocabularyName, body.Vocabulary)
	require.Equal(t, event.VocabularyVersion, body.Version)
	require.Equal(t, event.CoreNamespace, body.Namespace)
	require.Len(t, body.Items, len(event.Types()))
	require.NotEmpty(t, body.Items)

	for i, doc := range event.TypeDocs() {
		require.Equal(t, doc.Type, body.Items[i].Type)
		require.Equal(t, doc.Class.String(), body.Items[i].Class)
		require.Equal(t, doc.Class.Durable(), body.Items[i].Durable)
		require.Equal(t, doc.Summary, body.Items[i].Summary)
	}

	for _, item := range body.Items {
		require.NoErrorf(t, event.ValidateType(item.Type),
			"%s is served and the hub would refuse it", item.Type)
	}
}

// TestTheCapabilityProfilesAreParsedOnce holds the reason the load is lazy: the
// parse costs about 309 KB of heap, and doing it per request would put that on
// every reader of a page that never changes between restarts.
func TestTheCapabilityProfilesAreParsedOnce(t *testing.T) {
	t.Parallel()

	cache := &capabilityCache{}

	first, err := cache.load()
	require.NoError(t, err)

	second, err := cache.load()
	require.NoError(t, err)

	require.NotEmpty(t, first.Keys)
	require.Equal(t, first, second)
}

// decodeVocabulary reads one vocabulary endpoint into out.
func decodeVocabulary(t *testing.T, path string, out any) {
	t.Helper()

	rec := call(t, newTestServer(t).Handler(), http.MethodGet, path, "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), out))
}

// TestTheServedDeviceModelsSayWhichIsAToolChanger holds the wire projection of
// the flag: the XL carries it and a model whose slots are MMU feeders does not.
func TestTheServedDeviceModelsSayWhichIsAToolChanger(t *testing.T) {
	t.Parallel()

	var body struct {
		Items []struct {
			Model       string `json:"model"`
			ToolChanger bool   `json:"tool_changer"`
		} `json:"items"`
	}

	decodeVocabulary(t, vocabularyDeviceModelsPath, &body)

	served := map[string]bool{}
	for _, item := range body.Items {
		served[item.Model] = item.ToolChanger
	}

	require.True(t, served["XL"])
	require.False(t, served["MK4S"])
}
