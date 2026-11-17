package e2e

import (
	"net/http"
	"strconv"
	"strings"
	"testing"
)

// TestTheFirstRunTakesTheCodeFromTheLog is ADR-20260929-a4j end to end, on the built
// binary. A fresh install prints a code, refuses a decision without it or with
// a wrong one, takes it as a person types it, and then never takes it again.
func TestTheFirstRunTakesTheCodeFromTheLog(t *testing.T) {
	t.Parallel()

	srv := launchServerIn(t, t.TempDir(), "", "")
	code := srv.setupCode(t)

	block := func(log string) bool { return strings.Contains(log, "      "+code+"\n") }
	if log, ok := srv.awaitLog(block); !ok {
		t.Fatalf("the code is in the log line but not in the block on standard error:\n%s", log)
	}

	pending := srv.get(t, "/api/v1/devices")
	requireAnswer(t, pending, http.StatusServiceUnavailable, "core.auth/SETUP_PENDING")

	for _, body := range []string{
		`{"mode":"open"}`,
		`{"mode":"open","code":"AAAA-BBBB-CCCC"}`,
	} {
		refused := srv.do(t, http.MethodPost, "/api/v1/setup", body, nil)
		requireAnswer(t, refused, http.StatusForbidden, "core.auth/SETUP_CODE_REFUSED")
	}

	state := srv.get(t, "/api/v1/setup")
	requireAnswer(t, state, http.StatusOK, `"state":"pending"`)
	requireNoCode(t, state, code)

	typed := strings.ToLower(strings.ReplaceAll(code, "-", " "))
	recorded := srv.do(t, http.MethodPost, "/api/v1/setup", `{"mode":"open","code":"`+typed+`"}`, nil)
	requireAnswer(t, recorded, http.StatusOK, `"state":"done"`)
	requireNoCode(t, recorded, code)

	again := srv.do(t, http.MethodPost, "/api/v1/setup", `{"mode":"open","code":"`+code+`"}`, nil)
	requireAnswer(t, again, http.StatusConflict, "core.auth/SETUP_CLOSED")

	requireAnswer(t, srv.get(t, "/api/v1/devices"), http.StatusOK, "")

	// The test calls from loopback, which is never limited, so its two wrong
	// codes make one line a minute rather than a line each (ADR-20260929-a4j rule 10).
	if got := strings.Count(srv.stderr.String(), "wrong setup codes came from this machine"); got != 1 {
		t.Fatalf("the log holds %d lines about wrong setup codes from loopback, want 1:\n%s", got, srv.stderr.String())
	}
}

// TestWrongSetupCodesAreLimited is rule 9 on the wire: past five wrong codes
// the right one is refused too, with a Retry-After a client can wait for.
//
// Loopback is never counted, and the server here listens on loopback only. So
// the requests come through a proxy the file declares, from an address the
// proxy names in X-Forwarded-For, which is how a LAN client reaches a server
// behind one.
func TestWrongSetupCodesAreLimited(t *testing.T) {
	t.Parallel()

	srv := launchServerIn(t, t.TempDir(), "  trusted_proxies: [\"127.0.0.1/32\"]", "")
	code := srv.setupCode(t)
	lan := map[string]string{"X-Forwarded-For": "192.168.1.20"}

	for i := range 5 {
		wrong := srv.do(t, http.MethodPost, "/api/v1/setup",
			`{"mode":"open","code":"WRONG-`+strconv.Itoa(i)+`"}`, lan)
		requireAnswer(t, wrong, http.StatusForbidden, "core.auth/SETUP_CODE_REFUSED")
	}

	limited := srv.do(t, http.MethodPost, "/api/v1/setup", `{"mode":"open","code":"`+code+`"}`, lan)
	requireAnswer(t, limited, http.StatusTooManyRequests, "core.auth/SETUP_CODE_LIMITED")

	wait, err := strconv.Atoi(limited.header.Get("Retry-After"))
	if err != nil || wait < 1 || wait > 15*60 {
		t.Fatalf("Retry-After is %q, want whole seconds within fifteen minutes", limited.header.Get("Retry-After"))
	}

	// The owner at the machine itself is not counted, so the code still works
	// from loopback.
	recorded := srv.do(t, http.MethodPost, "/api/v1/setup", `{"mode":"open","code":"`+code+`"}`, nil)
	requireAnswer(t, recorded, http.StatusOK, `"state":"done"`)
}

