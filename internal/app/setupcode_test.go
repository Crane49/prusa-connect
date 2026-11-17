package app

import (
	"bytes"
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/app/resource"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// testSetupCode is the code the first-run tests configure, the way an
// automated install writes one into core.auth.setup_code.
const testSetupCode = "TEST-SETUP-CODE-0042"

// madeCode is the shape of a code the server makes: three groups of four
// symbols of Crockford's alphabet.
var madeCode = regexp.MustCompile(`^[0-9A-HJKMNP-TV-Z]{4}-[0-9A-HJKMNP-TV-Z]{4}-[0-9A-HJKMNP-TV-Z]{4}$`)

// clientA and clientB are two addresses on one home network.
const (
	clientA = "192.168.1.20"
	clientB = "192.168.1.21"
)

// at is a client at one address that sent no forwarding header.
func at(address string) httpapi.SetupClient { return httpapi.SetupClient{Address: address} }

// gateAt is a gate over a code the test chose, with a clock the test moves.
func gateAt(t *testing.T, code string, clock *time.Time) (*setupGate, *bytes.Buffer) {
	t.Helper()

	logged := bytes.NewBuffer(nil)
	logger := slog.New(slog.NewTextHandler(logged, &slog.HandlerOptions{Level: slog.LevelInfo}))

	gate, shown, err := newSetupGate(code, logger, func() time.Time { return *clock })
	require.NoError(t, err)
	require.Empty(t, shown, "a configured code was handed back to be printed")

	return gate, logged
}

// TestTheServerMakesALongRandomCode is ADR-20260929-a4j rule 1. The code has 60 random
// bits, so two draws are different, and it uses no symbol a person misreads.
func TestTheServerMakesALongRandomCode(t *testing.T) {
	t.Parallel()

	seen := make(map[string]bool)

	for range 200 {
		gate, shown, err := newSetupGate("", slog.New(slog.DiscardHandler), time.Now)
		require.NoError(t, err)
		require.Regexp(t, madeCode, shown)
		require.False(t, seen[shown], "two draws made the same code %s", shown)

		seen[shown] = true

		require.NoError(t, gate.admit(t.Context(), at(clientA), shown), "the code it printed was refused")
	}
}

// TestTheRightCodeIsAdmittedAndAnyOtherIsRefused is rules 5 and 6. The
// comparison forgives what a person types differently: case, hyphens, spaces,
// and O, I or L for 0 and 1. It forgives nothing else.
func TestTheRightCodeIsAdmittedAndAnyOtherIsRefused(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		sent  string
		admit bool
	}{
		{name: "as printed", sent: "K7QD-3M9X-TP20", admit: true},
		{name: "lower case without hyphens", sent: "k7qd3m9xtp20", admit: true},
		{name: "spaces between groups", sent: "K7QD 3M9X TP20", admit: true},
		{name: "the letter O for zero", sent: "K7QD-3M9X-TP2O", admit: true},
		{name: "one symbol wrong", sent: "K7QD-3M9X-TP21"},
		{name: "one symbol short", sent: "K7QD-3M9X-TP2"},
		{name: "one symbol more", sent: "K7QD-3M9X-TP200"},
		{name: "nothing", sent: ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			clock := time.Date(2026, time.September, 29, 9, 0, 0, 0, time.UTC)
			gate, _ := gateAt(t, "K7QD-3M9X-TP20", &clock)

			err := gate.admit(t.Context(), at(clientA), tc.sent)
			if tc.admit {
				require.NoError(t, err)

				return
			}

			requireFaultCode(t, err, fault.AuthSetupCodeRefused.Code())
		})
	}
}

