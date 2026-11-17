package discovery

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"os"
	"strings"
)

// The two ways a process learns it is inside a container. Docker writes the
// file into every container it starts, and Kubernetes sets the name in every
// pod. Neither is configuration: the core takes that from its own file. They
// say where the process is, and the second startup line is written only
// inside a container.
const (
	dockerEnvironmentFile = "/.dockerenv"
	kubernetesHostName    = "KUBERNETES_SERVICE_HOST"
)

// announcedInterface is one interface the responder answers on, the
// addresses it answers with, and the prefixes of those addresses.
type announcedInterface struct {
	index     int
	name      string
	addresses []netip.Addr
	prefixes  []netip.Prefix
	// loopback is an interface only this machine reaches, such as lo0 on
	// macOS, which carries a link-local address.
	loopback bool
}

// announce writes the startup lines: one naming every address that went out,
// and inside a container one more saying the address reaches nobody.
func (a *Announcer) announce(ctx context.Context, port int) {
	interfaces := announcedInterfaces()

	// host_records says who answers for the host name, so an operator reading
	// why a client finds the service and not the address knows where to look.
	records := "own"
	if a.config.SystemHost {
		records = "system"
	}

	a.logger.InfoContext(ctx,
		announcementLine(a.config.Instance, addressesOf(interfaces), a.config.Services),
		"host", hostName()+"."+domain, "port", port, "host_records", records)

	if warning := bridgeWarning(inContainer(), interfaces); warning != "" {
		a.logger.WarnContext(ctx, warning)
	}
}

// announcementLine is the first startup line. Its words are fixed and a test
// reads them, because an operator searches the log for them when a client on
// the same network finds nothing.
func announcementLine(instance string, addresses []netip.Addr, services []string) string {
	return fmt.Sprintf("announcing %s on %s as %s",
		instance, join(addresses), strings.Join(services, ", "))
}

// bridgeWarning is the second startup line, or empty when the announcement
// leaves this machine.
//
// It is written for the shipped container, where the process sits on a bridge
// network of its own. mDNS is multicast and multicast stays on one link, so a
// published TCP port forwards nothing here: the announcement reaches the other
// containers on that bridge and no machine on the network.
func bridgeWarning(containerised bool, interfaces []announcedInterface) string {
	if !containerised || len(interfaces) != 1 || !allPrivate(interfaces[0].addresses) {
		return ""
	}

	return fmt.Sprintf("%s is a bridge address that no machine on the network reaches; "+
		"set network_mode: host in compose or hostNetwork: true in Kubernetes",
		named(interfaces[0].addresses))
}

// inContainer reports whether this process runs in a container.
func inContainer() bool {
	if _, err := os.Stat(dockerEnvironmentFile); err == nil {
		return true
	}

	_, set := os.LookupEnv(kubernetesHostName)

	return set
}

// announcedInterfaces is what the responder puts on the link.
//
// The responder announces on these interfaces, so the startup line names what
// went out. A loopback address is left out:
// only this machine reaches one, so it says nothing about what the link saw.
func announcedInterfaces() []announcedInterface {
	var out []announcedInterface

	for _, iface := range multicastInterfaces() {
		addresses, err := iface.Addrs()
		if err != nil {
			continue
		}

		announced := announcedInterface{
			index: iface.Index, name: iface.Name, loopback: iface.Flags&net.FlagLoopback != 0,
		}

		for _, address := range addresses {
			prefix, parseErr := netip.ParsePrefix(address.String())
			if parseErr != nil {
				continue
			}

			bits := prefix.Bits()
			if prefix.Addr().Is4In6() {
				bits -= ipv6Bits - ipv4Bits
			}

			if addr := prefix.Addr().Unmap(); hostAddress(addr, bits) {
				announced.addresses = append(announced.addresses, addr)
				announced.prefixes = append(announced.prefixes, netip.PrefixFrom(addr, bits).Masked())
			}
		}

		if len(announced.addresses) > 0 {
			out = append(out, announced)
		}
	}

	return out
}

// hostAddress reports whether another machine can open this address. A
// loopback address reaches only this machine. An IPv4 address that is the
// network address of its own prefix names the whole network and no host:
// some virtual bridges carry one, and a link to it opens nothing.
func hostAddress(addr netip.Addr, bits int) bool {
	if addr.IsLoopback() {
		return false
	}

	if addr.Is4() && bits < ipv4Bits-1 && netip.PrefixFrom(addr, bits).Masked().Addr() == addr {
		return false
	}

	return true
}

// addressesOf is every announced address, in the order the interfaces gave
// them.
func addressesOf(interfaces []announcedInterface) []netip.Addr {
	var out []netip.Addr

	for _, one := range interfaces {
		out = append(out, one.addresses...)
	}

	return out
}

// allPrivate reports whether every address is one a local network hands out.
// A private address is from the ranges RFC 1918 and RFC 4193 reserve, and a
// link-local address never leaves the link at all. Neither of them says on its
// own whether that link is the real network or a container bridge, so the
// caller also asks whether the process is containerised.
func allPrivate(addresses []netip.Addr) bool {
	if len(addresses) == 0 {
		return false
	}

	for _, address := range addresses {
		if !address.IsPrivate() && !address.IsLinkLocalUnicast() {
			return false
		}
	}

	return true
}

// named is the address the warning shows. The first IPv4 address, because that
// is the one an operator reads in `docker inspect` and on the bridge network's
// own subnet.
func named(addresses []netip.Addr) string {
	for _, address := range addresses {
		if address.Is4() {
			return address.String()
		}
	}

	return addresses[0].String()
}

// join renders a list of addresses for one log line.
func join(addresses []netip.Addr) string {
	if len(addresses) == 0 {
		return "no address"
	}

	out := make([]string, 0, len(addresses))
	for _, address := range addresses {
		out = append(out, address.String())
	}

	return strings.Join(out, ", ")
}
