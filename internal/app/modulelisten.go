package app

import (
	"fmt"
	"io"
	"net"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/app/config/install"
	"github.com/prusa3d/prusa-connect/internal/app/resource"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/sdk/manifest"
)

// The ports a module asks to listen on, and the directory it asks for. A
// port is reachable from the network, so it takes an approval of its own
// beside approve.

// dataRequest is the request line of a manifest that asks for a data
// directory, in the words requests and approved both use.
const dataRequest = "data directory"

// firstOpenPort is the lowest port a module is granted. A port below it opens
// only for the system administrator, and this server does not run as one.
const firstOpenPort = 1024

// portsHeld is what an install's ports are held to: the ports the declaration
// it replaces grants, the ask the operator approved with them, and every port
// the server or another instance already uses, named by its user.
type portsHeld struct {
	granted map[string]install.ListenPort
	before  *install.ModuleAsks
	inUse   map[install.ListenPort]string
}

// grantedListen is the ports an install grants: every port asked for when
// the operator approved the ports, each at the port the operator named for it
// or the one asked for. Without that approval it keeps each port the
// declaration it replaces already grants for the same ask, at the port the
// operator chose then. The whole set is checked before the install stops or
// writes anything.
func grantedListen(request moduleRequest, asked []install.ListenAsk, held portsHeld) ([]install.ListenAsk, error) {
	if err := listenRequestUsable(request, asked); err != nil {
		return nil, err
	}

	if !request.approve {
		return nil, nil
	}

	var granted []install.ListenAsk

	for _, ask := range asked {
		if request.approveListen {
			if moved, named := request.listen[ask.Name]; named {
				ask.Port = moved
			}

			granted = append(granted, ask)

			continue
		}

		if kept, found := held.kept(ask); found {
			ask.Port = kept.Port
			granted = append(granted, ask)
		}
	}

	return granted, portsFree(granted, held.inUse)
}

// kept is the port the declaration grants for this ask. A port is kept only
// when the operator approved the same name, network and reason before: a
// changed reason is a new request.
func (h portsHeld) kept(ask install.ListenAsk) (install.ListenPort, bool) {
	port, found := h.granted[ask.Name]
	if !found || port.Network != ask.Network || h.before == nil {
		return install.ListenPort{}, false
	}

	approved := slices.ContainsFunc(h.before.Listen, func(was install.ListenAsk) bool {
		return was.Name == ask.Name && was.Network == ask.Network && was.Why == ask.Why
	})

	return port, approved
}

// added is the request line of each granted port the declaration did not
// grant for the same ask: a new name, a new network or a new reason.
func (h portsHeld) added(granted []install.ListenAsk) []string {
	var out []string

	for _, given := range granted {
		if _, kept := h.kept(given); !kept {
			out = append(out, given.Request())
		}
	}

	return out
}

// portsFree refuses a granted set that puts two names on one port, a port
// below firstOpenPort, and a port the server or another instance uses.
func portsFree(granted []install.ListenAsk, inUse map[install.ListenPort]string) error {
	seen := make(map[install.ListenPort]string, len(granted))

	for _, given := range granted {
		port := install.ListenPort{Network: given.Network, Port: given.Port}

		if given.Port < firstOpenPort {
			return fmt.Errorf("%w: %s as %s is below %d, and only the system administrator may open such "+
				"a port. Move it with listen %s=<port>", ErrUsage, port, given.Name, firstOpenPort, given.Name)
		}

		if other, taken := seen[port]; taken {
			return fmt.Errorf("%w: %s is granted to both %s and %s. Move one of them with listen <name>=<port>",
				ErrUsage, port, other, given.Name)
		}

		if user, taken := inUse[port]; taken {
			return fmt.Errorf("%w: %s as %s is already used by %s. Move it with listen %s=<port>",
				ErrUsage, port, given.Name, user, given.Name)
		}

		seen[port] = given.Name
	}

	return nil
}

// portsInUse is every port the server listens on and every port another
// instance's declaration grants, each named by who uses it. The server's own
// MQTT broker and the device listeners are resources with settings of their
// own, and are not read here.
func portsInUse(core config.Core, doc resource.Document, instance ref.Ref) map[install.ListenPort]string {
	used := map[install.ListenPort]string{}

	for _, own := range []struct{ key, addr string }{
		{"core.api.addr", core.API.Addr},
		{"core.observability.addr", core.Observability.Addr},
	} {
		if port, ok := tcpPortOf(own.addr); ok {
			used[port] = "the server's " + own.key
		}
	}

	for _, declared := range doc.Instances {
		if declared.Ref == instance || declared.Spec.Service == nil {
			continue
		}

		for name, port := range declared.Spec.Service.Listen {
			used[port] = declared.Ref.String() + " as " + name
		}
	}

	return used
}

// tcpPortOf is the TCP port of a host:port address, and false for an address
// that names none.
func tcpPortOf(addr string) (install.ListenPort, bool) {
	_, number, err := net.SplitHostPort(strings.TrimSpace(addr))
	if err != nil {
		return install.ListenPort{}, false
	}

	port, err := strconv.Atoi(number)
	if err != nil || port < 1 {
		return install.ListenPort{}, false
	}

	return install.ListenPort{Network: manifest.NetworkTCP, Port: port}, true
}