// TestADecisionWithoutTheCodeRecordsNothing is rule 5 through the first run: a
// refused code leaves the install pending, with no mode and no account.
func TestADecisionWithoutTheCodeRecordsNothing(t *testing.T) {
	t.Parallel()

	for _, code := range []string{"", "WRONG-WRONG-WRONG"} {
		install := freshInstall(t)

		_, err := install.first.Record(t.Context(), httpapi.SetupDecision{
			Code: code, Client: at(clientA), Mode: resource.SetupRequired,
			Account: &httpapi.NewAccount{Username: "owner@example.com", Password: "correct horse battery"},
		})
		requireFaultCode(t, err, fault.AuthSetupCodeRefused.Code())
		require.True(t, install.first.Pending(), "a decision with code %q was recorded", code)

		recorded, err := install.store.SetupMode()
		require.NoError(t, err)
		require.Empty(t, recorded)

		info, err := install.who.accounts.Info(t.Context())
		require.NoError(t, err)
		require.True(t, info.NeedsSetup, "an account was created without the setup code")
	}
}

// TestTheCodeWorksOnce is rule 7. Once a decision is recorded the code is gone:
// the same code answers SETUP_CLOSED, and the gate refuses it too.
func TestTheCodeWorksOnce(t *testing.T) {
	t.Parallel()

	install := freshInstall(t)

	_, err := install.first.Record(t.Context(), httpapi.SetupDecision{
		Code: testSetupCode, Client: at(clientA), Mode: resource.SetupOpen,
	})
	require.NoError(t, err)

	_, err = install.first.Record(t.Context(), httpapi.SetupDecision{
		Code: testSetupCode, Client: at(clientA), Mode: resource.SetupOpen,
	})
	requireFaultCode(t, err, fault.AuthSetupClosed.Code())

	requireFaultCode(t, install.first.code.admit(t.Context(), at(clientA), testSetupCode),
		fault.AuthSetupClosed.Code())
}

// TestASettledInstallTakesNoCode is rule 8. An install whose decision is
// already written never asks for a code, so the code a start made is ended.
func TestASettledInstallTakesNoCode(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	install := startInstall(t, dir, dir+"/host.db", func(core *config.Core) { core.Principal = "somebody-else" })
	require.False(t, install.first.Pending())

	_, err := install.first.Record(t.Context(), httpapi.SetupDecision{
		Code: testSetupCode, Client: at(clientA), Mode: resource.SetupOpen,
	})
	requireFaultCode(t, err, fault.AuthSetupClosed.Code())
}

// TestWrongCodesAreLimitedPerAddress is rule 9. Five wrong codes from one
// address refuse that address, even with the right code, until fifteen
// minutes after its first wrong code. Another address is not affected.
func TestWrongCodesAreLimitedPerAddress(t *testing.T) {
	t.Parallel()

	clock := time.Date(2026, time.September, 29, 9, 0, 0, 0, time.UTC)
	gate, _ := gateAt(t, testSetupCode, &clock)

	for i := range setupCodeFree {
		requireFaultCode(t, gate.admit(t.Context(), at(clientA), "WRONG-"+strconv.Itoa(i)),
			fault.AuthSetupCodeRefused.Code())
	}

	clock = clock.Add(time.Minute)

	err := gate.admit(t.Context(), at(clientA), testSetupCode)
	requireFaultCode(t, err, fault.AuthSetupCodeLimited.Code())

	limited, ok := errors.AsType[*fault.Fault](err)
	require.True(t, ok)

	wait, ok := limited.Params().Get(httpapi.RetryAfterParam)
	require.True(t, ok)
	require.Equal(t, strconv.Itoa(14*60), wait.String())
	require.Contains(t, limited.Error(), "Try again in 14 minutes",
		"the refusal does not name the wait in words a page can show")

	require.NoError(t, gate.admit(t.Context(), at(clientB), testSetupCode),
		"one address's wrong codes refused another address")

	clock = clock.Add(setupCodeWindow)

	require.NoError(t, gate.admit(t.Context(), at(clientA), testSetupCode),
		"the address was still refused after its window")
}

