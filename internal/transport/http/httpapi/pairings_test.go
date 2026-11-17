package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// The pairing the fake store starts with.
const (
	pairedProvider = "prusa-account"
	pairedSubject  = "8f1e1b2c-0000-4000-8000-abcdefabcdef"
	pairedPath     = pairingsPath + "/" + pairedProvider + "/" + pairedSubject
)

// fakePairings is a pairing store that records what it was asked, so a test
// holds the surface to asking the store rather than answering by itself.
type fakePairings struct {
	mu sync.Mutex

	rows     map[string]Pairing
	paired   []NewPairing
	listed   []string
	unpaired []string

	// fences is what Fence answers for each subject, and fenceReads is every
	// subject it was asked about, in order.
	fences     map[string]int64
	fenceReads []string
}

// Fence answers the generation the fake was given for the subject.
func (p *fakePairings) Fence(_ context.Context, subject string) (int64, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.fenceReads = append(p.fenceReads, subject)

	return p.fences[subject], nil
}

var _ Pairings = (*fakePairings)(nil)

func newFakePairings() *fakePairings {
	return &fakePairings{rows: map[string]Pairing{
		pairedProvider + "/" + pairedSubject: {
			Provider: pairedProvider, Subject: pairedSubject, Principal: "user:operator",
			Label: "the owner", CreatedAt: examplePairedAt,
		},
	}}
}

func (p *fakePairings) Pair(_ context.Context, spec NewPairing) (Pairing, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.paired = append(p.paired, spec)

	key := spec.Provider + "/" + spec.Subject
	if _, taken := p.rows[key]; taken {
		return Pairing{}, fault.AuthPairingExists.New("already paired").With(
			fault.StringParam("provider", spec.Provider), fault.StringParam("subject", spec.Subject))
	}

	row := Pairing{Provider: spec.Provider, Subject: spec.Subject, Principal: spec.Principal,
		Label: spec.Label, CreatedAt: examplePairedAt}
	p.rows[key] = row

	return row, nil
}

func (p *fakePairings) List(_ context.Context, provider string) ([]Pairing, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.listed = append(p.listed, provider)

	items := make([]Pairing, 0)

	for _, row := range p.rows {
		if provider == "" || row.Provider == provider {
			items = append(items, row)
		}
	}

	return items, nil
}

func (p *fakePairings) Unpair(_ context.Context, provider, subject string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	key := provider + "/" + subject
	p.unpaired = append(p.unpaired, key)

	if _, ok := p.rows[key]; !ok {
		return UnknownPairing(provider, subject)
	}

	delete(p.rows, key)

	return nil
}

// serverWithPairings is a server whose pairing store and graph are the
// caller's, resolving every request to user:operator.
func serverWithPairings(t *testing.T, pairings Pairings, grants GrantStore) *Server {
	t.Helper()

	server, err := New(Config{
		Version:    "test",
		Principals: fixedPrincipal("user:operator"),
		Resources:  seededStore(),
		Presets:    seededPresets(),
		Endpoints:  seededEndpoints(),
		Grants:     grants,
		Sessions:   newFakeSessions(),
		Accounts:   &fakeAccounts{},
		Pairings:   pairings,
		Readiness:  &fakeReadiness{report: readyReport()},
		Devices:    noPrinters{},
		Stores:     noStorages{},
		Events:     noEvents{},
	})
	require.NoError(t, err)

	return server
}

// TestPairingASubjectTakesTheAuthResourceKind is the permit and the refuse on
// the create route: with configure_resource on the auth kind the pairing is
// written and the graph was asked; without it nothing is written.
func TestPairingASubjectTakesTheAuthResourceKind(t *testing.T) {
	t.Parallel()

	body := `{"provider":"prusa-account","subject":"c2a4d6e8-0000-4000-8000-fedcbafedcba",` +
		`"principal":"service_account:wall-dashboard","label":"the dashboard"}`

	t.Run("permitted", func(t *testing.T) {
		t.Parallel()

		pairings := newFakePairings()
		grants := &grantingStore{allow: true}

		rec := call(t, serverWithPairings(t, pairings, grants).Handler(), http.MethodPost, pairingsPath, body)
		require.Equal(t, http.StatusCreated, rec.Code, "body: %s", rec.Body.String())

		var paired Pairing
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &paired))
		require.Equal(t, "service_account:wall-dashboard", paired.Principal)
		require.Equal(t, "the dashboard", paired.Label)

		require.NotEmpty(t, grants.asked, "the surface answered without asking the graph")
		require.Equal(t, "resource_kind:auth", grants.asked[0].String())
		require.Equal(t, []NewPairing{{
			Provider: "prusa-account", Subject: "c2a4d6e8-0000-4000-8000-fedcbafedcba",
			Principal: "service_account:wall-dashboard", Label: "the dashboard", Generation: new(int64),
			CreatedBy: "user:operator", CreatorGeneration: new(int64),
			RootCreator: "user:operator", RootGeneration: new(int64),
		}}, pairings.paired)
	})

	t.Run("refused", func(t *testing.T) {
		t.Parallel()

		pairings := newFakePairings()

		rec := call(t, serverWithPairings(t, pairings, &grantingStore{allow: false}).Handler(),
			http.MethodPost, pairingsPath, body)

		problem := requireProblem(t, rec, http.StatusForbidden)
		require.Equal(t, authz.CodeAccessDenied(), problem.Code)
		require.Empty(t, pairings.paired, "a refused caller wrote a pairing")
	})
}

