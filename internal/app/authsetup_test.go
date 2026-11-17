package app

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/app/resource"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/internal/platform/auth"
	"github.com/prusa3d/prusa-connect/internal/platform/auth/session"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpfault"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// firstRunSetup is one install as the first run sees it: the overlay it records
// the decision in, the identity layer the decision picked, and the log lines the
// start-up path wrote.
type firstRunSetup struct {
	first *firstRun
	who   identity
	store *resource.Store
	dir   string
	db    string
	log   *bytes.Buffer
}

// startInstall runs the start-up path over one directory and one database: read
// the recorded decision, wire the identity layer from it, then record the
// decision an install that predates the key already took.
//
// It is the sequence serveOver runs, so a test that starts the same install
// twice is testing what a restart does.
func startInstall(t *testing.T, dir, db string, shape func(*config.Core)) firstRunSetup {
	t.Helper()

	core := config.Default()
	core.Principal = "operator"
	core.Overlay = filepath.Join(dir, "config.d", "resources.json")
	core.Authz.Database = db

	if shape != nil {
		shape(&core)
	}

	recorded := bytes.NewBuffer(nil)
	logger := slog.New(slog.NewTextHandler(recorded, &slog.HandlerOptions{Level: slog.LevelInfo}))

	host := testHostAt(t, db)

	store, err := resource.New(resource.Config{
		Paths: resource.Paths{File: filepath.Join(dir, "config.yaml"), Overlay: core.Overlay},
		Graph: host.graph,

		ModulesOff: !core.ModulesOn(),
	})
	require.NoError(t, err)
	require.NoError(t, store.Reload(t.Context()))

	recordedMode, err := store.SetupMode()
	require.NoError(t, err)

	// One cell, the way serveOver holds it: the resolver and the first run read
	// the same decision, so recording one moves both.
	mode := newSetupMode(recordedMode)

	who, err := buildIdentity(t.Context(), core, host, identityPlane{}, mode, logger)
	require.NoError(t, err)

	code, _, err := newSetupGate(testSetupCode, logger, time.Now)
	require.NoError(t, err)

	first := newFirstRun(core, store, mode, code, who.accounts, "0.1.0", logger)
	require.NoError(t, first.settle(t.Context()))

	return firstRunSetup{first: first, who: who, store: store, dir: dir, db: db, log: recorded}
}

// freshInstall is an install nobody has started before.
func freshInstall(t *testing.T) firstRunSetup {
	t.Helper()

	dir := t.TempDir()

	return startInstall(t, dir, filepath.Join(dir, "host.db"), nil)
}

// TestAFreshInstallHasDecidedNothing is the old backlog's first part. A server
// nobody has set up cannot say whether a caller needs a credential, so it says
// so rather than picking one.
func TestAFreshInstallHasDecidedNothing(t *testing.T) {
	t.Parallel()

	install := freshInstall(t)
	require.True(t, install.first.Pending())

	state, err := install.first.State(t.Context())
	require.NoError(t, err)
	require.Equal(t, httpapi.SetupPending, state.State)
	require.Empty(t, state.Mode)
	require.Equal(t, "0.1.0", state.Version)
	require.False(t, state.Issuer)
	require.Empty(t, state.Modules)
}