// TestFourWrongCodesDoNotRefuseTheRightOne is the permit half of rule 9: a
// person who mistypes a few times still gets in.
func TestFourWrongCodesDoNotRefuseTheRightOne(t *testing.T) {
	t.Parallel()

	clock := time.Date(2026, time.September, 29, 9, 0, 0, 0, time.UTC)
	gate, _ := gateAt(t, testSetupCode, &clock)

	for range setupCodeFree - 1 {
		requireFaultCode(t, gate.admit(t.Context(), at(clientA), "WRONG"), fault.AuthSetupCodeRefused.Code())
	}

	require.NoError(t, gate.admit(t.Context(), at(clientA), testSetupCode))
}

// TestEachIPv6AddressIsCountedAlone is rule 10. Every host on a home network
// shares one link-local /64, so counting by the /64 would let one host use up
// the attempts of all the others. The whole address is the key.
func TestEachIPv6AddressIsCountedAlone(t *testing.T) {
	t.Parallel()

	clock := time.Date(2026, time.September, 29, 9, 0, 0, 0, time.UTC)
	gate, _ := gateAt(t, testSetupCode, &clock)

	for range setupCodeFree {
		requireFaultCode(t, gate.admit(t.Context(), at("fe80::1"), "WRONG"), fault.AuthSetupCodeRefused.Code())
	}

	requireFaultCode(t, gate.admit(t.Context(), at("fe80::1"), testSetupCode), fault.AuthSetupCodeLimited.Code())
	require.NoError(t, gate.admit(t.Context(), at("fe80::2"), testSetupCode),
		"another host on the same /64 was refused for the first host's wrong codes")
}

// TestLoopbackIsNeverRefusedForWrongCodes is rule 10's other half. The owner
// at the machine itself is not counted, so nobody on the network can lock
// them out, and a full table does not refuse them either.
func TestLoopbackIsNeverRefusedForWrongCodes(t *testing.T) {
	t.Parallel()

	for _, loopback := range []string{"127.0.0.1", "::1"} {
		clock := time.Date(2026, time.September, 29, 9, 0, 0, 0, time.UTC)
		gate, _ := gateAt(t, testSetupCode, &clock)

		for range setupCodeFree * 2 {
			requireFaultCode(t, gate.admit(t.Context(), at(loopback), "WRONG"), fault.AuthSetupCodeRefused.Code())
		}

		for i := range setupCodeClients {
			_ = gate.admit(t.Context(), at("10.0."+strconv.Itoa(i/256)+"."+strconv.Itoa(i%256)), "WRONG")
		}

		require.NoError(t, gate.admit(t.Context(), at(loopback), testSetupCode),
			"%s was refused after wrong codes or a full table", loopback)
	}
}

// TestAFullTableRefusesANewAddress is rule 11. The table of addresses has a
// bound, and a new address past it is refused rather than an old one
// forgotten, because forgetting would hand that old address new attempts. The
// refusal names the wait until the oldest address leaves, and the log says so
// once a minute rather than once per refusal.
func TestAFullTableRefusesANewAddress(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, time.September, 29, 9, 0, 0, 0, time.UTC)
	clock := start
	gate, logged := gateAt(t, testSetupCode, &clock)

	for i := range setupCodeClients {
		if i == 1 {
			clock = start.Add(10 * time.Minute)
		}

		address := "10.0." + strconv.Itoa(i/256) + "." + strconv.Itoa(i%256)
		requireFaultCode(t, gate.admit(t.Context(), at(address), "WRONG"), fault.AuthSetupCodeRefused.Code())
	}

	err := gate.admit(t.Context(), at(clientA), testSetupCode)
	requireFaultCode(t, err, fault.AuthSetupCodeLimited.Code())

	limited, ok := errors.AsType[*fault.Fault](err)
	require.True(t, ok)

	wait, ok := limited.Params().Get(httpapi.RetryAfterParam)
	require.True(t, ok)
	require.Equal(t, strconv.Itoa(5*60), wait.String(), "the wait is not the time until the oldest address leaves")

	requireFaultCode(t, gate.admit(t.Context(), at(clientB), testSetupCode), fault.AuthSetupCodeLimited.Code())
	require.Equal(t, 1, strings.Count(logged.String(), "too many addresses sent wrong codes"),
		"a full table was not logged, or was logged once per refusal")

	require.NoError(t, gate.admit(t.Context(), at("10.0.0.1"), testSetupCode),
		"an address already in the table lost its remaining attempts")

	clock = start.Add(setupCodeWindow)

	require.NoError(t, gate.admit(t.Context(), at(clientA), testSetupCode),
		"the oldest address was not swept once its window was over")
}