// TestAConfiguredSetupCodeIsAskedForAndNeverPrinted is rule 4: an automated
// install sets core.auth.setup_code, and the binary asks for that value and
// keeps it out of its log.
func TestAConfiguredSetupCodeIsAskedForAndNeverPrinted(t *testing.T) {
	t.Parallel()

	const configured = "E2E-CONFIGURED-SETUP-CODE"

	srv := launchServerIn(t, t.TempDir(), "  auth:\n    setup_code: "+configured, "")

	named := func(log string) bool { return strings.Contains(log, "core.auth.setup_code") }

	log, ok := srv.awaitLog(named)
	if !ok {
		t.Fatalf("the log does not say the setup code comes from core.auth.setup_code:\n%s", log)
	}

	if strings.Contains(log, configured) || setupCodeLine.MatchString(log) {
		t.Fatalf("the log printed a setup code the operator configured:\n%s", log)
	}

	recorded := srv.do(t, http.MethodPost, "/api/v1/setup", `{"mode":"open","code":"`+configured+`"}`, nil)
	requireAnswer(t, recorded, http.StatusOK, `"state":"done"`)
}

// TestARestartBeforeTheFirstRunPrintsANewCode is rule 3: the code lives in the
// process, so a restart prints a new one and the old one stops working.
func TestARestartBeforeTheFirstRunPrintsANewCode(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	before := launchServerIn(t, dir, "", "")
	old := before.setupCode(t)
	before.kill(t)

	after := launchServerIn(t, dir, "", "")
	fresh := after.setupCode(t)

	if fresh == old {
		t.Fatalf("the restart printed the same code %s", old)
	}

	stale := after.do(t, http.MethodPost, "/api/v1/setup", `{"mode":"open","code":"`+old+`"}`, nil)
	requireAnswer(t, stale, http.StatusForbidden, "core.auth/SETUP_CODE_REFUSED")

	recorded := after.do(t, http.MethodPost, "/api/v1/setup", `{"mode":"open","code":"`+fresh+`"}`, nil)
	requireAnswer(t, recorded, http.StatusOK, `"state":"done"`)
}

// requireAnswer holds one answer to its status and, when want is not empty, to
// a piece of its body.
func requireAnswer(t *testing.T, got response, status int, want string) {
	t.Helper()

	if got.status != status || !strings.Contains(string(got.body), want) {
		t.Fatalf("answered %d %s, want %d with %q", got.status, got.body, status, want)
	}
}

// requireNoCode refuses an answer that carries the setup code in any spelling
// a client could read it back in.
func requireNoCode(t *testing.T, got response, code string) {
	t.Helper()

	body := strings.ToUpper(string(got.body))
	if strings.Contains(body, code) || strings.Contains(body, strings.ReplaceAll(code, "-", "")) {
		t.Fatalf("an answer returned the setup code: %s", got.body)
	}
}

// TestAPasswordLoginInstallIsSetUpBehindTheCode is the case a review found
// open. With core.auth.local and no account, the install stays in its first
// run: the account path is closed until then, open is refused, and the
// administrator is created only with the code from the log.
func TestAPasswordLoginInstallIsSetUpBehindTheCode(t *testing.T) {
	t.Parallel()

	srv := launchServerIn(t, t.TempDir(), localAuth, "")
	code := srv.setupCode(t)

	account := `"username":"` + adminName + `","password":"` + password + `"`

	early := srv.do(t, http.MethodPost, "/api/v1/auth/setup", `{`+account+`}`, nil)
	requireAnswer(t, early, http.StatusServiceUnavailable, "core.auth/SETUP_PENDING")

	open := srv.do(t, http.MethodPost, "/api/v1/setup", `{"mode":"open","code":"`+code+`"}`, nil)
	requireAnswer(t, open, http.StatusUnprocessableEntity, "core.auth.local")

	refused := srv.do(t, http.MethodPost, "/api/v1/setup", `{"mode":"required","account":{`+account+`}}`, nil)
	requireAnswer(t, refused, http.StatusForbidden, "core.auth/SETUP_CODE_REFUSED")

	made := srv.do(t, http.MethodPost, "/api/v1/setup",
		`{"mode":"required","code":"`+code+`","account":{`+account+`}}`, nil)
	requireAnswer(t, made, http.StatusOK, `"signed_in":true`)

	closed := srv.do(t, http.MethodPost, "/api/v1/auth/setup", `{`+account+`,"code":"`+code+`"}`, nil)
	requireAnswer(t, closed, http.StatusConflict, "")
}
