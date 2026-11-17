package config

import (
	"fmt"
	"net/url"
	"strings"
)

// DefaultDiagnosticsSendURL is the Prusa beta portal. Settings offers to send
// the diagnostics bundle there, and the browser uploads it.
const DefaultDiagnosticsSendURL = "https://beta.prusa3d.com"

// Diagnostics is the support bundle an org admin downloads from Settings.
//
// Enabled is a pointer for the reason Dashboards.Enabled is one: only an
// operator who wrote `enabled: false` switches it off. `make build` reads the
// same key, and the minimal build leaves the bundle out whatever it says.
type Diagnostics struct {
	// Enabled serves the bundle and keeps the newest log records and the last
	// crash for it. The default is on.
	Enabled *bool `yaml:"enabled"`
	// SendURL is the portal the browser uploads the bundle to when the admin
	// chooses Send to Prusa. It is a pointer so that an operator who wrote an
	// empty string, which hides the button, is told apart from one who wrote
	// nothing and gets DefaultDiagnosticsSendURL.
	SendURL *string `yaml:"send_url"`
}

// On reports whether the configuration leaves the bundle on. An absent key is
// on.
func (d Diagnostics) On() bool { return d.Enabled == nil || *d.Enabled }

// Explicit reports whether the operator wrote `enabled: true`, which an open
// install needs: there everyone on the network is an admin.
func (d Diagnostics) Explicit() bool { return d.Enabled != nil && *d.Enabled }

// SendTarget is the portal the browser sends the bundle to, with no trailing
// slash, or empty when the operator switched sending off.
func (d Diagnostics) SendTarget() string {
	if d.SendURL == nil {
		return DefaultDiagnosticsSendURL
	}

	return strings.TrimRight(strings.TrimSpace(*d.SendURL), "/")
}

// validate refuses a send address the browser could not post to: one that is
// not an absolute http or https URL, or one carrying a user, a query or a
// fragment, which the upload path is appended after.
func (d Diagnostics) validate() error {
	target := d.SendTarget()
	if target == "" {
		return nil
	}

	parsed, err := url.Parse(target)
	if err != nil || (parsed.Scheme != schemeHTTP && parsed.Scheme != schemeHTTPS) ||
		parsed.Hostname() == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("%w: core.diagnostics.send_url is %q, expected an http or https address "+
			"such as %s", ErrInvalid, target, DefaultDiagnosticsSendURL)
	}

	return nil
}