// TestAWrongCodeIsLoggedWithoutTheCode is rule 12. The log names the address
// and the count, and never what was sent or what the code is.
func TestAWrongCodeIsLoggedWithoutTheCode(t *testing.T) {
	t.Parallel()

	clock := time.Date(2026, time.September, 29, 9, 0, 0, 0, time.UTC)
	gate, logged := gateAt(t, testSetupCode, &clock)

	require.NoError(t, gate.admit(t.Context(), at(clientA), testSetupCode))
	require.Empty(t, logged.String(), "a right code was logged as a failure")

	for range setupCodeFree {
		_ = gate.admit(t.Context(), at(clientA), "GUESS-GUESS-GUESS")
	}

	lines := logged.String()
	require.Contains(t, lines, "a first run sent a wrong setup code")
	require.Contains(t, lines, "client="+clientA)
	require.Contains(t, lines, "refused for a while")
	require.NotContains(t, lines, "GUESS")
	require.NotContains(t, lines, "TEST-SETUP")
}

// TestAMadeCodeIsPrintedAndAConfiguredOneIsNot is rules 2 and 4. A code the
// server made goes to standard error and to the log; a code from the file is
// only named, because the log is read by more people than the file is.
func TestAMadeCodeIsPrintedAndAConfiguredOneIsNot(t *testing.T) {
	t.Parallel()

	logged := bytes.NewBuffer(nil)
	logger := slog.New(slog.NewTextHandler(logged, &slog.HandlerOptions{Level: slog.LevelInfo}))
	terminal := bytes.NewBuffer(nil)

	announcePending(t.Context(), logger, terminal, "K7QD-3M9X-TP20", true, nil, true)
	require.Contains(t, terminal.String(), "      K7QD-3M9X-TP20\n")
	require.Contains(t, logged.String(), "setup_code=K7QD-3M9X-TP20")

	logged.Reset()
	terminal.Reset()

	announcePending(t.Context(), logger, terminal, "", true, nil, true)
	require.Empty(t, terminal.String())
	require.Contains(t, logged.String(), "core.auth.setup_code")
	require.NotContains(t, logged.String(), "setup_code=")

	// A decision with no account yet names the account path, which is the
	// door that takes the code in that state.
	logged.Reset()

	announcePending(t.Context(), logger, terminal, "K7QD-3M9X-TP20", false, nil, true)
	require.Contains(t, logged.String(), `setup="POST /api/v1/auth/setup"`)
	require.Contains(t, logged.String(), "no administrator yet")
}

// TestAServerWithNoWebAppSendsNobodyToABrowser holds the banner to what the
// server serves: without the web app, the address answers 404, so the block
// names the API door rather than a page to open.
func TestAServerWithNoWebAppSendsNobodyToABrowser(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.DiscardHandler)
	terminal := bytes.NewBuffer(nil)
	where := []string{"http://localhost:8080"}

	announcePending(t.Context(), logger, terminal, "K7QD-3M9X-TP20", true, where, false)
	require.Contains(t, terminal.String(), "serves no web app")
	require.Contains(t, terminal.String(), "POST /api/v1/setup")
	require.NotContains(t, terminal.String(), "browser")
	require.NotContains(t, terminal.String(), "http://localhost:8080")
	require.Contains(t, terminal.String(), "      K7QD-3M9X-TP20\n")

	terminal.Reset()

	announcePending(t.Context(), logger, terminal, "K7QD-3M9X-TP20", true, where, true)
	require.Contains(t, terminal.String(), "http://localhost:8080")
}

