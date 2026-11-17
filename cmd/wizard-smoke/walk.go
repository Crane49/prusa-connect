package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

const (
	adminName     = "admin"
	adminPassword = "smoke-test-pass-1"
	title         = "Wizard smoke test"
)

// setupCode is the code the server prints in its log while the first run is
// pending.
var setupCode = regexp.MustCompile(`setup_code=([A-Z0-9-]+)`)

// errStopped reports a walk that could not go on, after the failed check that
// says why.
var errStopped = errors.New("the walk stopped early")

// checks prints one line for each check and remembers which failed.
type checks struct {
	out   io.Writer
	names []string
}

func newChecks(out io.Writer) *checks { return &checks{out: out} }

func (c *checks) check(name string, ok bool, detail any) bool {
	if ok {
		fmt.Fprintln(c.out, "PASS "+name)

		return true
	}

	fmt.Fprintf(c.out, "FAIL %s  (%v)\n", name, detail)
	c.names = append(c.names, name)

	return false
}

func (c *checks) failed() bool { return len(c.names) > 0 }

func (c *checks) result(walkErr error) error {
	if walkErr != nil && !errors.Is(walkErr, errStopped) {
		fmt.Fprintln(c.out, "FAIL the walk: "+walkErr.Error())
		c.names = append(c.names, "the walk")
	}

	if len(c.names) == 0 {
		fmt.Fprintln(c.out, "\nRESULT OK")

		return nil
	}

	fmt.Fprintln(c.out, "\nRESULT FAILED: "+strings.Join(c.names, "; "))

	return errFailed
}

// walk is the whole run: the first start, the wizard, the restart it asks for,
// then a stop and a start that must keep the setup.
func walk(ctx context.Context, srv *server, report *checks, webApp bool) error {
	api, err := newClient()
	if err != nil {
		return err
	}

	if err = srv.start(ctx); err != nil {
		return err
	}

	if err = firstRun(ctx, srv, api, report, webApp); err != nil {
		return err
	}

	srv.stop(ctx)

	if err = srv.start(ctx); err != nil {
		return err
	}

	return secondStart(ctx, srv, api, report)
}

func origin(scheme string) string {
	return scheme + "://127.0.0.1:" + strconv.Itoa(apiPort)
}

func firstRun(ctx context.Context, srv *server, api *client, report *checks, webApp bool) error {
	plain := origin("http")

	health, up := api.waitUp(ctx, plain, startLimit)
	if !report.check("the server answers on its first start", up, "no answer in 90s") {
		return errStopped
	}

	fmt.Fprintln(report.out, "     version "+health.str("version"))

	found := setupCode.FindAllStringSubmatch(srv.log(ctx), -1)
	if !report.check("the log prints a setup code", len(found) > 0, "no setup_code= line") {
		return errStopped
	}

	code := found[len(found)-1][1]
	checkBanner(ctx, srv, report, webApp)

	state, err := api.call(ctx, http.MethodGet, plain+"/api/v1/setup", nil, nil)
	if err != nil {
		return err
	}

	report.check("the first run is pending", state.status == http.StatusOK && state.str("state") == "pending",
		state.text())

	certs, err := prepare(ctx, api, report, plain, code)
	if err != nil {
		return err
	}

	confirmed, err := api.call(ctx, http.MethodPost, plain+"/api/v1/setup", decision(code, certs), nil)
	if err != nil {
		return err
	}

	restarting, _ := confirmed.object["restarting"].(bool)
	report.check("confirm answers 202 and restarts", confirmed.status == http.StatusAccepted && restarting,
		confirmed.text())

	return afterConfirm(ctx, api, report, webApp)
}

func checkBanner(ctx context.Context, srv *server, report *checks, webApp bool) {
	banner := srv.log(ctx)
	if webApp {
		report.check("the banner names an address to open",
			strings.Contains(banner, "Open one of these addresses"), "no address in the banner")

		return
	}

	report.check("the banner names the API, not a browser", strings.Contains(banner, "serves no web app"),
		"the banner sends a person to a browser")
}

// prepare is what the wizard asks of the server before the confirm: the
// contract, the storage check and the certificates.
func prepare(ctx context.Context, api *client, report *checks, plain, code string) (reply, error) {
	withCode := map[string]string{"X-Setup-Code": code}

	contract, err := api.call(ctx, http.MethodGet, plain+"/api/v1/setup/configuration", nil, withCode)
	if err != nil {
		return reply{}, err
	}

	keys, _ := contract.object["keys"].([]any)
	report.check("the wizard reads its contract", contract.status == http.StatusOK && len(keys) > 20,
		contract.status)

	probe, err := api.call(ctx, http.MethodPost, plain+"/api/v1/setup/storage",
		map[string]string{"root": "./storage"}, withCode)
	if err != nil {
		return reply{}, err
	}

	writable, _ := probe.object["writable"].(bool)
	report.check("the storage folder is writable", probe.status == http.StatusOK && writable, probe.text())

	certs, err := api.call(ctx, http.MethodPost, plain+"/api/v1/setup/certificates",
		map[string][]string{"names": {"localhost", "127.0.0.1"}}, withCode)
	if err != nil {
		return reply{}, err
	}

	if !report.check("the wizard makes certificates",
		certs.status == http.StatusOK && certs.str("web_certificate") != "", certs.text()) {
		return reply{}, errStopped
	}

	return certs, nil
}

