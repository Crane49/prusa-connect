package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/printer/profile"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/extension"
)

// benchDialect is an inbound driver that exists for the two declaration tests
// below. It drives nothing: describeDialect builds one probe and asks which way
// the dialect is reached, and never hands it Arrivals and never sends it a
// request.
type benchDialect struct{}

func (benchDialect) Baseline() connectorapi.Set { return connectorapi.Set{} }

func (benchDialect) Handler(connectorapi.Arrivals) (http.Handler, error) {
	return http.NotFoundHandler(), nil
}

func benchDriver(connectorapi.Spec) (connectorapi.Driver, error) { return benchDialect{}, nil }

func benchDoc() extension.Doc {
	return extension.Doc{
		Summary: "One bench dialect, for a test.",
		Description: "It answers no route and talks to no machine. The tests beside it read what " +
			"its declaration says about the machines it would talk to.",
		Version:   "1.0.0",
		Contract:  connectorapi.Contract,
		Stability: extension.StabilityExperimental,
	}
}

// modelVocabulary is the whole answer, both halves, as a test reads it.
type modelVocabulary struct {
	Vocabulary string              `json:"vocabulary"`
	Items      []deviceModelEntry  `json:"items"`
	Connectors []connectorEntry    `json:"connectors"`
	Refused    []refusedModelEntry `json:"refused"`
}

func readModelVocabulary(t *testing.T) modelVocabulary {
	t.Helper()

	var body modelVocabulary
	decodeVocabulary(t, vocabularyDeviceModelsPath, &body)

	return body
}

// TestEveryModelNamesADialectTheAnswerDescribes is ADR-20260825-bhe rule 8 across the
// two halves of this set, both ways round.
//
// A model row naming a dialect the connectors half does not carry leaves a page
// unable to say how that machine is reached, which is the whole question the
// dialog asks. A connectors row nothing reaches is a choice a page would draw
// with no machine behind it.
func TestEveryModelNamesADialectTheAnswerDescribes(t *testing.T) {
	t.Parallel()

	body := readModelVocabulary(t)

	require.NotEmpty(t, body.Items, "no model is served, so this gate would compare two empty sets")
	require.NotEmpty(t, body.Connectors)

	described := map[string]connectorEntry{}
	for _, entry := range body.Connectors {
		require.NotEmpty(t, entry.Connector, "a connector row carries no name")
		require.NotContainsf(t, described, entry.Connector,
			"%s has two rows in the connectors half", entry.Connector)

		described[entry.Connector] = entry
	}

	named := map[string]bool{}

	for _, item := range body.Items {
		require.NotEmptyf(t, item.Connector, "%s names no dialect", item.Model)
		require.Containsf(t, described, item.Connector,
			"%s is reached over %s and the answer describes no such dialect", item.Model, item.Connector)

		named[item.Connector] = true
	}

	// The other direction. A row is legitimate either because a model names it
	// or because this build registered it, and a row that is neither came from
	// nowhere.
	for _, entry := range body.Connectors {
		require.Truef(t, named[entry.Connector] || entry.Available,
			"%s is neither named by a model nor carried by this build", entry.Connector)
	}
}

// TestEveryDialectRowSaysWhatItNeedsAndWhichWayItIsReached refuses a row a page
// cannot draw a form from. The needs word chooses the form, so an empty one or a
// word outside the closed set leaves the dialog with nothing to put on step two.
func TestEveryDialectRowSaysWhatItNeedsAndWhichWayItIsReached(t *testing.T) {
	t.Parallel()

	body := readModelVocabulary(t)

	allowed := map[string]bool{}
	for _, word := range connectorapi.NeedsWords() {
		allowed[word.String()] = true
	}

	for _, entry := range body.Connectors {
		require.Containsf(t, allowed, entry.Needs,
			"%s needs %q, which is not a word this build's pages know", entry.Connector, entry.Needs)
		require.NotEmptyf(t, entry.Kind, "%s says what kind of device it speaks to nowhere", entry.Connector)

		if !entry.Available {
			continue
		}

		require.Truef(t, entry.Dials || entry.Arrives,
			"%s is carried by this build and is reached in neither direction", entry.Connector)
		require.NotEmptyf(t, entry.Qualified, "%s is carried and carries no qualified name", entry.Connector)
	}
}

// TestTheDialectsThisBuildCarriesAreTheRegistry is rule 11 for the half that is
// read from a registry rather than from a table: the answer says a dialect is
// available exactly when this binary registered it.
func TestTheDialectsThisBuildCarriesAreTheRegistry(t *testing.T) {
	t.Parallel()

	types, err := connectorapi.NewRegistry()
	require.NoError(t, err)

	registered := map[string]bool{}
	for _, registration := range types.Types() {
		registered[registration.Name().String()] = true
	}

	for _, entry := range readModelVocabulary(t).Connectors {
		require.Equalf(t, registered[entry.Connector], entry.Available,
			"%s is served as available=%v and the registry says otherwise", entry.Connector, entry.Available)
	}
}

