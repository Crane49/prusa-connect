package httpapi

import (
	"context"
	"encoding/json"
	"maps"
	"net/http"
	"slices"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpfault"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// fakePresetStore is an in-memory stand-in for the overlay's preset section. It
// records which principal reached each method, so a test can prove that the run
// path read a preset without asking the preset kind anything.
type fakePresetStore struct {
	mu sync.Mutex

	presets map[string]GCodePreset
	version string

	// mayRead and mayWrite are the two grants ADR-20260825-z65 rule 16 separates,
	// keyed by principal.
	mayRead  map[string]bool
	mayWrite map[string]bool

	lookups []string
}

var _ GCodePresetStore = (*fakePresetStore)(nil)

func seededPresets() *fakePresetStore {
	return &fakePresetStore{
		version:  "v1",
		mayRead:  map[string]bool{"user:operator": true},
		mayWrite: map[string]bool{"user:operator": true},
		presets: map[string]GCodePreset{
			"purge": {
				GCodePresetSpec: GCodePresetSpec{
					Summary: "Home, heat and purge.",
					Steps:   []string{"G28", "M104 S215"},
				},
				Name:    "purge",
				Version: "v1",
			},
			"chamber": {
				GCodePresetSpec: GCodePresetSpec{
					Summary: "Preheat the chamber.",
					Models:  []string{"CoreOne"},
					Steps:   []string{"M141 S40"},
				},
				Name:    "chamber",
				Version: "v1",
			},
			"mk4-only": {
				GCodePresetSpec: GCodePresetSpec{
					Models: []string{"MK4", "MK4S"},
					Steps:  []string{"G28"},
				},
				Name:    "mk4-only",
				Version: "v1",
			},
		},
	}
}

func (s *fakePresetStore) ListPresets(_ context.Context, principal string) (GCodePresetListing, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	listing := GCodePresetListing{Version: s.version, Items: []GCodePreset{}}
	if !s.mayRead[principal] {
		return listing, nil
	}

	for _, name := range slices.Sorted(maps.Keys(s.presets)) {
		listing.Items = append(listing.Items, s.presets[name])
	}

	return listing, nil
}

func (s *fakePresetStore) GetPreset(_ context.Context, principal, name string) (GCodePreset, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	stored, ok := s.presets[name]
	if !ok || !s.mayRead[principal] {
		return GCodePreset{}, httpfault.ResourceNotFound(httpfault.Reference("gcode-preset", name))
	}

	return stored, nil
}

func (s *fakePresetStore) ApplyPreset(
	_ context.Context, principal, name string, desired GCodePresetSpec, pre Precondition,
) (GCodePresetResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, exists := s.presets[name]

	if !s.mayWrite[principal] {
		if s.mayRead[principal] {
			return GCodePresetResult{}, fault.New(authz.CodeAccessDenied(),
				"this caller may read presets and may not write them")
		}

		return GCodePresetResult{}, httpfault.ResourceNotFound(httpfault.Reference("gcode-preset", name))
	}

	if !exists && !pre.MustNotExist && len(pre.IfNoneMatch) == 0 {
		return GCodePresetResult{}, httpfault.ResourceNotFound(httpfault.Reference("gcode-preset", name))
	}

	if !pre.Holds(exists, s.version) {
		return GCodePresetResult{}, httpfault.VersionConflict(httpfault.Reference("gcode-preset", name), s.version)
	}

	stored := GCodePreset{GCodePresetSpec: desired, Name: name, Version: s.version + "+"}

	if pre.DryRun {
		return GCodePresetResult{Preset: stored, Created: !exists}, nil
	}

	s.version += "+"
	stored.Version = s.version
	s.presets[name] = stored

	return GCodePresetResult{Preset: stored, Created: !exists}, nil
}

func (s *fakePresetStore) DeletePreset(
	_ context.Context, principal, name string, pre Precondition,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, exists := s.presets[name]

	if !s.mayWrite[principal] {
		return httpfault.ResourceNotFound(httpfault.Reference("gcode-preset", name))
	}

	if !exists {
		return httpfault.ResourceNotFound(httpfault.Reference("gcode-preset", name))
	}

	if !pre.Holds(exists, s.version) {
		return httpfault.VersionConflict(httpfault.Reference("gcode-preset", name), s.version)
	}

	if pre.DryRun {
		return nil
	}

	delete(s.presets, name)
	s.version += "+"

	return nil
}

func (s *fakePresetStore) LookupPreset(_ context.Context, name string) (GCodePreset, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.lookups = append(s.lookups, name)

	stored, ok := s.presets[name]
	if !ok {
		return GCodePreset{}, httpfault.ResourceNotFound(httpfault.Reference("gcode-preset", name))
	}

	return stored, nil
}

func presetServer(t *testing.T, fleet DeviceFleet, grants GrantStore, presets *fakePresetStore) *Server {
	t.Helper()

	server, err := New(Config{
		Version:    "test",
		Principals: fixedPrincipal("user:operator"),
		Resources:  seededStore(),
		Presets:    presets,
		Endpoints:  seededEndpoints(),
		Grants:     grants,
		Sessions:   newFakeSessions(),
		Accounts:   &fakeAccounts{},
		Readiness:  &fakeReadiness{report: readyReport()},
		Devices:    fleet,
		Stores:     noStorages{},
		Events:     noEvents{},
		Mounts:     devMounts(),
	})
	require.NoError(t, err)

	return server
}

func decodePreset(t *testing.T, body []byte) GCodePreset {
	t.Helper()

	var preset GCodePreset
	require.NoError(t, json.Unmarshal(body, &preset))

	return preset
}

// TestAPresetIsListedReadAndWrittenThroughItsOwnSurface is the ordinary story
// end to end: a manager writes a preset, reads it back, and sees it in the
// listing under the token a write has to quote.
func TestAPresetIsListedReadAndWrittenThroughItsOwnSurface(t *testing.T) {
	t.Parallel()

	presets := seededPresets()
	server := presetServer(t, &gcodeFleetStub{printer: devPrinter()}, gcodeManager(), presets)

	rec := write(t, server.Handler(), http.MethodPut, "/api/v1/gcode-presets/level",
		http.Header{"If-None-Match": {anyTag}},
		`{"summary":"Level the bed.","steps":["G28","G29"],"models":["MK4"]}`)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	require.Equal(t, "/api/v1/gcode-presets/level", rec.Header().Get("Location"))
	require.NotEmpty(t, rec.Header().Get("ETag"))

	written := decodePreset(t, rec.Body.Bytes())
	require.Equal(t, "level", written.Name)
	require.Equal(t, []string{"G28", "G29"}, written.Steps)

	rec = call(t, server.Handler(), http.MethodGet, "/api/v1/gcode-presets/level", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, "Level the bed.", decodePreset(t, rec.Body.Bytes()).Summary)

	etag := rec.Header().Get("ETag")
	require.NotEmpty(t, etag)

	rec = call(t, server.Handler(), http.MethodGet, "/api/v1/gcode-presets", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var listing struct {
		Items []GCodePreset `json:"items"`
	}

	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &listing))
	require.Len(t, listing.Items, 4)
}