// TestRecordingRequiredCreatesTheAdministratorAndSignsThemIn is the whole of
// what a person does on the first run: they choose to need a login, they get an
// account, and the page they are on continues as them.
func TestRecordingRequiredCreatesTheAdministratorAndSignsThemIn(t *testing.T) {
	t.Parallel()

	install := freshInstall(t)

	result, err := install.first.Record(t.Context(), httpapi.SetupDecision{
		Code: testSetupCode,
		Mode: resource.SetupRequired,
		Account: &httpapi.NewAccount{
			Username: "owner@example.com",
			Password: "correct horse battery",
		},
	})
	require.NoError(t, err)
	require.Equal(t, resource.SetupRequired, result.Mode)
	require.True(t, result.SignedIn)
	require.NotEmpty(t, result.Session.Token)

	require.False(t, install.first.Pending())

	recorded, err := install.store.SetupMode()
	require.NoError(t, err)
	require.Equal(t, resource.SetupRequired, recorded)

	state, err := install.first.State(t.Context())
	require.NoError(t, err)
	require.Equal(t, httpapi.SetupDone, state.State)
	require.Equal(t, resource.SetupRequired, state.Mode)

	// And the resolver follows the decision without a restart: a request with
	// no credential is refused, and the session this call minted is admitted.
	_, err = install.who.principals.Principal(httptest.NewRequest(http.MethodGet, "/api/v1/devices", nil))
	require.True(t, auth.Refused(err), "a request with no credential was served anyway: %v", err)

	signed := httptest.NewRequest(http.MethodGet, "/api/v1/devices", nil)
	signed.AddCookie(&http.Cookie{Name: session.CookieName, Value: result.Session.Token})

	who, err := install.who.principals.Principal(signed)
	require.NoError(t, err)
	require.Equal(t, "user:operator", who)
}

// TestASecondDecisionIsRefused keeps the first run closed. Reopening it over
// the API would let whoever reaches the port choose again.
func TestASecondDecisionIsRefused(t *testing.T) {
	t.Parallel()

	install := freshInstall(t)

	_, err := install.first.Record(t.Context(), httpapi.SetupDecision{Code: testSetupCode, Mode: resource.SetupOpen})
	require.NoError(t, err)

	_, err = install.first.Record(t.Context(), httpapi.SetupDecision{Code: testSetupCode, Mode: resource.SetupOpen})
	requireFaultCode(t, err, fault.AuthSetupClosed.Code())
}

// TestRecordingOpenServesEveryRequestAsThePrincipal is the other decision. An
// open deployment asks for no credential, and every request is core.principal
// because somebody chose that and it is written down.
func TestRecordingOpenServesEveryRequestAsThePrincipal(t *testing.T) {
	t.Parallel()

	install := freshInstall(t)

	result, err := install.first.Record(
		t.Context(),
		httpapi.SetupDecision{Code: testSetupCode, Mode: resource.SetupOpen},
	)
	require.NoError(t, err)
	require.Equal(t, resource.SetupOpen, result.Mode)
	require.False(t, result.SignedIn)
	require.Empty(t, result.Session.Token)

	recorded, err := install.store.SetupMode()
	require.NoError(t, err)
	require.Equal(t, resource.SetupOpen, recorded)

	who, err := install.who.principals.Principal(
		httptest.NewRequest(http.MethodGet, "/api/v1/devices", nil))
	require.NoError(t, err)
	require.Equal(t, "user:operator", who)
}

// TestRecordingOpenLeavesNoAdministratorToCreate holds the start that applies
// an open wizard: it builds the account store before it records the decision,
// so the store must stop saying an administrator is missing once it is.
func TestRecordingOpenLeavesNoAdministratorToCreate(t *testing.T) {
	t.Parallel()

	install := freshInstall(t)

	_, err := install.first.Record(t.Context(), httpapi.SetupDecision{Code: testSetupCode, Mode: resource.SetupOpen})
	require.NoError(t, err)

	info, err := install.who.accounts.Info(t.Context())
	require.NoError(t, err)
	require.Equal(t, httpapi.AuthInfo{}, info, "an open server still offers a password login")

	needed, err := install.first.needsAccount(t.Context())
	require.NoError(t, err)
	require.False(t, needed, "an open server still asks for its first administrator")
}