// listenRequestUsable refuses a port approval without approve, a moved port
// without the approval, a name the manifest does not list, and a port
// outside 1 to 65535.
func listenRequestUsable(request moduleRequest, asked []install.ListenAsk) error {
	if request.approveListen && !request.approve {
		return fmt.Errorf("%w: approve_listen is given only with approve", ErrUsage)
	}

	if len(request.listen) > 0 && !request.approveListen {
		return fmt.Errorf("%w: listen moves a port only with approve_listen", ErrUsage)
	}

	for name, port := range request.listen {
		if !slices.ContainsFunc(asked, func(ask install.ListenAsk) bool { return ask.Name == name }) {
			return fmt.Errorf("%w: listen names %q, and the manifest lists no port by that name", ErrUsage, name)
		}

		if port < 1 || port > manifest.MaxPort {
			return fmt.Errorf("%w: listen.%s is %d, expected 1 to %d", ErrUsage, name, port, manifest.MaxPort)
		}
	}

	return nil
}

// openPorts is the request line of each port asked for and not granted.
func openPorts(asked, granted []install.ListenAsk) []string {
	var out []string

	for _, ask := range asked {
		if !slices.ContainsFunc(granted, func(given install.ListenAsk) bool { return given.Name == ask.Name }) {
			out = append(out, ask.Request())
		}
	}

	return out
}

// listenAsksOf is a manifest's listen list in the words the declaration
// records.
func listenAsksOf(declared []manifest.Listen) []install.ListenAsk {
	if len(declared) == 0 {
		return nil
	}

	out := make([]install.ListenAsk, 0, len(declared))
	for _, listen := range declared {
		out = append(out, install.ListenAsk{
			Name: listen.Name, Network: listen.Network, Port: listen.Port, Why: listen.Why,
		})
	}

	return out
}

// manifestListen is the granted ports written back into the manifest the
// rest of the install reads.
func manifestListen(granted []install.ListenAsk) []manifest.Listen {
	if len(granted) == 0 {
		return nil
	}

	out := make([]manifest.Listen, 0, len(granted))
	for _, given := range granted {
		out = append(out, manifest.Listen{
			Name: given.Name, Network: given.Network, Port: given.Port, Why: given.Why,
		})
	}

	return out
}

// declaredListen is the granted ports in the declaration's spelling, and nil
// for none.
func declaredListen(granted []manifest.Listen) map[string]string {
	if len(granted) == 0 {
		return nil
	}

	out := make(map[string]string, len(granted))
	for _, given := range granted {
		out[given.Name] = install.ListenPort{Network: given.Network, Port: given.Port}.String()
	}

	return out
}

// heldListen is the ports the declaration an install replaces grants, and
// nil for none.
func heldListen(declared resource.Declaration) map[string]install.ListenPort {
	if declared.Spec.Service == nil {
		return nil
	}

	return declared.Spec.Service.Listen
}

// parseListenFlag reads -listen: name=port pairs separated by commas.
func parseListenFlag(written string) (map[string]int, error) {
	if strings.TrimSpace(written) == "" {
		return nil, nil //nolint:nilnil // no -listen moves no port, and nothing went wrong.
	}

	out := map[string]int{}

	for pair := range strings.SplitSeq(written, ",") {
		name, number, found := strings.Cut(strings.TrimSpace(pair), "=")
		if !found {
			return nil, fmt.Errorf("%w: -listen takes name=port pairs, and %q is not one", ErrUsage, pair)
		}

		port, err := strconv.Atoi(strings.TrimSpace(number))
		if err != nil {
			return nil, fmt.Errorf("%w: -listen %s names the port %q, expected a number", ErrUsage, name, number)
		}

		name = strings.TrimSpace(name)
		if _, twice := out[name]; twice {
			return nil, fmt.Errorf("%w: -listen names %s twice", ErrUsage, name)
		}

		out[name] = port
	}

	return out, nil
}

// listenMoves is the ports an install request moves, in the words the
// command reads from -listen.
func listenMoves(moved map[string]httpapi.ModulePort) map[string]int {
	if len(moved) == 0 {
		return nil
	}

	out := make(map[string]int, len(moved))
	for name, port := range moved {
		out[name] = int(port)
	}

	return out
}

// moduleDataFolder is the folder beside the modules directory that holds one
// data directory per module.
const moduleDataFolder = "module-data"

// moduleDataRoot is where the modules' data directories are made: beside
// core.modules, in a folder an update and a removal never touch.
func moduleDataRoot(core config.Core) string {
	if strings.TrimSpace(core.Modules) == "" {
		return ""
	}

	return filepath.Join(filepath.Dir(filepath.Clean(core.Modules)), moduleDataFolder)
}

// listensGranted is each port an approved install grants, as the install
// output names it.
func listensGranted(declared []manifest.Listen) []string {
	out := make([]string, 0, len(declared))

	for _, listen := range declared {
		out = append(out, fmt.Sprintf("%s/%d as %s (%s)", listen.Network, listen.Port, listen.Name, listen.Why))
	}

	return out
}

// reportListens prints the ports an install granted.
func reportListens(out io.Writer, listens []string) {
	for _, listen := range listens {
		fmt.Fprintf(out, "  listens on  %s\n", listen)
	}
}

// reportPortsOpen prints each port an enabled install did not grant.
func reportPortsOpen(out io.Writer, ports []string) {
	for _, port := range ports {
		fmt.Fprintf(out, "  not granted %s. Run the same line with -approve -approve-listen\n", port)
	}
}
