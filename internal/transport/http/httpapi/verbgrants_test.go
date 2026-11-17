package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/authz"
)

// deviceVerbs is the path the cases below drive.
const deviceVerbs = "/api/v1/grants/device/p-alpha/verbs"

// fakeVerbs is the dangerous tier as this surface reaches it. It records what
// it was asked, because what the transport is on the hook for is passing the
// path, the body and the caller through unchanged.
type fakeVerbs struct {
	written   []authz.VerbGrant
	withdrawn []authz.VerbGrant
	held      []authz.VerbGrant
	accepted  []string
	refuse    error
}

var _ VerbGrantStore = (*fakeVerbs)(nil)

func (f *fakeVerbs) VerbGrants(
	_ context.Context, _ authz.Subject, _ authz.Object,
) ([]authz.VerbGrant, error) {
	return f.held, f.refuse
}

func (f *fakeVerbs) WriteVerbGrant(
	_ context.Context, _ authz.Subject, grant authz.VerbGrant,
) (bool, error) {
	if f.refuse != nil {
		return false, f.refuse
	}

	f.written = append(f.written, grant)

	return true, nil
}

func (f *fakeVerbs) WithdrawVerbGrant(_ context.Context, _ authz.Subject, grant authz.VerbGrant) error {
	if f.refuse != nil {
		return f.refuse
	}

	f.withdrawn = append(f.withdrawn, grant)

	return nil
}

func (f *fakeVerbs) AcceptDeclarationChange(
	_ context.Context, _ authz.Subject, module string, kind authz.DeviceKind, verb, to string,
) error {
	if f.refuse != nil {
		return f.refuse
	}

	f.accepted = append(f.accepted, module+" "+kind.String()+" "+verb+" -> "+to)

	return nil
}

func (f *fakeVerbs) Acceptances(
	_ context.Context, _ authz.Subject, kind authz.DeviceKind,
) ([]authz.AcceptedChange, error) {
	if f.refuse != nil {
		return nil, f.refuse
	}

	return []authz.AcceptedChange{{
		Module: "acme.io/shelf", Kind: kind, Verb: "acme.io.shelf.command.reboot",
		To: "operate", Class: "configure",
	}}, nil
}

// VerbsOfSubject confers flash as well, on a subject holding no configure, so
// the listing has a row it must leave out: the command path checks the class
// before the row.
func (f *fakeVerbs) VerbsOfSubject(_ context.Context, _ authz.Subject) (map[string][]string, error) {
	return map[string][]string{"p-alpha": {"core.command.send_gcode", "core.command.flash"}}, f.refuse
}

func (f *fakeVerbs) SubjectDevices(
	_ context.Context, _, _ authz.Subject,
) ([]authz.DeviceRow, error) {
	return []authz.DeviceRow{{
		ID:      "p-alpha",
		Name:    "Alpha",
		Kind:    authz.DeviceKindPrinter,
		Actions: []authz.Action{authz.ActionObserve, authz.ActionOperate},
	}}, f.refuse
}

func (f *fakeVerbs) SubjectGrants(
	_ context.Context, _, subject authz.Subject,
) ([]authz.Tuple, error) {
	if f.refuse != nil {
		return nil, f.refuse
	}

	object, err := authz.NewObject(authz.TypeDevice, "p-alpha")
	if err != nil {
		return nil, err
	}

	tuple, err := authz.NewTuple(object, authz.RelBlocked, subject)
	if err != nil {
		return nil, err
	}

	return []authz.Tuple{tuple}, nil
}

// serverWithVerbs is a server whose dangerous tier is the caller's.
func serverWithVerbs(t *testing.T, verbs VerbGrantStore) *Server {
	t.Helper()

	server, err := New(Config{
		Version:    "test",
		Principals: fixedPrincipal("user:operator"),
		Resources:  seededStore(),
		Presets:    seededPresets(),
		Endpoints:  seededEndpoints(),
		Grants:     &fakeGrantStore{},
		Verbs:      verbs,
		Sessions:   newFakeSessions(),
		Accounts:   &fakeAccounts{},
		Readiness:  &fakeReadiness{report: readyReport()},
		Devices:    noPrinters{},
		Stores:     noStorages{},
		Events:     noEvents{},
	})
	require.NoError(t, err)

	return server
}

// The path parameter has to reach the store, because a row written on the empty
// device is a row on nothing at all. Huma binds by reflection and cannot write
// into an unexported embedded field, so this is the assertion that catches the
// binding rather than the semantics.
func TestAVerbRowCarriesThePathTheCallerWrote(t *testing.T) {
	t.Parallel()

	verbs := &fakeVerbs{}
	handler := serverWithVerbs(t, verbs).Handler()

	rec := call(t, handler, http.MethodPut, deviceVerbs,
		`{"verb":"core.command.send_gcode","subject":"user:alice"}`)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	require.Len(t, verbs.written, 1)
	require.Equal(t, "device:p-alpha", verbs.written[0].Object().String())
	require.Equal(t, "core.command.send_gcode", verbs.written[0].Verb())
	require.Equal(t, "user:alice", verbs.written[0].Subject().String())

	rec = call(t, handler, http.MethodDelete,
		deviceVerbs+"?verb=core.command.send_gcode&subject=user:alice", "")
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	require.Len(t, verbs.withdrawn, 1)
	require.Equal(t, "device:p-alpha", verbs.withdrawn[0].Object().String())
}