// TestAnInstallWithAccountsIsTreatedAsRequired is the old backlog's third part.
// An install that upgrades keeps working: the decision it already took is
// written on the first start that looks for it, and said out loud.
func TestAnInstallWithAccountsIsTreatedAsRequired(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	db := filepath.Join(dir, "host.db")

	install := startInstall(t, dir, db, nil)

	_, err := install.first.Record(t.Context(), httpapi.SetupDecision{
		Code: testSetupCode,
		Mode: resource.SetupRequired,
		Account: &httpapi.NewAccount{
			Username: "owner@example.com",
			Password: "correct horse battery",
		},
	})
	require.NoError(t, err)

	// The install this server upgrades from: accounts in the database and no
	// decision in the overlay, because the key did not exist when it was set up.
	forgetTheDecision(t, install.dir)

	restarted := startInstall(t, dir, db, nil)
	require.False(t, restarted.first.Pending(), "an install with accounts was told to set itself up again")

	recorded, err := restarted.store.SetupMode()
	require.NoError(t, err)
	require.Equal(t, resource.SetupRequired, recorded)

	require.Contains(t, restarted.log.String(), "predates the first-run decision")
	require.Contains(t, restarted.log.String(), "this server already holds accounts")
}

// TestAnInstallWithAPrincipalOfItsOwnIsTreatedAsOpen is the same rule for the
// deployment that never had a login. An operator who named the subject every
// request is served as chose to run it open, and that choice is now a key.
func TestAnInstallWithAPrincipalOfItsOwnIsTreatedAsOpen(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	install := startInstall(t, dir, filepath.Join(dir, "host.db"), func(core *config.Core) {
		core.Principal = "workshop"
	})

	require.False(t, install.first.Pending())

	recorded, err := install.store.SetupMode()
	require.NoError(t, err)
	require.Equal(t, resource.SetupOpen, recorded)
	require.Contains(t, install.log.String(), "core.principal names a subject the operator chose")
}

// TestAnInstallWithAPasswordLoginIsTreatedAsRequired covers the operator who
// set core.auth.local and has not created their administrator yet. That
// deployment refuses every request carrying no credential, so it has decided,
// and a first run holding it shut would take a working server out of service.
func TestAPasswordLoginWithNoAccountStaysPending(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	install := startInstall(t, dir, filepath.Join(dir, "host.db"), func(core *config.Core) {
		core.Auth = config.Auth{Local: true}
	})

	require.True(t, install.first.Pending(), "a password login with no account was settled, "+
		"which leaves POST /api/v1/auth/setup as the only way in")

	recorded, err := install.store.SetupMode()
	require.NoError(t, err)
	require.Empty(t, recorded)

	// Open is refused, because the operator's file asks every caller for a
	// credential.
	_, err = install.first.Record(t.Context(), httpapi.SetupDecision{Code: testSetupCode, Mode: resource.SetupOpen})
	requireFaultCode(t, err, httpfault.CodeRequestInvalid())
	require.True(t, install.first.Pending())

	result, err := install.first.Record(t.Context(), httpapi.SetupDecision{
		Code: testSetupCode, Mode: resource.SetupRequired,
		Account: &httpapi.NewAccount{Username: "owner@example.com", Password: "correct horse battery"},
	})
	require.NoError(t, err)
	require.True(t, result.SignedIn)
}

// TestADecisionThatCannotBeCarriedOutRecordsNoMode is what atomic means here.
// The install stays pending, so the page asks again rather than leaving a
// deployment nobody decided the identity of.
func TestADecisionThatCannotBeCarriedOutRecordsNoMode(t *testing.T) {
	t.Parallel()

	install := freshInstall(t)

	_, err := install.first.Record(t.Context(), httpapi.SetupDecision{
		Code:    testSetupCode,
		Mode:    resource.SetupOpen,
		Modules: []httpapi.SetupModuleChoice{{Instance: "sink:nothing-declared", Enabled: true}},
	})
	require.Error(t, err)

	require.True(t, install.first.Pending(), "a decision that failed was recorded anyway")

	recorded, err := install.store.SetupMode()
	require.NoError(t, err)
	require.Empty(t, recorded)
}