// TestAPresetWriteWithNoTokenIsRefused holds the preset surface to the fencing
// rule the resource surface already has. A client that read the file a minute
// ago must not overwrite what somebody wrote since.
func TestAPresetWriteWithNoTokenIsRefused(t *testing.T) {
	t.Parallel()

	presets := seededPresets()
	server := presetServer(t, &gcodeFleetStub{printer: devPrinter()}, gcodeManager(), presets)

	for _, method := range []string{http.MethodPut, http.MethodDelete} {
		body := ""
		if method == http.MethodPut {
			body = `{"steps":["G28"]}`
		}

		rec := call(t, server.Handler(), method, "/api/v1/gcode-presets/purge", body)
		require.Equal(t, http.StatusPreconditionRequired, rec.Code, "%s: %s", method, rec.Body.String())
	}
}

// TestAPresetRunsOnAPrinterThroughTheSameStopRule is ADR-20260825-z65 rules 11 and 17:
// the preset is expanded, every step goes through the path a raw sequence
// takes, and the run stops at the first refusal with the rest reported as not
// attempted.
func TestAPresetRunsOnAPrinterThroughTheSameStopRule(t *testing.T) {
	t.Parallel()

	presets := seededPresets()
	presets.presets["long"] = GCodePreset{
		GCodePresetSpec: GCodePresetSpec{Steps: []string{"G28", "M109 S215", "G1 E10"}},
		Name:            "long",
		Version:         "v1",
	}

	fleet := &gcodeFleetStub{
		printer: devPrinter(),
		refuseAt: map[string]error{
			"M109 S215": connectorapi.GCodeNotAllowed.New("this server never sends that gcode word"),
		},
	}

	server := presetServer(t, fleet, gcodeManager(), presets)

	rec := call(t, server.Handler(), http.MethodPost,
		"/api/v1/devices/pdeadbeef/gcode/presets/long", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	outcome := decodeOutcome(t, rec.Body.Bytes())
	require.True(t, outcome.Stopped)
	require.Equal(t, 1, outcome.Completed)
	require.Len(t, outcome.Steps, 3)
	require.Equal(t, stepAccepted, outcome.Steps[0].Status)
	require.Equal(t, stepRefused, outcome.Steps[1].Status)
	require.Equal(t, stepNotAttempted, outcome.Steps[2].Status)

	require.Len(t, fleet.received, 2, "a step after the refusal reached the device plane")
}

// TestAPresetPinnedToAnotherModelIsRefusedAndSendsNothing is the decision
// ADR-20260825-z65 rule 15 left open: a preset that names models the target printer is
// not is refused, and nothing reaches the machine.
//
// The code is the preset's own and not the schema refusal every malformed
// request answers with, and it carries both model lists, because that is what a
// client offering the presets that do run here has to read.
func TestAPresetPinnedToAnotherModelIsRefusedAndSendsNothing(t *testing.T) {
	t.Parallel()

	presets := seededPresets()
	fleet := &gcodeFleetStub{printer: devPrinter()}
	server := presetServer(t, fleet, gcodeManager(), presets)

	rec := call(t, server.Handler(), http.MethodPost,
		"/api/v1/devices/pdeadbeef/gcode/presets/mk4-only", "")

	body := requireProblem(t, rec, http.StatusUnprocessableEntity)
	require.Equal(t, fault.PrinterPresetModelMismatch.Code(), body.Code)
	require.NotEqual(t, httpfault.CodeRequestInvalid(), body.Code,
		"a preset refused for its model is not a request that failed the schema")
	require.Equal(t, "COREONE", paramString(t, body.Params, "printer_model"))
	require.Equal(t, "MK4, MK4S", paramString(t, body.Params, "preset_models"))
	require.Empty(t, fleet.received, "a preset for another model reached the printer")
}

// TestAPresetPinnedToTheTargetModelRunsWhateverTheCase covers the other side of
// the same bound: a printer reports COREONE and the preset was written CoreOne.
func TestAPresetPinnedToTheTargetModelRunsWhateverTheCase(t *testing.T) {
	t.Parallel()

	presets := seededPresets()
	fleet := &gcodeFleetStub{printer: devPrinter()}
	server := presetServer(t, fleet, gcodeManager(), presets)

	rec := call(t, server.Handler(), http.MethodPost,
		"/api/v1/devices/pdeadbeef/gcode/presets/chamber", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	require.Len(t, fleet.received, 1)
	require.Equal(t, "M141 S40", fleet.received[0].Args["gcode"])
}

// TestAPresetNamingNoModelRunsOnAnyPrinter is rule 15's first sentence.
func TestAPresetNamingNoModelRunsOnAnyPrinter(t *testing.T) {
	t.Parallel()

	presets := seededPresets()
	fleet := &gcodeFleetStub{printer: devPrinter()}
	server := presetServer(t, fleet, gcodeManager(), presets)

	rec := call(t, server.Handler(), http.MethodPost,
		"/api/v1/devices/pdeadbeef/gcode/presets/purge", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Len(t, fleet.received, 2)
}

// TestACallerWhoMayRunAPresetNeedNotBeAbleToEditIt is the first half of the
// authorization split. An operator trusted with a maintenance macro is not
// thereby trusted to rewrite what that macro sends.
func TestACallerWhoMayRunAPresetNeedNotBeAbleToEditIt(t *testing.T) {
	t.Parallel()

	presets := seededPresets()
	presets.mayRead = map[string]bool{}
	presets.mayWrite = map[string]bool{}

	fleet := &gcodeFleetStub{printer: devPrinter()}
	server := presetServer(t, fleet, gcodeManager(), presets)

	rec := call(t, server.Handler(), http.MethodPost,
		"/api/v1/devices/pdeadbeef/gcode/presets/purge", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Len(t, fleet.received, 2, "a caller holding send_gcode could not run a preset")

	rec = write(t, server.Handler(), http.MethodPut, "/api/v1/gcode-presets/purge",
		http.Header{"If-Match": {`"v1"`}}, `{"steps":["M502"]}`)
	require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())

	rec = call(t, server.Handler(), http.MethodGet, "/api/v1/gcode-presets/purge", "")
	require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
}

// TestACallerWhoMayEditAPresetNeedNotBeAbleToRunIt is the other half. Holding
// configure_resource on the preset kind grants nothing on any printer.
func TestACallerWhoMayEditAPresetNeedNotBeAbleToRunIt(t *testing.T) {
	t.Parallel()

	presets := seededPresets()
	fleet := &gcodeFleetStub{printer: devPrinter()}

	// The caller holds operate on the printer and not send_gcode, which is
	// the operator ADR-20260825-z65 rule 8 keeps raw gcode away from.
	server := presetServer(t, fleet, commandOnly(), presets)

	rec := call(t, server.Handler(), http.MethodGet, "/api/v1/gcode-presets/purge", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	rec = call(t, server.Handler(), http.MethodPost,
		"/api/v1/devices/pdeadbeef/gcode/presets/purge", "")

	body := requireProblem(t, rec, http.StatusForbidden)
	require.Equal(t, authz.CodeAccessDenied(), body.Code)
	require.Empty(t, fleet.received, "a caller without send_gcode reached the printer through a preset")
	require.Empty(t, presets.lookups,
		"the preset was read before the printer was authorized, which tells a stranger a name is taken")
}

// TestRunningAPresetOnAPrinterTheCallerCannotSeeIsRefusedAsAbsent keeps the
// printer surface's absence rule: a printer nothing answers to is 404 and never
// a report of steps nobody ran.
func TestRunningAPresetOnAPrinterTheCallerCannotSeeIsRefusedAsAbsent(t *testing.T) {
	t.Parallel()

	presets := seededPresets()
	fleet := &gcodeFleetStub{printer: devPrinter()}
	server := presetServer(t, fleet, gcodeManager(), presets)

	rec := call(t, server.Handler(), http.MethodPost,
		"/api/v1/devices/pnothing/gcode/presets/purge", "")
	requireProblem(t, rec, http.StatusNotFound)
	require.Empty(t, fleet.received)
	require.Empty(t, presets.lookups,
		"a caller naming a printer that is not there learned whether a preset name is taken")
}

// TestRunningAPresetThatIsNotThereIsRefusedAfterThePrinterIsAuthorized covers
// the missing name, and pins the order: the printer grant is checked first.
func TestRunningAPresetThatIsNotThereIsRefusedAfterThePrinterIsAuthorized(t *testing.T) {
	t.Parallel()

	presets := seededPresets()
	fleet := &gcodeFleetStub{printer: devPrinter()}
	server := presetServer(t, fleet, gcodeManager(), presets)

	rec := call(t, server.Handler(), http.MethodPost,
		"/api/v1/devices/pdeadbeef/gcode/presets/nothing-here", "")

	body := requireProblem(t, rec, http.StatusNotFound)
	require.Equal(t, httpfault.CodeResourceNotFound(), body.Code)
	require.Equal(t, []string{"nothing-here"}, presets.lookups)
	require.Empty(t, fleet.received)
}

// TestAServerWithNoPresetStoreDoesNotStart keeps the published document
// independent of how the process was wired, which is the rule every other
// dependency on this surface follows.
func TestAServerWithNoPresetStoreDoesNotStart(t *testing.T) {
	t.Parallel()

	_, err := New(Config{
		Version:    "test",
		Principals: fixedPrincipal("user:operator"),
		Resources:  seededStore(),
		Grants:     &fakeGrantStore{},
		Sessions:   newFakeSessions(),
		Accounts:   &fakeAccounts{},
		Readiness:  &fakeReadiness{report: readyReport()},
		Devices:    noPrinters{},
		Stores:     noStorages{},
		Events:     noEvents{},
	})

	require.ErrorIs(t, err, ErrNoGCodePresetStore)
}
