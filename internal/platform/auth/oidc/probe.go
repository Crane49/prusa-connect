package oidc

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

// Verdict is what one probe of an issuer learned.
type Verdict string

// The verdicts a probe gives.
const (
	// VerdictPassed is an issuer that answered the way a working login needs.
	VerdictPassed Verdict = "passed"
	// VerdictFailed is an issuer that refused what a login would send.
	VerdictFailed Verdict = "failed"
	// VerdictUnknown is an answer the probe cannot read either way.
	VerdictUnknown Verdict = "unknown"
)

// Finding is one probe's verdict and what the issuer said.
type Finding struct {
	Verdict Verdict
	Detail  string
}

// probeCode is the authorization code the client probe sends. No issuer
// issued it, so a token endpoint that authenticated the client refuses the
// code and not the client.
const probeCode = "prusa-connect-probe-not-a-code"

// maxProbeBytes bounds what an issuer's answer to a probe is read up to.
const maxProbeBytes = 64 << 10

// oauthError is the error body RFC 6749 section 5.2 defines.
type oauthError struct {
	Error       string `json:"error"`
	Description string `json:"error_description"`
}

func (e oauthError) String() string {
	if e.Description == "" {
		return e.Error
	}

	return e.Error + ": " + e.Description
}

// ProbeClient asks the token endpoint whether it accepts this client, without
// a login. It sends an authorization code nobody issued, with the client's
// secret when it has one. RFC 6749 section 5.2 has a token endpoint
// authenticate the client before it reads the code, so invalid_grant means
// the client id and secret were taken, and invalid_client means they were
// not.
func (c *Client) ProbeClient(ctx context.Context) Finding {
	pkce, err := NewPKCE()
	if err != nil {
		return Finding{Verdict: VerdictUnknown, Detail: err.Error()}
	}

	response, err := c.postCode(ctx, probeCode, pkce)
	if err != nil {
		return Finding{Verdict: VerdictFailed, Detail: err.Error()}
	}
	defer func() { _ = response.Body.Close() }()

	body, _ := io.ReadAll(io.LimitReader(response.Body, maxProbeBytes))

	var said oauthError
	_ = json.Unmarshal(body, &said)

	switch {
	case said.Error == "invalid_grant":
		return Finding{Verdict: VerdictPassed, Detail: said.String()}
	case response.StatusCode == http.StatusUnauthorized,
		said.Error == "invalid_client", said.Error == "unauthorized_client", said.Error == "invalid_request":
		return Finding{Verdict: VerdictFailed, Detail: statusAnd(response.StatusCode, said.String(), body)}
	default:
		return Finding{Verdict: VerdictUnknown, Detail: statusAnd(response.StatusCode, said.String(), body)}
	}
}

// ProbeRedirect opens the authorization endpoint the way a browser would at
// the start of a login, and follows no redirect. An issuer that sends the
// browser on, to its sign-in page or back to the redirect address, took the
// client id and the redirect address. One that answers 4xx refused them,
// which is how most issuers report an address that is not registered. An
// issuer may also show that refusal on a page it answers 200 with, so a pass
// here is not proof.
func (c *Client) ProbeRedirect(ctx context.Context) Finding {
	pkce, err := NewPKCE()
	if err != nil {
		return Finding{Verdict: VerdictUnknown, Detail: err.Error()}
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.AuthorizationURL("prusa-connect-probe", "prusa-connect-probe", pkce), nil)
	if err != nil {
		return Finding{Verdict: VerdictUnknown, Detail: err.Error()}
	}

	stay := *c.http
	stay.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

	response, err := stay.Do(request)
	if err != nil {
		return Finding{Verdict: VerdictFailed, Detail: fmt.Sprintf("get %s: %v", c.meta.AuthorizationEndpoint, err)}
	}
	defer func() { _ = response.Body.Close() }()

	body, _ := io.ReadAll(io.LimitReader(response.Body, maxProbeBytes))

	switch {
	case response.StatusCode >= http.StatusMultipleChoices && response.StatusCode < http.StatusBadRequest:
		location := response.Header.Get("Location")
		if strings.HasPrefix(location, c.config.RedirectURI) {
			return Finding{
				Verdict: VerdictPassed,
				Detail:  "the issuer sends the browser back to " + c.config.RedirectURI,
			}
		}

		if errorPage(location) {
			return Finding{Verdict: VerdictFailed, Detail: "the issuer sends the browser to an error page: " + location}
		}

		return Finding{Verdict: VerdictPassed, Detail: "the issuer sends the browser on to sign in"}
	case response.StatusCode >= http.StatusOK && response.StatusCode < http.StatusMultipleChoices:
		return Finding{Verdict: VerdictPassed, Detail: "the issuer answered with a page"}
	case response.StatusCode >= http.StatusBadRequest && response.StatusCode < http.StatusInternalServerError:
		return Finding{Verdict: VerdictFailed, Detail: statusAnd(response.StatusCode, titleOf(body), nil)}
	default:
		return Finding{Verdict: VerdictUnknown, Detail: statusAnd(response.StatusCode, titleOf(body), nil)}
	}
}

// errorPage reports a redirect to the issuer's own error page, which is how
// some issuers refuse an unknown client or redirect address: Google sends the
// browser to /signin/oauth/error. An "error" in the path or a query key says
// so; the sign-in page itself carries neither.
func errorPage(location string) bool {
	target, err := url.Parse(location)
	if err != nil {
		return false
	}

	if strings.Contains(strings.ToLower(target.Path), "error") {
		return true
	}

	for key := range target.Query() {
		if strings.Contains(strings.ToLower(key), "error") {
			return true
		}
	}

	return false
}

// statusAnd is a status code and what the issuer said with it, or the start of
// its body when it said nothing an OAuth client reads.
func statusAnd(status int, said string, body []byte) string {
	if said == "" {
		said = summarise(body)
	}

	if said == "" {
		return fmt.Sprintf("status %d", status)
	}

	return fmt.Sprintf("status %d: %s", status, said)
}

var pageTitle = regexp.MustCompile(`(?is)<title[^>]*>\s*(.*?)\s*</title>`)

// titleOf is the title of an HTML page, which is where an issuer names the
// error it shows a browser.
func titleOf(body []byte) string {
	match := pageTitle.FindSubmatch(body)
	if match == nil {
		return ""
	}

	return strings.Join(strings.Fields(string(match[1])), " ")
}