// TestARestartBeforeSetupPrintsANewCode is rule 3 over a real socket: a core
// started twice over one directory prints two codes, and the first one no
// longer opens the first run.
func TestARestartBeforeSetupPrintsANewCode(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	write(t, dir+"/config.yaml", lines("core:", "  log:", "    level: error"))

	first, firstCode := launchPending(t, dir)
	first.stopAndWait(t)

	second, secondCode := launchPending(t, dir)
	require.NotEqual(t, firstCode, secondCode)

	stale := second.call(t, http.MethodPost, "/api/v1/setup", `{"mode":"open","code":"`+firstCode+`"}`)
	require.Equal(t, http.StatusForbidden, stale.status, stale.body)
	require.Contains(t, stale.body, "core.auth/SETUP_CODE_REFUSED")

	state := second.get(t, "/api/v1/setup")
	require.NotContains(t, state.body, secondCode, "the first-run state returned the code")

	recorded := second.call(t, http.MethodPost, "/api/v1/setup", `{"mode":"open","code":"`+secondCode+`"}`)
	require.Equal(t, http.StatusOK, recorded.status, recorded.body)
	require.NotContains(t, recorded.body, secondCode, "the answer returned the code")

	second.stopAndWait(t)
}

// TestACoreThatCannotServePrintsNoSetupCode is a second copy started on a port
// a running copy already holds. The browser reaches the running copy, which
// refuses any code but its own, so the copy that cannot serve prints none.
func TestACoreThatCannotServePrintsNoSetupCode(t *testing.T) {
	t.Parallel()

	held := t.TempDir()
	write(t, held+"/config.yaml", lines("core:", "  log:", "    level: error"))

	running, _ := launchPending(t, held)

	late := t.TempDir()
	write(t, late+"/config.yaml", lines("core:", "  log:", "    level: error"))

	terminal := &lockedBuffer{}
	refused := launchOptions(t, Options{
		ConfigPath:   late + "/config.yaml",
		APIAddr:      running.addr,
		Version:      "test",
		Stderr:       terminal,
		LoopbackOnly: true,
	})

	var err error

	require.Eventually(t, func() bool {
		var over bool

		over, err = refused.exited()

		return over
	}, settle, 10*time.Millisecond, "a core on a held port kept running")
	require.ErrorContains(t, err, "bind the api listener")

	for line := range strings.SplitSeq(terminal.String(), "\n") {
		require.NotRegexp(t, madeCode, strings.TrimSpace(line),
			"a core that never served printed a setup code:\n%s", terminal.String())
	}

	running.stopAndWait(t)
}

// launchPending starts a core over dir and reads the setup code it printed on
// standard error, the way an operator reads it in `docker logs`.
func launchPending(t *testing.T, dir string) (*process, string) {
	t.Helper()

	terminal := &lockedBuffer{}
	running := launchWith(t, dir, terminal)
	waitUntilReady(t, running)

	for line := range strings.SplitSeq(terminal.String(), "\n") {
		if code := strings.TrimSpace(line); madeCode.MatchString(code) {
			return running, code
		}
	}

	t.Fatalf("the core printed no setup code:\n%s", terminal.String())

	return nil, ""
}

// lockedBuffer is standard error as a test reads it while the core writes it.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.String()
}