// decision is the confirm the wizard sends for these choices.
func decision(code string, certs reply) map[string]any {
	return map[string]any{
		"code":      code,
		"mode":      "required",
		"account":   map[string]string{"username": adminName, "password": adminPassword},
		"admission": "ask",
		"configuration": map[string]any{
			"core.api.tls.certificate": certs.str("web_certificate"),
			"core.api.tls.key":         certs.str("web_key"),
			"core.frontend.title":      title,
		},
		"storage": map[string]string{"root": "./storage"},
		"history": map[string]string{"retention": "168h"},
		"mqtt":    map[string]string{"url": "tcp://127.0.0.1:1883"},
		"printers": map[string]string{
			"addr":        ":" + strconv.Itoa(printerPort),
			"certificate": certs.str("printer_certificate"),
			"key":         certs.str("printer_key"),
		},
	}
}

// afterConfirm checks the server the confirm restarted into.
func afterConfirm(ctx context.Context, api *client, report *checks, webApp bool) error {
	secure := origin("https")

	_, up := api.waitUp(ctx, secure, startLimit)
	if !report.check("the restarted server answers over HTTPS", up, "no answer in 90s") {
		return errStopped
	}

	ready := api.waitResources(ctx, secure)
	report.check("every declared resource is served", ready.num("serving") == ready.num("declared"), ready.text())

	state, err := api.call(ctx, http.MethodGet, secure+"/api/v1/setup", nil, nil)
	if err != nil {
		return err
	}

	report.check("the first run is done, with sign-in required",
		state.status == http.StatusOK && state.str("state") == "done" && state.str("mode") == "required", state.text())
	report.check("a new printer waits for approval", state.str("admission") == "ask", state.text())

	if err = signIn(ctx, api, report, secure, "the administrator signs in"); err != nil {
		return err
	}

	if err = upload(ctx, api, report, secure); err != nil {
		return err
	}

	version := printerTLS(ctx)
	report.check("the printers' port speaks TLS 1.2", version == "TLS 1.2", version)

	return checkWebApp(ctx, api, report, secure, webApp)
}

func signIn(ctx context.Context, api *client, report *checks, secure, name string) error {
	login, err := api.call(ctx, http.MethodPost, secure+"/api/v1/auth/login",
		map[string]string{"username": adminName, "password": adminPassword}, nil)
	if err != nil {
		return err
	}

	report.check(name, login.status >= 200 && login.status < 300, login.status)

	return nil
}

func upload(ctx context.Context, api *client, report *checks, secure string) error {
	saved, err := api.call(ctx, http.MethodPut, secure+"/api/v1/storages/local/content?path=smoke/cube.gcode",
		[]byte("G28\nG1 X10\n"), nil)
	if err != nil {
		return err
	}

	report.check("a file uploads into the storage", saved.status == http.StatusOK || saved.status == http.StatusCreated,
		saved.text())

	return listed(ctx, api, report, secure, "the uploaded file is listed")
}

func listed(ctx context.Context, api *client, report *checks, secure, name string) error {
	listing, err := api.call(ctx, http.MethodGet, secure+"/api/v1/storages/local/files?path=smoke", nil, nil)
	if err != nil {
		return err
	}

	report.check(name, listing.status == http.StatusOK && strings.Contains(listing.text(), "cube.gcode"),
		listing.text())

	return nil
}

func checkWebApp(ctx context.Context, api *client, report *checks, secure string, webApp bool) error {
	settings, err := api.call(ctx, http.MethodGet, secure+"/api/v1/ui", nil, nil)
	if err != nil {
		return err
	}

	report.check("the web app settings carry the title",
		settings.status == http.StatusOK && strings.Contains(settings.text(), title), settings.status)

	page, err := api.call(ctx, http.MethodGet, secure+"/", nil, nil)
	if err != nil {
		return err
	}

	if webApp {
		report.check("the web app is served",
			page.status == http.StatusOK && strings.Contains(strings.ToLower(page.text()), "<html"), page.status)

		return nil
	}

	report.check("a build without the web app answers 404 at the root", page.status == http.StatusNotFound,
		page.status)

	return nil
}

// secondStart checks a server stopped and started again on the setup.
func secondStart(ctx context.Context, srv *server, api *client, report *checks) error {
	secure := origin("https")

	_, up := api.waitUp(ctx, secure, startLimit)
	if !report.check("the server starts again over HTTPS", up, "no answer in 90s") {
		return errStopped
	}

	ready := api.waitResources(ctx, secure)
	report.check("every declared resource is served again", ready.num("serving") == ready.num("declared"),
		ready.text())

	state, err := api.call(ctx, http.MethodGet, secure+"/api/v1/setup", nil, nil)
	if err != nil {
		return err
	}

	report.check("the first run stays done", state.status == http.StatusOK && state.str("state") == "done",
		state.text())

	if err = signIn(ctx, api, report, secure, "the administrator still signs in"); err != nil {
		return err
	}

	if err = listed(ctx, api, report, secure, "the uploaded file is still there"); err != nil {
		return err
	}

	log := srv.log(ctx)

	var errorsLogged []string

	for line := range strings.Lines(log) {
		if strings.Contains(line, "level=ERROR") {
			errorsLogged = append(errorsLogged, strings.TrimSpace(line))
		}
	}

	report.check("the log holds no ERROR line", len(errorsLogged) == 0, strings.Join(errorsLogged, "\n"))

	_, after, _ := strings.Cut(log, "the first-run wizard was confirmed")
	report.check("no setup code is printed after the first run", !strings.Contains(after, "setup_code="),
		"a setup code after the confirm")

	return nil
}
