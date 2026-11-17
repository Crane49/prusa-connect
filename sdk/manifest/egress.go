package manifest

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/prusa3d/prusa-connect/sdk/extension"
)

// EgressFrom is where an egress connection starts.
type EgressFrom string

// The places a module connects out from.
const (
	// EgressFromProcess is the module's own process on the server.
	EgressFromProcess EgressFrom = "process"
	// EgressFromUI is the module's page in the operator's browser.
	EgressFromUI EgressFrom = "ui"
)

// The longest texts an egress entry may carry, in characters.
const (
	MaxEgressWhy   = 200
	MaxEgressSends = 300
)

// defaultEgressPort is the port an egress entry with no ports names.
const defaultEgressPort = 443

// Egress is one connection a module makes outside Prusa Connect. The operator
// reads it before approving the install. A module that declares none
// connects nowhere outside.
type Egress struct {
	// Host is the host it connects to: an exact name, *.domain for any name
	// one level below the domain, or an exact IP address. It is empty when
	// Option names the host.
	Host string
	// Option is the key of the module's option whose value the operator sets
	// to the host. It is empty when Host names the host.
	Option string
	// Ports is the ports it connects to, 443 when the entry names none.
	Ports []int
	// From is where the connection starts: the process or the page.
	From EgressFrom
	// Why says why the module connects there.
	Why string
	// Sends says what leaves the server or the browser on that connection.
	Sends string
}

// Target is the host or, for an option, the option it reads the host from,
// as the install output prints it.
func (e Egress) Target() string {
	if e.Option != "" {
		return "the host in the setting " + e.Option
	}

	return e.Host
}

// Line is the install output's line for the entry.
func (e Egress) Line() string {
	ports := make([]string, 0, len(e.Ports))
	for _, port := range e.Ports {
		ports = append(ports, strconv.Itoa(port))
	}

	return fmt.Sprintf("%s port %s from the %s: %s. Sends: %s",
		e.Target(), strings.Join(ports, ","), e.From, strings.TrimSuffix(e.Why, "."), e.Sends)
}

// egressKeys is every key one egress entry may have.
//
//nolint:gochecknoglobals // a closed table read at parse, never written.
var egressKeys = []string{"host", "option", "ports", "from", "why", "sends"}

// readEgress reads the egress list. An absent list connects nowhere. An
// option entry must name a text option the manifest declares, so it reads
// the options first.
func readEgress(tree map[string]any, options []extension.Option) ([]Egress, error) {
	entries, err := nodeList(tree, "", "egress")
	if err != nil {
		return nil, err
	}

	var held []Egress

	for index, entry := range entries {
		one, readErr := readEgressEntry(fmt.Sprintf("egress[%d]", index), entry, options)
		if readErr != nil {
			return nil, readErr
		}

		held = append(held, one)
	}

	return held, nil
}

func readEgressEntry(where string, entry map[string]any, options []extension.Option) (Egress, error) {
	if err := refuseUnknown(entry, where, egressKeys); err != nil {
		return Egress{}, err
	}

	var (
		held Egress
		err  error
	)

	if held.Host, err = text(entry, where, "host"); err != nil {
		return Egress{}, err
	}

	if held.Option, err = text(entry, where, "option"); err != nil {
		return Egress{}, err
	}

	if (held.Host == "") == (held.Option == "") {
		return Egress{}, fmt.Errorf("%w: %s names %s, expected exactly one of host and option",
			ErrInvalid, where, egressNames(held))
	}

	if err = checkEgressTarget(where, held, options); err != nil {
		return Egress{}, err
	}

	if held.Ports, err = egressPorts(entry, where); err != nil {
		return Egress{}, err
	}

	if held.From, err = egressFrom(entry, where); err != nil {
		return Egress{}, err
	}

	if held.Why, err = boundedText(entry, where, "why", MaxEgressWhy); err != nil {
		return Egress{}, err
	}

	if held.Sends, err = boundedText(entry, where, "sends", MaxEgressSends); err != nil {
		return Egress{}, err
	}

	return held, nil
}

func egressNames(held Egress) string {
	if held.Host == "" {
		return "neither"
	}

	return "both"
}

// checkEgressTarget holds the host to a name, a one-level wildcard or an IP
// address, and the option to a text option the manifest declares.
func checkEgressTarget(where string, held Egress, options []extension.Option) error {
	if held.Option != "" {
		for _, option := range options {
			if option.Key != held.Option {
				continue
			}

			if option.Type != extension.OptionString && option.Type != extension.OptionURL {
				return fmt.Errorf("%w: %s.option names %q, which is a %s option, expected string or url",
					ErrInvalid, where, held.Option, option.Type)
			}

			return nil
		}

		return fmt.Errorf("%w: %s.option names %q, which options does not declare",
			ErrInvalid, where, held.Option)
	}

	if net.ParseIP(held.Host) != nil {
		return nil
	}

	name := strings.TrimPrefix(held.Host, "*.")
	if strings.Contains(name, "*") || !isHostName(name) || (name != held.Host && !strings.Contains(name, ".")) {
		return fmt.Errorf("%w: %s.host is %q, expected a host name, *.domain or an IP address",
			ErrInvalid, where, held.Host)
	}

	return nil
}

// isHostName reports whether name is a DNS name: labels of letters, digits
// and hyphens, separated by dots.
func isHostName(name string) bool {
	if name == "" || len(name) > 253 {
		return false
	}

	for label := range strings.SplitSeq(name, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}

		for _, r := range label {
			if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '-' {
				return false
			}
		}
	}

	return true
}

func egressPorts(entry map[string]any, where string) ([]int, error) {
	raw, present := entry["ports"]
	if !present || raw == nil {
		return []int{defaultEgressPort}, nil
	}

	list, isList := raw.([]any)
	if !isList || len(list) == 0 {
		return nil, fmt.Errorf("%w: %s.ports is %s, expected a list of ports", ErrInvalid, where, shapeOf(raw))
	}

	ports := make([]int, 0, len(list))

	for index, item := range list {
		port, err := whole(map[string]any{"port": item}, fmt.Sprintf("%s.ports[%d]", where, index), "port")
		if err != nil {
			return nil, err
		}

		if port < 1 || port > MaxPort {
			return nil, fmt.Errorf("%w: %s.ports[%d] is %d, expected 1 to %d", ErrInvalid, where, index, port, MaxPort)
		}

		ports = append(ports, port)
	}

	return ports, nil
}

func egressFrom(entry map[string]any, where string) (EgressFrom, error) {
	from, err := text(entry, where, "from")
	if err != nil {
		return "", err
	}

	switch EgressFrom(from) {
	case "":
		return EgressFromProcess, nil
	case EgressFromProcess, EgressFromUI:
		return EgressFrom(from), nil
	default:
		return "", fmt.Errorf("%w: %s.from is %q, expected process or ui", ErrInvalid, where, from)
	}
}

// boundedText reads a required text of at most limit characters.
func boundedText(entry map[string]any, where, key string, limit int) (string, error) {
	value, err := requiredText(entry, where, key)
	if err != nil {
		return "", err
	}

	if err = plainText(where, key, value); err != nil {
		return "", err
	}

	if n := utf8.RuneCountInString(value); n > limit {
		return "", fmt.Errorf(
			"%w: %s is %d characters, and it may have at most %d",
			ErrInvalid,
			at(where, key),
			n,
			limit,
		)
	}

	return value, nil
}
