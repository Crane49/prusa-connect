package manifest

import (
	"fmt"
	"strconv"
)

// Network is the transport a listen entry opens its port on.
type Network string

// The networks a module may listen on.
const (
	NetworkUDP Network = "udp"
	NetworkTCP Network = "tcp"
)

// The memory budget a module may recommend, in MiB.
const (
	MinMemoryMiB = 16
	MaxMemoryMiB = 8192
)

// MaxPort is the highest port a listen entry may name.
const MaxPort = 65535

// maxListenName bounds a listen entry's name. It is a key in the declaration
// and a word in the install output, never a sentence.
const maxListenName = 32

// Needs is what a module asks of the machine it runs on, beyond the API. It
// is a request like Permissions: the operator approves it at install, and a
// port takes an approval of its own.
type Needs struct {
	// Listen is the ports the module listens on for a protocol of its own.
	Listen []Listen
	// Data asks for one writable directory the host chooses.
	Data bool
	// Database asks for a database of the module's own. The operator chooses
	// the engine, so a module that asks ships its migrations for SQLite and
	// for Postgres.
	Database bool
	// MemoryMiB is the memory budget the module recommends for its process,
	// from MinMemoryMiB to MaxMemoryMiB. Zero means no recommendation, and
	// the server applies its default.
	MemoryMiB int
}

// Listen is one port a module listens on.
type Listen struct {
	// Name is how the declaration and the grant name the port, so the
	// operator can move it and the module still finds it.
	Name string
	// Network is the transport, udp or tcp.
	Network Network
	// Port is the port the module asks for. The operator may grant another.
	Port int
	// Why is the sentence the operator reads before approving the port.
	Why string
}

// Request is the install output's line for the port, such as
// "listen udp 13514 as syslog (printer log lines)". It names the port's name,
// so a page that read the line can send the request back as expect.
func (l Listen) Request() string {
	return fmt.Sprintf("listen %s %d as %s (%s)", l.Network, l.Port, l.Name, l.Why)
}

// needsKeys is every key the needs block may have.
//
//nolint:gochecknoglobals // a closed table read at parse, never written.
var needsKeys = []string{"listen", "data", "database", "memory_mib"}

// listenKeys is every key one listen entry may have.
//
//nolint:gochecknoglobals // a closed table read at parse, never written.
var listenKeys = []string{"name", "udp", "tcp", "why"}

// readNeeds reads the needs block. An absent block asks for nothing.
func readNeeds(tree map[string]any) (Needs, error) {
	block, present, err := node(tree, "", "needs")
	if err != nil || !present {
		return Needs{}, err
	}

	if err = refuseUnknown(block, "needs", needsKeys); err != nil {
		return Needs{}, err
	}

	var held Needs

	if held.Data, err = truth(block, "needs", "data"); err != nil {
		return Needs{}, err
	}

	if held.Database, err = truth(block, "needs", "database"); err != nil {
		return Needs{}, err
	}

	if held.MemoryMiB, err = whole(block, "needs", "memory_mib"); err != nil {
		return Needs{}, err
	}

	if held.MemoryMiB != 0 && (held.MemoryMiB < MinMemoryMiB || held.MemoryMiB > MaxMemoryMiB) {
		return Needs{}, fmt.Errorf("%w: needs.memory_mib is %d, expected %d to %d, or no key for the server default",
			ErrInvalid, held.MemoryMiB, MinMemoryMiB, MaxMemoryMiB)
	}

	entries, err := nodeList(block, "needs", "listen")
	if err != nil {
		return Needs{}, err
	}

	names := map[string]bool{}
	ports := map[string]bool{}

	for index, entry := range entries {
		listen, readErr := readListen(fmt.Sprintf("needs.listen[%d]", index), entry)
		if readErr != nil {
			return Needs{}, readErr
		}

		if names[listen.Name] {
			return Needs{}, fmt.Errorf("%w: needs.listen names %q twice", ErrInvalid, listen.Name)
		}

		address := string(listen.Network) + "/" + strconv.Itoa(listen.Port)
		if ports[address] {
			return Needs{}, fmt.Errorf("%w: needs.listen asks for %s twice", ErrInvalid, address)
		}

		names[listen.Name] = true
		ports[address] = true
		held.Listen = append(held.Listen, listen)
	}

	return held, nil
}

// readListen reads one listen entry. It must name one network.
func readListen(where string, entry map[string]any) (Listen, error) {
	if err := refuseUnknown(entry, where, listenKeys); err != nil {
		return Listen{}, err
	}

	name, err := requiredText(entry, where, "name")
	if err != nil {
		return Listen{}, err
	}

	if !IsListenName(name) {
		return Listen{}, fmt.Errorf("%w: %s.name is %q, expected lower case letters, digits and hyphens, "+
			"starting with a letter, at most %d characters", ErrInvalid, where, name, maxListenName)
	}

	why, err := requiredText(entry, where, "why")
	if err != nil {
		return Listen{}, err
	}

	held := Listen{Name: name, Why: why}

	for _, network := range []Network{NetworkUDP, NetworkTCP} {
		if _, present := entry[string(network)]; !present {
			continue
		}

		if held.Network != "" {
			return Listen{}, fmt.Errorf("%w: %s names both udp and tcp, expected one", ErrInvalid, where)
		}

		held.Network = network

		if held.Port, err = whole(entry, where, string(network)); err != nil {
			return Listen{}, err
		}
	}

	if held.Network == "" {
		return Listen{}, fmt.Errorf("%w: %s names no network, expected udp or tcp with a port", ErrInvalid, where)
	}

	if held.Port < 1 || held.Port > MaxPort {
		return Listen{}, fmt.Errorf("%w: %s.%s is %d, expected 1 to %d",
			ErrInvalid, where, held.Network, held.Port, MaxPort)
	}

	return held, nil
}

// IsListenName reports whether name may name a listen entry: lower case
// letters, digits and hyphens, starting with a letter, at most 32 characters.
func IsListenName(name string) bool {
	if name == "" || len(name) > maxListenName || name[0] < 'a' || name[0] > 'z' {
		return false
	}

	for _, r := range name {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
			return false
		}
	}

	return true
}