// TestTheDecisionSwitchesADeclaredInstance holds the third thing the first run
// does. It goes through the resource store as the principal the graph's first
// grant names, so switching something on here obeys the rules an administrator's
// own call obeys.
func TestTheDecisionSwitchesADeclaredInstance(t *testing.T) {
	t.Parallel()

	install := freshInstall(t)

	instance, err := ref.New(ref.KindStorage, "local")
	require.NoError(t, err)

	_, err = install.store.Apply(t.Context(), "user:operator", instance, httpapi.ResourceSpec{
		Type:    "filesystem",
		Enabled: true,
		Config:  httpapi.ResourceConfig(`{"root":"` + strings.ReplaceAll(install.dir, `\`, `\\`) + `"}`),
	}, httpapi.Precondition{MustNotExist: true})
	require.NoError(t, err)

	_, err = install.first.Record(t.Context(), httpapi.SetupDecision{
		Code:    testSetupCode,
		Mode:    resource.SetupOpen,
		Modules: []httpapi.SetupModuleChoice{{Instance: instance.String(), Enabled: false}},
	})
	require.NoError(t, err)

	after, err := install.store.Get(t.Context(), "user:operator", instance)
	require.NoError(t, err)
	require.False(t, after.Enabled, "the decision did not switch the instance off")

	// And it is not on the first-run listing, because it is not a module: the
	// page offers what an operator installed, not what the binary compiles in.
	state, err := install.first.State(t.Context())
	require.NoError(t, err)
	require.Empty(t, state.Modules)
}

// forgetTheDecision rewrites the overlay without the first-run key, which is
// what an install set up before the key existed holds.
func forgetTheDecision(t *testing.T, dir string) {
	t.Helper()

	path := filepath.Join(dir, "config.d", "resources.json")

	content, err := os.ReadFile(path)
	require.NoError(t, err)

	var document map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(content, &document))

	delete(document, "auth")

	rewritten, err := json.MarshalIndent(document, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, rewritten, 0o600))
}

// TestACoreThatHasDecidedNothingServesOnlyTheFirstRun drives the whole thing
// over a socket: a real core, a real overlay and the real router. It is the
// answer a self-hoster gets the first time they start this server.
func TestACoreThatHasDecidedNothingServesOnlyTheFirstRun(t *testing.T) {
	t.Parallel()

	running := startPending(t, t.TempDir(), lines("core:", "  log:", "    level: error",
		"  auth:", "    setup_code: "+testSetupCode))

	require.Equal(t, http.StatusOK, running.get(t, "/api/v1/health").status)
	require.Equal(t, http.StatusOK, running.get(t, "/api/v1/readiness").status)

	refused := running.get(t, "/api/v1/devices")
	require.Equal(t, http.StatusServiceUnavailable, refused.status, refused.body)
	require.Contains(t, refused.body, "core.auth/SETUP_PENDING")

	pending := running.get(t, "/api/v1/setup")
	require.Equal(t, http.StatusOK, pending.status, pending.body)
	require.Contains(t, pending.body, `"state":"pending"`)

	recorded := running.call(t, http.MethodPost, "/api/v1/setup", `{"mode":"open","code":"`+testSetupCode+`"}`)
	require.Equal(t, http.StatusOK, recorded.status, recorded.body)

	// And the surface answers, without a restart.
	require.Equal(t, http.StatusOK, running.get(t, "/api/v1/devices").status)

	again := running.call(t, http.MethodPost, "/api/v1/setup", `{"mode":"open","code":"`+testSetupCode+`"}`)
	require.Equal(t, http.StatusConflict, again.status, again.body)

	running.stopAndWait(t)
}

// TestTheAdmissionChoiceSurvivesARestart is the old backlog's admission row. The page
// draws "ask me first" and "let every printer in", and the answer has to land
// somewhere a restart reads back: the machine-owned overlay, beside the mode.
func TestTheAdmissionChoiceSurvivesARestart(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	db := filepath.Join(dir, "host.db")

	install := startInstall(t, dir, db, nil)

	recorded, err := install.store.AdmissionMode()
	require.NoError(t, err)
	require.Empty(t, recorded, "a fresh install has already decided how a printer joins")

	result, err := install.first.Record(t.Context(), httpapi.SetupDecision{
		Code: testSetupCode,
		Mode: resource.SetupOpen, Admission: resource.AdmissionOpen,
	})
	require.NoError(t, err)
	require.Equal(t, resource.AdmissionOpen, result.Admission)

	recorded, err = install.store.AdmissionMode()
	require.NoError(t, err)
	require.Equal(t, resource.AdmissionOpen, recorded)

	// The restart is the whole point: the connector reads this when it is
	// built, which is the next start and not this one.
	restarted := startInstall(t, dir, db, nil)

	recorded, err = restarted.store.AdmissionMode()
	require.NoError(t, err)
	require.Equal(t, resource.AdmissionOpen, recorded)

	state, err := restarted.first.State(t.Context())
	require.NoError(t, err)
	require.Equal(t, resource.AdmissionOpen, state.Admission)
}