// TestOnlyAPersonOrAServiceAccountCanBePaired is the refusal for a principal a
// bearer token could never speak for, and for a subject already paired.
func TestOnlyAPersonOrAServiceAccountCanBePaired(t *testing.T) {
	t.Parallel()

	for name, principal := range map[string]string{
		"a device":  "device_credential:mk4-01",
		"a group":   "group:operators",
		"a userset": "group:operators#member",
		"rubbish":   "not a subject",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			pairings := newFakePairings()
			body := `{"provider":"prusa-account","subject":"s","principal":"` + principal + `"}`

			rec := call(t, serverWithPairings(t, pairings, &grantingStore{allow: true}).Handler(),
				http.MethodPost, pairingsPath, body)

			requireProblem(t, rec, http.StatusUnprocessableEntity)
			require.Empty(t, pairings.paired, "a principal nothing can speak for was paired")
		})
	}

	t.Run("already paired", func(t *testing.T) {
		t.Parallel()

		body := `{"provider":"` + pairedProvider + `","subject":"` + pairedSubject + `","principal":"user:mallory"}`

		rec := call(t, serverWithPairings(t, newFakePairings(), &grantingStore{allow: true}).Handler(),
			http.MethodPost, pairingsPath, body)

		problem := requireProblem(t, rec, http.StatusConflict)
		require.Equal(t, fault.AuthPairingExists.Code(), problem.Code)
	})
}

