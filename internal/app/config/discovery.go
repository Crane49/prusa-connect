package config

import (
	"fmt"
	"regexp"
)

// The service names this server announces on the local network. A service name
// is what a client asks for over mDNS, which is DNS sent to every machine on
// the same link.
const (
	// ServicePrusaLink is the name a Prusa printer answers to, so a client that
	// looks for a printer finds this server as well.
	ServicePrusaLink = "_prusalink._tcp"
	// ServicePrusaConnect says a server answered and not a printer. A client
	// that reads it knows it found a fleet and not one machine.
	ServicePrusaConnect = "_prusa-connect._tcp"
)

// NearbyServices are the service names the listener asks for, in order of
// priority: a machine that announces several is listed under the first, so a
// printer that runs a server is listed as the server.
func NearbyServices() []string { return []string{ServicePrusaConnect, ServicePrusaLink} }

// Who answers for this machine's host name.
const (
	// HostRecordsOwn is the server's own responder: it claims the host name
	// and answers with this machine's addresses.
	HostRecordsOwn = "own"
	// HostRecordsSystem is a responder the operating system runs on the same
	// machine, such as Avahi.
	HostRecordsSystem = "system"
)

// serviceNamePattern is the shape RFC 6763 section 7 gives a service name: an
// underscore, 1 to 15 letters, digits or hyphens, then the transport. No
// client asks for a name outside it, so such a value is refused at startup.
//
//nolint:gochecknoglobals // a compiled pattern read at validation time, never written.
var serviceNamePattern = regexp.MustCompile(`^_[A-Za-z0-9-]{1,15}\._(tcp|udp)$`)

// Discovery is the announcement this server sends on the local network, so a
// slicer on the same network finds it without a typed address.
//
// Enabled is a pointer for the reason core.frontend.enabled is one: an operator
// who wrote `enabled: false` is told apart from one who wrote no block, and
// only the first means off.
type Discovery struct {
	// Enabled announces this server's API socket. The default is on, in every
	// deployment. Inside a container without host networking the packets stay on
	// the bridge. The startup line says so, and the announcement stays on.
	Enabled *bool `yaml:"enabled"`
	// Services are the service names announced, one record for each. An empty
	// list is the shipped pair, because `enabled: false` is the only switch.
	Services []string `yaml:"services"`
	// Hidden is the value the hidden setting starts with. A hidden server
	// announces nothing, so the others on the network do not list it, and it
	// still serves its page and its API. A person changes the setting in the
	// web app, and that choice is stored in the machine-owned file, where it
	// wins over this key.
	Hidden bool `yaml:"hidden"`
	// HostRecords says who answers for the host name: HostRecordsOwn, or
	// HostRecordsSystem, which leaves it to a responder the operating system
	// runs on the same machine. The server then announces its services alone,
	// each naming that host, and never claims or renames the host name.
	HostRecords string `yaml:"host_records"`
}

// SystemHost reports that the operating system's responder answers for the
// host name.
func (d Discovery) SystemHost() bool { return d.HostRecords == HostRecordsSystem }

// On reports whether this server announces itself. An absent key is on.
func (d Discovery) On() bool { return d.Enabled == nil || *d.Enabled }

// validate refuses a service name no browser would ever ask for.
func (d Discovery) validate() error {
	for _, service := range d.Services {
		if !serviceNamePattern.MatchString(service) {
			return fmt.Errorf("%w: core.discovery.services has %q, expected a name such as %q: an "+
				"underscore, 1 to 15 letters, digits or hyphens, then ._tcp or ._udp "+
				"(RFC 6763 section 7)", ErrInvalid, service, ServicePrusaLink)
		}
	}

	if d.HostRecords != HostRecordsOwn && d.HostRecords != HostRecordsSystem {
		return fmt.Errorf("%w: core.discovery.host_records is %q, expected %q or %q",
			ErrInvalid, d.HostRecords, HostRecordsOwn, HostRecordsSystem)
	}

	return nil
}

// withDefaults fills the announcement's keys. Enabled is filled too, so a
// reader of the filled value never meets a nil.
func (d Discovery) withDefaults(defaults Discovery) Discovery {
	d.Enabled = orDefault(d.Enabled, defaults.Enabled)

	if len(d.Services) == 0 {
		d.Services = defaults.Services
	}

	if d.HostRecords == "" {
		d.HostRecords = defaults.HostRecords
	}

	return d
}