// TestTheAccountPathTakesTheSetupCode is rule 17 over a real socket. An
// install that recorded required before it had an account still answers POST
// /api/v1/auth/setup. That path asks for the code the start printed, so
// nobody else on the network can make themselves the administrator with a
// plain request, and a restart once the account exists prints no code.
func TestTheAccountPathTakesTheSetupCode(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	write(t, dir+"/config.yaml", lines("core:", "  log:", "    level: error",
		"  overlay: '"+slashed(dir+"/resources.json")+"'", "  auth:", "    local: true"))
	write(t, dir+"/resources.json", `{"version": 1, "instances": [], "auth": {"mode": "required"}}`+"\n")

	running, code := launchPending(t, dir)

	account := `"username":"owner@example.com","password":"correct horse battery"`

	for _, sent := range []string{`{` + account + `}`, `{` + account + `,"code":"AAAA-BBBB-CCCC"}`} {
		refused := running.call(t, http.MethodPost, "/api/v1/auth/setup", sent)
		require.Equal(t, http.StatusForbidden, refused.status, refused.body)
		require.Contains(t, refused.body, "core.auth/SETUP_CODE_REFUSED")
	}

	made := running.call(t, http.MethodPost, "/api/v1/auth/setup", `{`+account+`,"code":"`+code+`"}`)
	require.Equal(t, http.StatusCreated, made.status, made.body)
	require.NotContains(t, made.body, code, "the answer returned the code")

	again := running.call(t, http.MethodPost, "/api/v1/auth/setup", `{`+account+`,"code":"`+code+`"}`)
	require.Equal(t, http.StatusConflict, again.status, again.body)

	running.stopAndWait(t)

	terminal := &lockedBuffer{}
	restarted := launchWith(t, dir, terminal)
	waitUntilReady(t, restarted)
	require.NotContains(t, terminal.String(), "setup code", "a server with an administrator printed a code")
	restarted.stopAndWait(t)
}

// TestOneDecisionIsRecordedAtATime is rule 7's lock. A second caller with the
// right code waits while the first decision is written, then finds the first
// run closed. It never writes its own admission choice over the first one.
func TestOneDecisionIsRecordedAtATime(t *testing.T) {
	t.Parallel()

	install := freshInstall(t)
	first := install.first

	// The test is the first decision: it holds the lock and writes what a
	// decision writes, while the second caller is already on its way.
	first.recording.Lock()

	done := make(chan error, 1)

	go func() {
		_, err := first.Record(t.Context(), httpapi.SetupDecision{
			Code: testSetupCode, Client: at(clientB), Mode: resource.SetupOpen, Admission: resource.AdmissionOpen,
		})
		done <- err
	}()

	select {
	case err := <-done:
		first.recording.Unlock()
		t.Fatalf("a second decision finished while the first held the lock: %v", err)
	case <-time.After(200 * time.Millisecond):
	}

	require.NoError(t, install.store.RecordAdmission(resource.AdmissionAsk))
	require.NoError(t, install.store.RecordSetupMode(resource.SetupOpen))
	first.mode.set(resource.SetupOpen)
	first.recording.Unlock()

	requireFaultCode(t, <-done, fault.AuthSetupClosed.Code())

	admission, err := install.store.AdmissionMode()
	require.NoError(t, err)
	require.Equal(t, resource.AdmissionAsk, admission, "the second decision wrote its admission choice")
}

// TestWrongCodesFromLoopbackAreLoggedOnceAMinute is the log half of rule 10.
// Loopback is never limited, and behind a proxy on this machine that nobody
// listed, every client is loopback. One line per wrong code would then let a
// caller fill the log and push the code out of `docker logs`.
func TestWrongCodesFromLoopbackAreLoggedOnceAMinute(t *testing.T) {
	t.Parallel()

	clock := time.Date(2026, time.September, 29, 9, 0, 0, 0, time.UTC)
	gate, logged := gateAt(t, testSetupCode, &clock)

	for range 1000 {
		requireFaultCode(t, gate.admit(t.Context(), at("127.0.0.1"), "WRONG"), fault.AuthSetupCodeRefused.Code())
	}

	require.Equal(t, 1, strings.Count(logged.String(), "wrong setup codes came from this machine"),
		"a thousand wrong codes from loopback in one minute wrote more than one line")

	clock = clock.Add(setupCodeFullLogEvery)
	requireFaultCode(t, gate.admit(t.Context(), at("::1"), "WRONG"), fault.AuthSetupCodeRefused.Code())

	require.Equal(t, 2, strings.Count(logged.String(), "wrong setup codes came from this machine"))
	require.Contains(t, logged.String(), "wrong_codes=1000", "the second line does not count the codes it stood for")
}

