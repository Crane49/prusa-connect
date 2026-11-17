package discovery

import (
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/miekg/dns"
)

// maxLabel is the longest DNS label, and maxName the longest DNS name, in
// bytes.
const (
	maxLabel = 63
	maxName  = 255
)

// canonical is a name in the form RFC 6762 section 16 compares names in: the
// bytes it has on the wire, with ASCII letters in lower case. Names are
// compared in this form only. A name arrives written with the escapes of the
// DNS library, such as a backslash before a space, and a name this server
// builds is written with its own, so the two differ as text even when they
// are the same name. A name that does not fit on the wire is compared as
// lower-case text.
func canonical(name string) string {
	buf := make([]byte, maxName)

	end, err := dns.PackDomainName(dns.Fqdn(name), buf, 0, nil, false)
	if err != nil {
		return strings.ToLower(name)
	}

	wire := buf[:end]

	for i, b := range wire {
		if 'A' <= b && b <= 'Z' {
			wire[i] = b + 'a' - 'A'
		}
	}

	return string(wire)
}

// sameName reports whether two names are one name.
func sameName(a, b string) bool {
	return canonical(a) == canonical(b)
}

// escapeLabel writes a label the way a DNS name carries it, so a dot in a
// title stays in the label.
func escapeLabel(label string) string {
	return strings.NewReplacer(`\`, `\\`, `.`, `\.`).Replace(label)
}

// renamed is a name after a number of renames, cut to fit in one label. The
// first rename adds the suffix with 2, the next with 3, and so on.
func renamed(base string, tries int, suffix func(n string) string) string {
	if tries > 0 {
		return cutLabel(base, suffix(strconv.Itoa(tries+1)))
	}

	return cutLabel(base, "")
}

// hostSuffix and instanceSuffix are the forms a rename takes: "desk-2" for a
// host, as RFC 6762 section 9 shows, and "Desk (2)" for an instance.
func hostSuffix(n string) string     { return "-" + n }
func instanceSuffix(n string) string { return " (" + n + ")" }

// cutLabel is a name and a suffix, with the name cut short so both fit in one
// label. It cuts between characters, never inside one.
func cutLabel(base, suffix string) string {
	for len(base)+len(suffix) > maxLabel {
		_, size := utf8.DecodeLastRuneInString(base)
		base = base[:len(base)-size]
	}

	return base + suffix
}

// instanceName is the first label of an instance's full name, as a person
// reads it.
func instanceName(instance, service string) string {
	suffix := "." + service

	if len(instance) > len(suffix) && strings.EqualFold(instance[len(instance)-len(suffix):], suffix) {
		instance = instance[:len(instance)-len(suffix)]
	}

	return unescapeLabel(instance)
}

// unescapeLabel reads a DNS label the way RFC 1035 section 5.1 writes one. A
// backslash and a character stand for the character, and a backslash and three
// digits stand for the byte with that decimal value.
func unescapeLabel(label string) string {
	if !strings.Contains(label, `\`) {
		return label
	}

	out := make([]byte, 0, len(label))

	for i := 0; i < len(label); i++ {
		if label[i] != '\\' || i+1 == len(label) {
			out = append(out, label[i])

			continue
		}

		if i+3 < len(label) {
			if value, err := strconv.ParseUint(label[i+1:i+4], 10, 8); err == nil {
				out = append(out, byte(value))
				i += 3

				continue
			}
		}

		out = append(out, label[i+1])
		i++
	}

	return string(out)
}
