// Package datagram reads the UDP datagrams a Prusa printer sends when its
// Metrics & Log setting is on.
//
// Both streams are RFC 5424 syslog messages. The formats below are the ones
// the public Buddy firmware writes at tag v6.10.1, quoted from its source:
//
//	syslog,  src/logging/log_dest_syslog.cpp:
//	    "<%i>1 - %s %s %s - [metrics@64164 v=\"5\" msg=\"%" PRIu32 "\" tm=\"%" PRIu64 "\"] %s"
//	metrics, src/common/metric_handlers.cpp:
//	    "<%i>1 - %s %s - - - msg=%" PRIu32 ",tm=%" PRId64 ",v=4 "
//
// Firmware 6.4.0 writes a syslog line with no structured data,
// "<%i>1 - %s %s %s - - %s", cut at 128 bytes, so its lines carry no sequence
// and no time since boot. Both are read the same way.
//
// The HOSTNAME field is the MAC address of the printer's Ethernet port, in
// lower case with colons (src/common/otp.cpp, otp_get_mac_address_str). A
// datagram whose HOSTNAME is not a MAC in that form is refused. A metrics datagram carries one point per line after the header,
// in the InfluxDB line protocol, each ending in a time offset in microseconds
// from tm.
//
// A datagram this package cannot read is refused with ErrUnreadable. The
// receiver counts it and keeps it raw, so a firmware that changes the format
// is visible and never fatal.
package datagram

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

// MaxBytes is the largest datagram this package reads. The firmware cuts a
// syslog line at 200 bytes (6.10) or 128 bytes (6.4) and a metrics datagram at
// 1024 bytes, so a larger one is not from a printer.
const MaxBytes = 2 << 10

// MaxPoints bounds the points of one metrics datagram. A point the firmware
// writes takes at least eight bytes, so 1024 bytes hold fewer than this.
const MaxPoints = 128

// MaxFields bounds the fields of one point. A custom metric of the firmware
// writes a handful.
const MaxFields = 32

// MaxTextBytes bounds the text of one syslog line and the line of one point,
// after Clean. Longer text is cut.
const MaxTextBytes = 1 << 10

// MaxComponentBytes bounds a syslog line's component. The firmware's names,
// such as Connect or Dwarf_4, are a few letters.
const MaxComponentBytes = 64

// headerFields is TIMESTAMP, HOSTNAME, APP-NAME, PROCID and MSGID, the
// fields of an RFC 5424 header after the version.
const headerFields = 5

// severities is how many severities RFC 5424 has. PRI is the facility times
// this, plus the severity.
const severities = 8

// ErrUnreadable reports a datagram that is not in either format.
var ErrUnreadable = errors.New("datagram: not a printer syslog or metrics message")

// Kind is which stream a datagram belongs to.
type Kind string

// The two streams.
const (
	KindSyslog  Kind = "syslog"
	KindMetrics Kind = "metrics"
)

// Severity is the RFC 5424 severity, 0 the most severe and 7 the least.
type Severity int

// The severities the firmware sends. It maps its own levels onto these and
// never sends debug over the network (src/buddy/logging.cpp).
const (
	SeverityEmergency Severity = 0
	SeverityAlert     Severity = 1
	SeverityCritical  Severity = 2
	SeverityError     Severity = 3
	SeverityWarning   Severity = 4
	SeverityNotice    Severity = 5
	SeverityInfo      Severity = 6
	SeverityDebug     Severity = 7
)

// severityNames are the words a person reads and a query names.
//
//nolint:gochecknoglobals // a closed table, read and never written.
var severityNames = []string{"emergency", "alert", "critical", "error", "warning", "notice", "info", "debug"}

// String is the severity's word, such as "error".
func (s Severity) String() string {
	if s < 0 || int(s) >= len(severityNames) {
		return "unknown"
	}

	return severityNames[s]
}

// ParseSeverity reads a severity word back. ok is false for a word that is
// not one.
func ParseSeverity(word string) (Severity, bool) {
	for i, name := range severityNames {
		if strings.EqualFold(word, name) {
			return Severity(i), true
		}
	}

	return 0, false
}

// Datagram is one message, read.
type Datagram struct {
	Kind Kind
	// MAC is the HOSTNAME field in lower case. It names the printer.
	MAC string
	// Seq is the firmware's message counter. It goes up by one per message
	// and starts again at a reboot.
	Seq uint64
	// TmUS is microseconds since the printer started.
	TmUS int64

	// Severity, Component and Text are a syslog line's.
	Severity  Severity
	Component string
	Text      string

	// Points are a metrics datagram's.
	Points []Point
}

