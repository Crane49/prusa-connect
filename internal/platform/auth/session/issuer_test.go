package session

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/prusa3d/prusa-connect/internal/platform/auth/oidc"
)

// The issuer these tests run against. Every one of them talks to a real
// OpenID Connect provider over a real socket: there is no fake issuer in this
// package, because a fake issuer is the bypass that docker/mock-oidc exists to
// make unnecessary.
//
//	docker compose -f docker-compose.local.yml up -d mock-oidc
const (
	defaultIssuer      = "http://localhost:58092"
	defaultClientID    = "prusa-connect"
	defaultRedirectURI = "http://localhost:8080/api/v1/auth/callback"

	// testSubject is a user in usersFile.
	testSubject = "operator"

	// usersFile declares which subjects the issuer authenticates. The container
	// is started from it, so it is what separates this repository's issuer from
	// any other issuer reachable at the same address.
	usersFile = "docker/mock-oidc/config/users.yaml"

	// usersFilePath reaches usersFile from this package's directory.
	usersFilePath = "../../../../" + usersFile
)

// issuerURL is where the provider is, overridable for a CI runner that
// published a different port.
func issuerURL() string {
	if issuer := os.Getenv("PCL_OIDC_ISSUER"); issuer != "" {
		return issuer
	}

	return defaultIssuer
}

// newIssuerClient returns a client against the running provider, or skips. A
// skip is honest about what was not proved; it is not a path through the code
// that admits anybody.
func newIssuerClient(t *testing.T) *oidc.Client {
	t.Helper()

	issuer := issuerURL()

	client, err := oidc.New(t.Context(), oidc.Config{
		Issuer:      issuer,
		ClientID:    defaultClientID,
		RedirectURI: defaultRedirectURI,
		HTTPClient:  noRedirectClient(),
	})
	if err != nil {
		t.Skipf("no OpenID Connect provider at %s (%v).\n"+
			"Start one: docker compose -f docker-compose.local.yml up -d mock-oidc", issuer, err)
	}

	return client
}