// TestADeclaredDialectDescribesItself is the path T7 moved the brands onto: a
// driver declares them beside its factory, and this package serves what the
// declaration says instead of a table of dialect names it keeps itself.
//
// The registry under go test holds no dialect, so the declaration is made here
// and read back through the same function the route uses.
func TestADeclaredDialectDescribesItself(t *testing.T) {
	t.Parallel()

	registration, err := connectorapi.Declare("bench", benchDoc(), benchDriver,
		connectorapi.Dialect{
			Brands: []string{"Bench Works"},
			Needs:  connectorapi.NeedsSettingsFile,
		})
	require.NoError(t, err)

	entry := connectorEntry{Needs: connectorapi.NeedsUnknown.String()}
	describeDialect(&entry, registration)

	require.Equal(t, []string{"Bench Works"}, entry.Brands)
	require.Equal(t, connectorapi.NeedsSettingsFile.String(), entry.Needs)
	require.Equal(t, "One bench dialect, for a test.", entry.Summary)
}

// TestADialectThatDeclaresNoNeedsWordIsAnsweredFromItsDirection is the fallback
// for a driver whose author wrote no word. The driver itself settles it: a
// machine this server dials has to be told where, and one that dials in does
// not.
func TestADialectThatDeclaresNoNeedsWordIsAnsweredFromItsDirection(t *testing.T) {
	t.Parallel()

	registration, err := connectorapi.Declare("bench", benchDoc(), benchDriver)
	require.NoError(t, err)

	entry := connectorEntry{Needs: connectorapi.NeedsUnknown.String()}
	describeDialect(&entry, registration)

	require.True(t, entry.Available)
	require.True(t, entry.Arrives)
	require.Equal(t, connectorapi.NeedsNothing.String(), entry.Needs)
}

// benchModels is the catalogue a build composed with the bench dialect serves:
// the core's rows and the one model the bench driver declared.
func benchModels(t *testing.T) profile.Catalogue {
	t.Helper()

	catalogue, err := profile.Place(profile.ModelDoc{
		Model:       "Bench_One",
		DisplayName: "Bench Works Tool Changer",
		Vendor:      "Bench Works",
		Icon:        profile.IconGeneric,
		Connector:   "bench",
		ToolChanger: true,
	})
	require.NoError(t, err)
	require.Empty(t, catalogue.Refused())

	return catalogue
}

// serverWithModels is a server whose host placed the given catalogue.
func serverWithModels(t *testing.T, models profile.Catalogue) *Server {
	t.Helper()

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
		DeviceModels: models,
	})
	require.NoError(t, err)

	return server
}

// TestADeclaredModelIsServedUnderItsDialect is ADR-20261016-qrp rule 6: the route
// serves a model a driver declared beside the core's rows, with the flag that
// makes a page draw a row per tool, and the connectors half names it under the
// dialect that declared it.
func TestADeclaredModelIsServedUnderItsDialect(t *testing.T) {
	t.Parallel()

	rec := call(t, serverWithModels(t, benchModels(t)).Handler(), http.MethodGet, vocabularyDeviceModelsPath, "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var body modelVocabulary
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))

	require.Contains(t, body.Items, deviceModelEntry{
		Model:       "Bench_One",
		DisplayName: "Bench Works Tool Changer",
		Vendor:      "Bench Works",
		Kind:        "printer",
		Icon:        profile.IconGeneric,
		Connector:   "bench",
		ToolChanger: true,
	})

	registration, err := connectorapi.Declare("bench", benchDoc(), benchDriver)
	require.NoError(t, err)

	for _, entry := range connectorEntriesFrom(benchModels(t), registration) {
		if entry.Connector != "bench" {
			continue
		}

		require.True(t, entry.Available, "the dialect that declared the model is in this build")
		require.Equal(t, []string{"Bench_One"}, entry.Models)

		return
	}

	t.Fatal("the connectors half has no row for the dialect that declared a model")
}

// TestARefusedModelIsServedWithItsReason is ADR-20261016-qrp rule 5 as a reader of the
// vocabulary meets it. The refused model has no row, and the answer says which
// driver declared it, what it collided with and what to change, so a test of a
// composed build reads the refusal from the API and not from the log.
func TestARefusedModelIsServedWithItsReason(t *testing.T) {
	t.Parallel()

	catalogue, err := profile.Place(profile.ModelDoc{
		Model:       "MK4S",
		DisplayName: "Bench Works MK4S",
		Vendor:      "Bench Works",
		Icon:        profile.IconGeneric,
		Connector:   "bench",
		DeclaredBy:  "com.example.aux.bench/bench",
	})
	require.NoError(t, err)

	rec := call(t, serverWithModels(t, catalogue).Handler(), http.MethodGet, vocabularyDeviceModelsPath, "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var body modelVocabulary
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))

	require.Equal(t, []refusedModelEntry{{
		Model:        "MK4S",
		Connector:    "bench",
		Qualified:    "com.example.aux.bench/bench",
		CollidesWith: `the Prusa model "MK4S"`,
		Remedy:       profile.Refusal{Profile: "MK4S"}.Remedy(),
	}}, body.Refused)

	for _, item := range body.Items {
		require.NotEqual(t, "Bench Works", item.Vendor, "the refused row was served")
	}
}

// TestABuildWithNoRefusalServesAnEmptyList holds the member to a list, so a
// client reads its length and never meets a null.
func TestABuildWithNoRefusalServesAnEmptyList(t *testing.T) {
	t.Parallel()

	rec := call(t, serverWithModels(t, benchModels(t)).Handler(), http.MethodGet, vocabularyDeviceModelsPath, "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), `"refused":[]`)
}
