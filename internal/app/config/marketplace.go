package config

import (
	"github.com/prusa3d/prusa-connect/sdk/manifest"

	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/extension"
)

// DefaultMarketplaceTimeout is how long one read of the marketplace catalogue
// waits. It is the timeout this server gives an identity provider, for the
// same reason: a remote that is slow must not make a settings screen hang.
const DefaultMarketplaceTimeout = 10 * time.Second

// DefaultMarketplaceURL is the Prusa Research marketplace. A deployment reads
// its catalogue unless the operator switches the marketplace off.
const DefaultMarketplaceURL = "https://plugin-marketplace.prusa3d.com"

// Marketplace is where this deployment reads a catalogue of modules from. It
// is on by default. An operator who wants no catalogue writes enabled: false,
// and nothing is then fetched.
type Marketplace struct {
	// Enabled is whether the catalogue is read at all. Absent is on.
	Enabled *bool `yaml:"enabled"`
	// URL is the marketplace's origin, DefaultMarketplaceURL when absent. The
	// catalogue is read from its /api/connect/v1/extensions.
	URL string `yaml:"url"`
	// Timeout bounds one read of the catalogue.
	Timeout time.Duration `yaml:"timeout"`
}

// The reasons a marketplace address is refused, each phrased to follow it.
var (
	errMarketplaceScheme = errors.New("it is not an https address, and only a loopback host may use http")
	errMarketplaceHost   = errors.New("it names no host")
	errMarketplaceExtra  = errors.New("it carries a query, a fragment or a user, and an origin carries none")
)

// Configured reports whether this deployment reads a marketplace at all: it is
// enabled and names one.
func (m Marketplace) Configured() bool {
	return (m.Enabled == nil || *m.Enabled) && strings.TrimSpace(m.URL) != ""
}

// Base is the URL with no trailing slash, so a path is appended to it as is.
func (m Marketplace) Base() string { return strings.TrimRight(strings.TrimSpace(m.URL), "/") }

// withDefaults fills the keys an operator left out. The marketplace is on, at
// DefaultMarketplaceURL, until the operator says otherwise.
func (m Marketplace) withDefaults(defaults Marketplace) Marketplace {
	m.Enabled = orDefault(m.Enabled, defaults.Enabled)

	if strings.TrimSpace(m.URL) == "" {
		m.URL = defaults.URL
	}

	if m.Timeout == 0 {
		m.Timeout = defaults.Timeout
	}

	return m
}

func (m Marketplace) validate() error {
	if m.Timeout <= 0 {
		return fmt.Errorf("%w: core.marketplace.timeout is %s, expected a positive duration", ErrInvalid, m.Timeout)
	}

	if !m.Configured() {
		return nil
	}

	if err := ValidRemoteURL(m.URL); err != nil {
		return fmt.Errorf("%w: core.marketplace.url is %q, %w. Expected an address such as "+
			"https://plugins.example", ErrInvalid, m.URL, err)
	}

	return nil
}

// ValidRemoteURL holds an address this server fetches somebody else's bytes
// from: https, or http to a loopback host for a marketplace on the same
// machine. It is exported because a release a catalogue names is held to the
// same rule.
func ValidRemoteURL(raw string) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return errMarketplaceScheme
	}

	if parsed.Host == "" || parsed.Hostname() == "" {
		return errMarketplaceHost
	}

	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return errMarketplaceExtra
	}

	switch parsed.Scheme {
	case schemeHTTPS:
		return nil
	case schemeHTTP:
		if IsLoopbackHost(parsed.Hostname()) {
			return nil
		}
	}

	return errMarketplaceScheme
}

// IsLoopbackHost reports a host name that can only be this machine: localhost,
// or a loopback IP address. A name that resolves to loopback does not count,
// because what it resolves to can change between the check and the dial.
func IsLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}

	ip := net.ParseIP(host)

	return ip != nil && ip.IsLoopback()
}

// validateModules checks where installed modules are recorded, the release
// directories offered as local installs, the memory pool, and the marketplace
// they come from.
func (c Core) validateModules() error {
	if c.Modules == "" {
		return fmt.Errorf("%w: core.modules is empty", ErrInvalid)
	}

	if err := validateModulesLocal(c.ModulesLocal); err != nil {
		return err
	}

	if err := c.validateModulesMemory(); err != nil {
		return err
	}

	if _, err := manifest.ParseStability(c.ModulesMinimumStability); err != nil {
		return fmt.Errorf("%w: core.modules_minimum_stability: %w", ErrInvalid, err)
	}

	return c.Marketplace.validate()
}

func validateModulesLocal(local map[string]string) error {
	for module, from := range local {
		if _, err := extension.NamespaceOf(module); err != nil {
			return fmt.Errorf("%w: core.modules_local names %q, which %w", ErrInvalid, module, err)
		}

		if strings.TrimSpace(from) == "" {
			return fmt.Errorf("%w: core.modules_local.%s is empty, expected a release directory",
				ErrInvalid, module)
		}
	}

	return nil
}