// TestListingAndUnpairingTakeTheAuthResourceKind covers the other two routes in
// both directions, and the not-found for a subject nobody paired.
func TestListingAndUnpairingTakeTheAuthResourceKind(t *testing.T) {
	t.Parallel()

	t.Run("list permitted", func(t *testing.T) {
		t.Parallel()

		pairings := newFakePairings()

		rec := call(t, serverWithPairings(t, pairings, &grantingStore{allow: true}).Handler(),
			http.MethodGet, pairingsPath+"?provider=prusa-account", "")
		require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

		var body struct {
			Items []Pairing `json:"items"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		require.Len(t, body.Items, 1)
		require.Equal(t, pairedSubject, body.Items[0].Subject)
		require.Equal(t, []string{"prusa-account"}, pairings.listed)
	})

	t.Run("list refused", func(t *testing.T) {
		t.Parallel()

		pairings := newFakePairings()

		rec := call(t, serverWithPairings(t, pairings, &grantingStore{allow: false}).Handler(),
			http.MethodGet, pairingsPath, "")

		requireProblem(t, rec, http.StatusForbidden)
		require.Empty(t, pairings.listed, "a refused caller read the listing")
	})

	t.Run("unpair permitted", func(t *testing.T) {
		t.Parallel()

		pairings := newFakePairings()

		rec := call(t, serverWithPairings(t, pairings, &grantingStore{allow: true}).Handler(),
			http.MethodDelete, pairedPath, "")
		require.Equal(t, http.StatusNoContent, rec.Code, "body: %s", rec.Body.String())
		require.Equal(t, []string{pairedProvider + "/" + pairedSubject}, pairings.unpaired)
	})

	t.Run("unpair refused", func(t *testing.T) {
		t.Parallel()

		pairings := newFakePairings()

		rec := call(t, serverWithPairings(t, pairings, &grantingStore{allow: false}).Handler(),
			http.MethodDelete, pairedPath, "")

		requireProblem(t, rec, http.StatusForbidden)
		require.Empty(t, pairings.unpaired, "a refused caller removed a pairing")
	})

	t.Run("unpair unknown", func(t *testing.T) {
		t.Parallel()

		rec := call(t, serverWithPairings(t, newFakePairings(), &grantingStore{allow: true}).Handler(),
			http.MethodDelete, pairingsPath+"/prusa-account/nobody", "")

		requireProblem(t, rec, http.StatusNotFound)
	})
}

// TestAServerWiredWithoutPairingsHoldsNone is the default: nothing to list,
// nothing to remove, and a write is refused rather than kept for nobody.
func TestAServerWiredWithoutPairingsHoldsNone(t *testing.T) {
	t.Parallel()

	server := serverWithPairings(t, nil, &grantingStore{allow: true})

	rec := call(t, server.Handler(), http.MethodGet, pairingsPath, "")
	require.Equal(t, http.StatusOK, rec.Code)
	require.JSONEq(t, `{"items":[]}`, rec.Body.String())

	rec = call(t, server.Handler(), http.MethodPost, pairingsPath,
		`{"provider":"prusa-account","subject":"s","principal":"user:operator"}`)
	requireProblem(t, rec, http.StatusUnprocessableEntity)

	rec = call(t, server.Handler(), http.MethodDelete, pairedPath, "")
	requireProblem(t, rec, http.StatusNotFound)
}

// TestAPairingSavesTheGenerationItReadBeforeTheSessionCheck holds the handler
// to the order the fence needs, as the mint is held. It reads the principal's
// generation, checks the session, and saves what it read.
func TestAPairingSavesTheGenerationItReadBeforeTheSessionCheck(t *testing.T) {
	t.Parallel()

	pairings := newFakePairings()
	pairings.fences = map[string]int64{"user:grace": 5, "user:operator": 8, "user:ada": 13}

	credentials := &orderedSession{pairings: pairings, root: "user:ada"}

	server, err := New(Config{
		Version:     "test",
		Principals:  fixedPrincipal("user:operator"),
		Resources:   seededStore(),
		Presets:     seededPresets(),
		Endpoints:   seededEndpoints(),
		Grants:      &grantingStore{allow: true},
		Sessions:    newFakeSessions(),
		Accounts:    &fakeAccounts{},
		Credentials: credentials,
		Pairings:    pairings,
		Readiness:   &fakeReadiness{report: readyReport()},
		Devices:     noPrinters{},
		Stores:      noStorages{},
		Events:      noEvents{},
	})
	require.NoError(t, err)

	rec := call(t, server.Handler(), http.MethodPost, pairingsPath,
		`{"provider":"prusa-account","subject":"new-subject","principal":"user:grace"}`)
	require.Equal(t, http.StatusCreated, rec.Code, "body: %s", rec.Body.String())

	require.Len(t, pairings.paired, 1)
	require.NotNil(t, pairings.paired[0].Generation, "the pairing saved no generation")
	require.Equal(t, int64(5), *pairings.paired[0].Generation)
	require.Equal(t, "user:operator", pairings.paired[0].CreatedBy, "the pairing does not name who made it")
	require.NotNil(t, pairings.paired[0].CreatorGeneration, "the pairing saved no creator generation")
	require.Equal(t, int64(8), *pairings.paired[0].CreatorGeneration)
	require.Contains(t, credentials.readBeforeLastCheck, "user:grace",
		"the principal's generation was read after the last session check")
	require.Contains(t, credentials.readBeforeLastCheck, "user:operator",
		"the creator's generation was read after the last session check")
	require.Contains(t, credentials.readBeforeLastCheck, "user:ada",
		"the root's generation was read after the last session check")
	require.Equal(t, "user:ada", pairings.paired[0].RootCreator, "the pairing did not record the caller's root")
	require.NotNil(t, pairings.paired[0].RootGeneration)
	require.Equal(t, int64(13), *pairings.paired[0].RootGeneration)
}

// orderedSession is a signed-in caller that records which subjects' fences
// had been read when the session was last checked.
type orderedSession struct {
	noCredentials

	pairings            *fakePairings
	readBeforeLastCheck []string
	root                string
}

func (o *orderedSession) Root(*http.Request) string { return o.root }

func (o *orderedSession) SignedIn(*http.Request) bool {
	o.pairings.mu.Lock()
	o.readBeforeLastCheck = append([]string(nil), o.pairings.fenceReads...)
	o.pairings.mu.Unlock()

	return true
}