// TestADecisionWithNoAdmissionAsksFirst is the careful default. A body that
// says nothing must not be the one that lets any machine on the wire in.
func TestADecisionWithNoAdmissionAsksFirst(t *testing.T) {
	t.Parallel()

	install := freshInstall(t)

	result, err := install.first.Record(
		t.Context(),
		httpapi.SetupDecision{Code: testSetupCode, Mode: resource.SetupOpen},
	)
	require.NoError(t, err)
	require.Equal(t, resource.AdmissionAsk, result.Admission)

	recorded, err := install.store.AdmissionMode()
	require.NoError(t, err)
	require.Equal(t, resource.AdmissionAsk, recorded)
}

// TestAnAdmissionWordNobodyKnowsRecordsNothing keeps the set at two, and keeps
// the install pending so the page can ask again.
func TestAnAdmissionWordNobodyKnowsRecordsNothing(t *testing.T) {
	t.Parallel()

	install := freshInstall(t)

	_, err := install.first.Record(t.Context(), httpapi.SetupDecision{
		Code: testSetupCode,
		Mode: resource.SetupOpen, Admission: "sometimes",
	})
	require.Error(t, err)
	require.True(t, install.first.Pending())

	recorded, err := install.store.SetupMode()
	require.NoError(t, err)
	require.Empty(t, recorded)
}

// TestAFreshInstallReportsEveryStepAsUndone is the old backlog. A person opening
// the page reads one row per step and the sentence beside it, instead of
// working out what is left from three other fields.
func TestAFreshInstallReportsEveryStepAsUndone(t *testing.T) {
	t.Parallel()

	install := freshInstall(t)

	state, err := install.first.State(t.Context())
	require.NoError(t, err)

	require.Equal(t, []httpapi.SetupStep{
		{Step: httpapi.SetupStepMode, Done: false, Detail: httpapi.SetupModeDetail("")},
		{Step: httpapi.SetupStepAdministrator, Done: false, Detail: "No account has been created yet."},
		{Step: httpapi.SetupStepAdmission, Done: false, Detail: httpapi.SetupAdmissionDetail("")},
	}, state.Steps)
}

// TestASettledInstallReportsWhatItsFirstRunDid is the other half of row 2: the
// steps are read off the disk, so a server that was set up long ago and has
// been restarted since still answers what it decided.
func TestASettledInstallReportsWhatItsFirstRunDid(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	db := filepath.Join(dir, "host.db")

	install := startInstall(t, dir, db, nil)

	result, err := install.first.Record(t.Context(), httpapi.SetupDecision{
		Code:      testSetupCode,
		Mode:      resource.SetupRequired,
		Admission: resource.AdmissionAsk,
		Account: &httpapi.NewAccount{
			Username: "owner@example.com",
			Password: "correct horse battery",
		},
	})
	require.NoError(t, err)
	require.Equal(t, []httpapi.SetupStep{
		{Step: httpapi.SetupStepMode, Done: true, Detail: httpapi.SetupModeDetail(resource.SetupRequired)},
		{Step: httpapi.SetupStepAdministrator, Done: true,
			Detail: "This server holds an account to sign in with."},
		{Step: httpapi.SetupStepAdmission, Done: true,
			Detail: httpapi.SetupAdmissionDetail(resource.AdmissionAsk)},
	}, result.Steps)

	restarted := startInstall(t, dir, db, nil)

	state, err := restarted.first.State(t.Context())
	require.NoError(t, err)
	require.Equal(t, result.Steps, state.Steps,
		"a restart reports different steps from the call that recorded them")
}