// Parse reads one datagram.
func Parse(raw []byte) (Datagram, error) {
	if len(raw) == 0 || len(raw) > MaxBytes {
		return Datagram{}, fmt.Errorf("%w: %d bytes", ErrUnreadable, len(raw))
	}

	text := string(raw)

	pri, rest, ok := header(text)
	if !ok {
		return Datagram{}, fmt.Errorf("%w: no <PRI>1 header", ErrUnreadable)
	}

	fields, rest, ok := words(rest, headerFields)
	if !ok {
		return Datagram{}, fmt.Errorf("%w: the header has fewer than five fields", ErrUnreadable)
	}

	// fields: TIMESTAMP, HOSTNAME, APP-NAME, PROCID, MSGID.
	held := Datagram{MAC: strings.ToLower(fields[1])}
	if !ValidMAC(held.MAC) {
		return Datagram{}, fmt.Errorf("%w: the HOSTNAME is not a MAC address", ErrUnreadable)
	}

	sd, message, ok := structured(rest)
	if !ok {
		return Datagram{}, fmt.Errorf("%w: unreadable structured data", ErrUnreadable)
	}

	if sd == "-" && strings.HasPrefix(message, "msg=") {
		return metrics(held, message)
	}

	held.Kind = KindSyslog
	held.Severity = Severity(pri % severities)
	held.Component = cut(Clean(nilDash(fields[3])), MaxComponentBytes)
	held.Text = cut(Clean(strings.TrimRight(message, "\r\n\x00")), MaxTextBytes)

	params := sdParams(sd)
	held.Seq, _ = strconv.ParseUint(params["msg"], 10, 64)
	held.TmUS, _ = strconv.ParseInt(params["tm"], 10, 64)

	return held, nil
}

// header reads "<PRI>1 " and answers the priority and what follows.
func header(text string) (int, string, bool) {
	if !strings.HasPrefix(text, "<") {
		return 0, "", false
	}

	end := strings.IndexByte(text, '>')
	if end < 2 || end > 4 {
		return 0, "", false
	}

	pri, err := strconv.Atoi(text[1:end])
	if err != nil || pri < 0 || pri > 191 {
		return 0, "", false
	}

	rest, ok := strings.CutPrefix(text[end+1:], "1 ")

	return pri, rest, ok
}

// words splits off the first n space-separated words.
func words(text string, n int) ([]string, string, bool) {
	out := make([]string, 0, n)

	for range n {
		word, rest, found := strings.Cut(text, " ")
		if !found || word == "" {
			return nil, "", false
		}

		out = append(out, word)
		text = rest
	}

	return out, text, true
}

// structured splits the STRUCTURED-DATA field from the message. It is "-" or
// one or more [...] elements, whose quoted values may hold a space or a
// bracket.
func structured(text string) (string, string, bool) {
	if strings.HasPrefix(text, "- ") || text == "-" {
		return "-", strings.TrimPrefix(strings.TrimPrefix(text, "-"), " "), true
	}

	if !strings.HasPrefix(text, "[") {
		return "", "", false
	}

	quoted, escaped := false, false

	for i, r := range text {
		switch {
		case escaped:
			escaped = false
		case r == '\\':
			escaped = true
		case r == '"':
			quoted = !quoted
		case r == ']' && !quoted && (i+1 == len(text) || text[i+1] != '['):
			return text[:i+1], strings.TrimPrefix(text[i+1:], " "), true
		}
	}

	return "", "", false
}

// sdParams reads the name="value" pairs of structured data.
func sdParams(sd string) map[string]string {
	out := map[string]string{}

	for _, pair := range splitOutsideQuotes(strings.Trim(sd, "[]"), ' ') {
		name, value, found := strings.Cut(pair, "=")
		if found {
			out[name] = strings.Trim(value, `"`)
		}
	}

	return out
}

// ValidMAC reports whether mac is six pairs of lower-case hex digits joined
// by colons, the form the firmware writes.
func ValidMAC(mac string) bool {
	const (
		length = len("aa:bb:cc:dd:ee:ff")
		// pair is two digits and the colon after them.
		pair  = len("aa:")
		colon = len("aa")
	)

	if len(mac) != length {
		return false
	}

	for i := range length {
		c := mac[i]

		switch {
		case i%pair == colon:
			if c != ':' {
				return false
			}
		case (c < '0' || c > '9') && (c < 'a' || c > 'f'):
			return false
		}
	}

	return true
}

// Clean escapes every control character, C0, DEL and C1, and every byte that
// is not UTF-8, as \xHH. A sender can then not forge a second line or an
// ANSI escape in a download, a terminal or a diagnostics file.
func Clean(text string) string {
	if !needsClean(text) {
		return text
	}

	var out strings.Builder

	for i := 0; i < len(text); {
		r, size := utf8.DecodeRuneInString(text[i:])

		switch {
		case r == utf8.RuneError && size == 1:
			fmt.Fprintf(&out, `\x%02x`, text[i])
		case control(r):
			fmt.Fprintf(&out, `\x%02x`, r)
		default:
			out.WriteString(text[i : i+size])
		}

		i += size
	}

	return out.String()
}

func needsClean(text string) bool {
	if !utf8.ValidString(text) {
		return true
	}

	return strings.ContainsFunc(text, control)
}

func control(r rune) bool {
	return r < 0x20 || (r >= 0x7f && r <= 0x9f)
}

// cut bounds a text to limit bytes without splitting a character.
func cut(text string, limit int) string {
	if len(text) <= limit {
		return text
	}

	end := limit
	for end > 0 && !utf8.RuneStart(text[end]) {
		end--
	}

	return text[:end]
}

func nilDash(word string) string {
	if word == "-" {
		return ""
	}

	return word
}