// noRedirectClient is the client a server uses to talk to an issuer. It follows
// no redirect, so the test can read one.
func noRedirectClient() *http.Client {
	return &http.Client{
		Timeout:       10 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// declaredSubjects reads the subjects usersFile names, sorted. Every test in
// this package compares against the same list, so the file is read once.
var declaredSubjects = sync.OnceValues(func() ([]string, error) {
	raw, err := os.ReadFile(usersFilePath)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", usersFile, err)
	}

	var users []struct {
		Sub string `yaml:"sub"`
	}

	if parseErr := yaml.Unmarshal(raw, &users); parseErr != nil {
		return nil, fmt.Errorf("parse %s: %w", usersFile, parseErr)
	}

	subjects := make([]string, 0, len(users))
	for _, user := range users {
		subjects = append(subjects, user.Sub)
	}

	slices.Sort(subjects)

	return subjects, nil
})

// loginPageOption matches one entry of the subject list a login page presents.
var loginPageOption = regexp.MustCompile(`<option value="([^"]*)"`)

// offeredSubjects reads the subject list out of a login page, sorted.
func offeredSubjects(page string) []string {
	matches := loginPageOption.FindAllStringSubmatch(page, -1)

	subjects := make([]string, 0, len(matches))
	for _, match := range matches {
		subjects = append(subjects, match[1])
	}

	slices.Sort(subjects)

	return subjects
}

// checkIssuerIsOurs reports whether a login page came from the issuer usersFile
// describes. An address proves nothing about who answered on it, and any stack
// on the same host may publish an issuer of its own on the same port. The
// subject list is what tells two issuers apart, so a foreign one is named here
// instead of reaching a test as a missing option.
func checkIssuerIsOurs(issuer, page string, declared []string) error {
	offered := offeredSubjects(page)
	if slices.Equal(offered, declared) {
		return nil
	}

	return fmt.Errorf(
		"the issuer at %s is not this repository's mock issuer.\n"+
			"it offers %s.\n"+
			"%s declares %s.\n"+
			"another service holds that port. start this repository's issuer on a free port,"+
			" then point the suite at it:\n"+
			"    PCL_OIDC_PORT=<free port> docker compose -f docker-compose.local.yml up -d mock-oidc\n"+
			"    PCL_OIDC_ISSUER=http://localhost:<free port> go test ./internal/platform/auth/...",
		issuer, subjectList(offered), usersFile, subjectList(declared))
}

// subjectList renders a subject list for a failure message.
func subjectList(subjects []string) string {
	if len(subjects) == 0 {
		return "no subjects at all"
	}

	return strings.Join(subjects, ", ")
}

// TestTheSuiteRefusesAnIssuerWhoseSubjectsAreNotTheFiles drives the guard both
// ways, because a guard that only proves the subject it wants was offered
// passes against any issuer that happens to name the same one.
func TestTheSuiteRefusesAnIssuerWhoseSubjectsAreNotTheFiles(t *testing.T) {
	t.Parallel()

	declared, err := declaredSubjects()
	require.NoError(t, err)
	require.Contains(t, declared, testSubject, "%s no longer declares %q", usersFile, testSubject)

	issuer := issuerURL()

	require.NoError(t, checkIssuerIsOurs(issuer, loginPageOffering(declared...), declared),
		"an issuer started from %s must be accepted", usersFile)

	// The subject list another stack's issuer presents.
	foreign := []string{"ithan", "lukas", "outsider", "stranger", "unverified"}

	err = checkIssuerIsOurs(issuer, loginPageOffering(foreign...), declared)
	require.Error(t, err, "an issuer offering somebody else's subjects must be refused")
	require.Contains(t, err.Error(), issuer)
	require.Contains(t, err.Error(), strings.Join(foreign, ", "))
	require.Contains(t, err.Error(), strings.Join(declared, ", "))
	t.Logf("a foreign issuer reads:\n%v", err)

	// A page offering every subject the file names and one more is a different
	// issuer, and every test here would otherwise run against it.
	extra := append([]string{"somebody-else"}, declared...)
	require.Error(t, checkIssuerIsOurs(issuer, loginPageOffering(extra...), declared),
		"an issuer offering the file's subjects and others must be refused")

	require.Error(t, checkIssuerIsOurs(issuer, "<html>not an issuer at all</html>", declared),
		"a page with no subject list must be refused")
}

// loginPageOffering builds the part of a login page this suite reads: the
// select whose value the page submits back as `sub`.
func loginPageOffering(subjects ...string) string {
	var page strings.Builder

	page.WriteString(`<select id="sub" name="sub" autofocus>`)

	for _, subject := range subjects {
		fmt.Fprintf(&page, `<option value="%s">%s</option>`, subject, subject)
	}

	page.WriteString(`</select>`)

	return page.String()
}

// authorize walks the browser half of the flow: it fetches the authorization
// endpoint, checks that this repository's login page came back, then submits
// the subject the same way the page's own form does, and reads the code out of
// the redirect.
//
// This is the browser, driven from Go. Nothing about the server's own code path
// changes because a test is what is holding the browser's end.
func authorize(t *testing.T, authorizationURL string) (code string, state string) {
	t.Helper()

	client := noRedirectClient()

	asked, err := url.Parse(authorizationURL)
	require.NoError(t, err)

	origin := asked.Scheme + "://" + asked.Host

	page := fetch(t, client, authorizationURL)
	require.Equal(t, http.StatusOK, page.status, "the authorization endpoint did not present a login page")

	declared, err := declaredSubjects()
	require.NoError(t, err)
	require.Contains(t, declared, testSubject, "%s no longer declares %q", usersFile, testSubject)

	if foreign := checkIssuerIsOurs(origin, page.body, declared); foreign != nil {
		t.Fatal(foreign)
	}

	// The login page embeds these in its form and submits them back. Carrying
	// them here is what the page's own script does.
	form := url.Values{}
	for _, name := range []string{"redirect_uri", "code_challenge", "code_challenge_method", "scope", "state", "nonce"} {
		form.Set(name, asked.Query().Get(name))
	}

	form.Set("sub", testSubject)

	submitted := fetch(t, client, origin+"/api/form?"+form.Encode())

	require.Equal(t, http.StatusFound, submitted.status, "submitting the login form did not redirect")

	back, err := url.Parse(submitted.location)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(submitted.location, defaultRedirectURI),
		"the issuer redirected to %q rather than the registered redirect uri", submitted.location)

	code, state = back.Query().Get("code"), back.Query().Get("state")
	require.NotEmpty(t, code, "no authorization code came back: %s", submitted.location)

	return code, state
}

type httpResult struct {
	status   int
	location string
	body     string
}

func fetch(t *testing.T, client *http.Client, target string) httpResult {
	t.Helper()

	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, target, nil)
	require.NoError(t, err)

	response, err := client.Do(request)
	require.NoError(t, err, "GET %s", target)

	defer func() { require.NoError(t, response.Body.Close()) }()

	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	require.NoError(t, err)

	return httpResult{
		status:   response.StatusCode,
		location: response.Header.Get("Location"),
		body:     string(body),
	}
}