// A verb outside the grammar is refused by the constructor and never reaches
// the store, so a row nobody can spell is not a row nobody can delete.
func TestAVerbOutsideTheGrammarNeverReachesTheStore(t *testing.T) {
	t.Parallel()

	verbs := &fakeVerbs{}
	handler := serverWithVerbs(t, verbs).Handler()

	rec := call(t, handler, http.MethodPut, deviceVerbs,
		`{"verb":"NOT A VERB","subject":"user:alice"}`)
	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	require.Empty(t, verbs.written)
}

// The acceptance route carries the kind from the path and the rest from the
// body, and a body with no verb is the module-path acceptance.
func TestAnAcceptanceCarriesTheKindAndTheModule(t *testing.T) {
	t.Parallel()

	verbs := &fakeVerbs{}
	handler := serverWithVerbs(t, verbs).Handler()

	rec := call(t, handler, http.MethodPost,
		"/api/v1/grants/kinds/acme.io%2Fshelf/acceptances",
		`{"module":"github.com/acme/shelf","verb":"acme.io.shelf.command.reset","from":"configure","to":"operate"}`)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	require.Equal(t,
		[]string{"github.com/acme/shelf acme.io/shelf acme.io.shelf.command.reset -> operate"},
		verbs.accepted)
}

// An acceptance can be read back. The write side answered 201 and there was no
// reader at all, so an operator who had accepted something could not see what
// they had accepted until the server was restarted and the effect showed up, or
// did not.
func TestAnAcceptanceCanBeReadBackWithTheRecordItWasWrittenAgainst(t *testing.T) {
	t.Parallel()

	handler := serverWithVerbs(t, &fakeVerbs{}).Handler()

	rec := call(t, handler, http.MethodGet,
		"/api/v1/grants/kinds/acme.io%2Fshelf/acceptances", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var body struct {
		Items []AcceptedChange `json:"items"`
	}

	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Len(t, body.Items, 1)
	require.Equal(t, "acme.io/shelf", body.Items[0].Kind)
	require.Equal(t, "acme.io.shelf.command.reboot", body.Items[0].Verb)
	require.Equal(t, "operate", body.Items[0].To)
	require.Equal(t, "configure", body.Items[0].Class,
		"the record the acceptance was written against is rendered beside it, so a reader can see "+
			"whether it has been applied yet")
}

// What a subject holds is rendered per kind, with the verbs the classes reach.
// "Operator on a shelf" reads as nothing until the answer says which verbs that
// is, and this is the assertion that keeps the answer saying so.
func TestASubjectListingRendersTheVerbsTheClassesReach(t *testing.T) {
	t.Parallel()

	handler := serverWithVerbs(t, &fakeVerbs{}).Handler()

	rec := call(t, handler, http.MethodGet, "/api/v1/grants/subject/user:alice", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var body struct {
		Subject string        `json:"subject"`
		Devices []SubjectHold `json:"devices"`
	}

	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, "user:alice", body.Subject)
	require.Len(t, body.Devices, 1)

	held := body.Devices[0]
	require.Equal(t, "printer", held.Kind)
	require.Equal(t, []string{"observe", "operate"}, held.Actions)
	require.Contains(t, held.Verbs, "core.command.pause_print")
	require.Contains(t, held.Verbs, "core.command.send_gcode",
		"the dangerous verb a row conferred is not in what the subject reaches")
	require.NotContains(t, held.Verbs, "core.command.flash",
		"a configure verb a row conferred reached a caller holding only observe and operate")
	require.NotContains(t, held.Verbs, "core.command.force_gcode",
		"a dangerous verb nothing conferred is in what the subject reaches")
}

// The direct listing renders the grants as written, a block included, so a page
// can show the switch that lifts it.
func TestASubjectsDirectGrantsAreRenderedAsWritten(t *testing.T) {
	t.Parallel()

	handler := serverWithVerbs(t, &fakeVerbs{}).Handler()

	rec := call(t, handler, http.MethodGet, "/api/v1/grants/subject/user:alice/direct", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var body struct {
		Subject string  `json:"subject"`
		Items   []Grant `json:"items"`
	}

	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, "user:alice", body.Subject)
	require.Equal(t, []Grant{{Object: "device:p-alpha", Relation: "blocked", Subject: "user:alice"}}, body.Items)
}

func TestASubjectsDirectGrantsRefuseASubjectOutsideTheVocabulary(t *testing.T) {
	t.Parallel()

	handler := serverWithVerbs(t, &fakeVerbs{}).Handler()

	rec := call(t, handler, http.MethodGet, "/api/v1/grants/subject/nobody/direct", "")
	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
}