// TestAnUnlistedProxyIsWarnedAboutOnce says what the log flood above means:
// a proxy on this machine forwards requests, and core.trusted_proxies does not
// list it, so nothing limits the clients behind it.
func TestAnUnlistedProxyIsWarnedAboutOnce(t *testing.T) {
	t.Parallel()

	clock := time.Date(2026, time.September, 29, 9, 0, 0, 0, time.UTC)
	gate, logged := gateAt(t, testSetupCode, &clock)

	_ = gate.admit(t.Context(), at("127.0.0.1"), "WRONG")
	require.NotContains(t, logged.String(), "core.trusted_proxies does not list it",
		"a request with no forwarding header was taken for a proxy")

	forwarded := httpapi.SetupClient{Address: "127.0.0.1", Forwarded: true}
	for range 3 {
		_ = gate.admit(t.Context(), forwarded, "WRONG")
	}

	require.Equal(t, 1, strings.Count(logged.String(), "core.trusted_proxies does not list it"))
}

// TestOneIPv6NetworkTakesAtMostSixteenSlots is rule 11's other bound. A host
// holds a whole /64, so without a cap it could fill every slot and lock out
// each new address, the owner's Docker bridge address among them.
func TestOneIPv6NetworkTakesAtMostSixteenSlots(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, time.September, 29, 9, 0, 0, 0, time.UTC)
	clock := start
	gate, logged := gateAt(t, testSetupCode, &clock)

	// An older address outside the network, so the wait until the network's
	// oldest address leaves differs from the wait for the whole table.
	requireFaultCode(t, gate.admit(t.Context(), at("10.0.0.9"), "WRONG"), fault.AuthSetupCodeRefused.Code())

	for i := range setupCodeNetworkSlots {
		switch i {
		case 0:
			clock = start.Add(3 * time.Minute)
		case 1:
			clock = start.Add(13 * time.Minute)
		}

		requireFaultCode(t, gate.admit(t.Context(), at("2001:db8:1:2::"+strconv.FormatInt(int64(i+1), 16)), "WRONG"),
			fault.AuthSetupCodeRefused.Code())
	}

	err := gate.admit(t.Context(), at("2001:db8:1:2::ffff"), testSetupCode)
	requireFaultCode(t, err, fault.AuthSetupCodeLimited.Code())

	limited, ok := errors.AsType[*fault.Fault](err)
	require.True(t, ok)

	wait, ok := limited.Params().Get(httpapi.RetryAfterParam)
	require.True(t, ok)
	require.Equal(
		t,
		strconv.Itoa(5*60),
		wait.String(),
		"the wait is not the time until the network's oldest address leaves",
	)
	require.Contains(t, logged.String(), "its IPv6 network sent too many wrong codes")

	require.NoError(t, gate.admit(t.Context(), at("2001:db8:1:3::1"), testSetupCode),
		"another /64 was refused for this one's addresses")
	require.NoError(t, gate.admit(t.Context(), at("172.17.0.1"), testSetupCode),
		"the Docker bridge address was refused for one /64's addresses")
	require.NoError(t, gate.admit(t.Context(), at("2001:db8:1:2::1"), testSetupCode),
		"an address of the network that is already counted lost its remaining attempts")
}

// TestAStartThatAppliesTheWizardDoesNotWarnOfNoAdministrator holds the log of
// the start after a confirmed wizard: the administrator it names is created
// during that start, so no line may say the server has none.
func TestAStartThatAppliesTheWizardDoesNotWarnOfNoAdministrator(t *testing.T) {
	// Not parallel: captureStderr swaps the process-wide os.Stderr.
	dir := t.TempDir()
	write(t, dir+"/config.yaml", lines("core:", "  log:", "    level: info"))

	install, _ := wizardInstall(t, dir)
	_, err := install.first.Record(t.Context(), wizardDecision(dir))
	require.NoError(t, err)

	output := captureStderr(t)
	running := launchWith(t, dir, &lockedBuffer{})
	waitUntilReady(t, running)
	running.stopAndWait(t)

	logged := output()
	require.Contains(t, logged, "the first run the wizard confirmed is recorded")
	require.NotContains(t, logged, "no administrator yet")
}
