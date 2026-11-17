package install

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/prusa3d/prusa-connect/sdk/manifest"
)

// The ports a declaration grants a module.

// ListenPort is one port a module was granted: the network and the number.
type ListenPort struct {
	Network manifest.Network
	Port    int
}

// String is the port as the declaration writes it, such as udp/13514.
func (p ListenPort) String() string {
	return string(p.Network) + listenSeparator + strconv.Itoa(p.Port)
}

// listenSeparator joins the network and the port in a declaration.
const listenSeparator = "/"

// ParseListenPort reads one port as the declaration writes it: udp or tcp, a
// slash, and a port from 1 to 65535.
func ParseListenPort(written string) (ListenPort, error) {
	network, number, found := strings.Cut(strings.TrimSpace(written), listenSeparator)
	if !found {
		return ListenPort{}, fmt.Errorf("%q is not a port, expected udp/<port> or tcp/<port>", written)
	}

	held := ListenPort{Network: manifest.Network(network)}
	if held.Network != manifest.NetworkUDP && held.Network != manifest.NetworkTCP {
		return ListenPort{}, fmt.Errorf("%q names the network %q, expected udp or tcp", written, network)
	}

	port, err := strconv.Atoi(number)
	if err != nil || port < 1 || port > manifest.MaxPort {
		return ListenPort{}, fmt.Errorf("%q names the port %q, expected 1 to %d", written, number, manifest.MaxPort)
	}

	held.Port = port

	return held, nil
}

// listenOf reads the ports a declaration grants, refusing a name the manifest
// rule would refuse and a port written in any other shape.
func listenOf(declared map[string]string) (map[string]ListenPort, error) {
	if len(declared) == 0 {
		return nil, nil //nolint:nilnil // no grant is no map, and nothing went wrong.
	}

	out := make(map[string]ListenPort, len(declared))
	seen := make(map[ListenPort]string, len(declared))

	for name, written := range declared {
		if !manifest.IsListenName(name) {
			return nil, fmt.Errorf("service.listen names %q, expected lower case letters, digits and hyphens", name)
		}

		held, err := ParseListenPort(written)
		if err != nil {
			return nil, fmt.Errorf("service.listen.%s: %w", name, err)
		}

		if other, taken := seen[held]; taken {
			return nil, fmt.Errorf("service.listen grants %s to both %s and %s", held, other, name)
		}

		seen[held] = name
		out[name] = held
	}

	return out, nil
}
