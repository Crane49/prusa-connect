// Package diagnostics builds the bundle an operator attaches to a support
// ticket, and owns the policy that keeps secrets and personal data out of it.
package diagnostics

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/url"
	"path"
	"regexp"
	"strings"

	"github.com/prusa3d/prusa-connect/internal/kernel/machinetext"
)

// minSweptSecret is the shortest secret the sweep removes. A shorter value
// matches ordinary text too often to be cut out of every file.
const minSweptSecret = 6

// frontend/src/lib/log/index.ts applies the same text patterns to the
// browser's buffer, which "Copy browser details" sends without this server.
var (
	urlPattern    = regexp.MustCompile(`\b[a-zA-Z][a-zA-Z0-9+.-]*://[^\s"'<>]+`)
	jwtPattern    = regexp.MustCompile(`\beyJ[A-Za-z0-9_-]*\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]*`)
	bearerPattern = regexp.MustCompile(`(?i)\bbearer\s+[A-Za-z0-9._~+/=-]+`)
	authPattern   = regexp.MustCompile(`(?i)(\bauthorization"?\s*:\s*["\[]{0,2})(?:basic|token|digest)\s+[^\r\n"\]]+`)
	cookiePattern = regexp.MustCompile(`(?i)\b((?:set-)?cookie"?\s*:\s*["\[]{0,2})[^\r\n"\]]+`)
	pairPattern   = regexp.MustCompile(
		`(?i)([A-Za-z0-9_-]*(?:password|passwd|pwd|token|secret|sslpassword|api[_-]?key)"?)` +
			`(\s*[=:]\s*)("[^"]*"|'[^']*'|[^\s&,;]+)`,
	)
	emailPattern = regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}`)
	// userPattern is the quoted account name a database or broker error
	// repeats, as in: password authentication failed for user "lukas".
	userPattern = regexp.MustCompile(`(?i)\b((?:user|role|username|login)(?:\s+|\s*[=:]\s*))("[^"]*"|'[^']*')`)
	macPattern  = regexp.MustCompile(`\b[0-9A-Fa-f]{2}(?:[:-][0-9A-Fa-f]{2}){5}\b`)
	// lookupPattern is the host a resolver error names without a scheme or a
	// port: lookup mqtt.home.lan on 192.168.1.1:53.
	lookupPattern = regexp.MustCompile(`\blookup\s+([A-Za-z0-9-]+(?:\.[A-Za-z0-9-]+)*)`)
	// hostPattern is a dotted name with a port, or a name under a domain that
	// only a local network serves.
	hostPattern = regexp.MustCompile(
		`\b[A-Za-z0-9-]+(?:\.[A-Za-z0-9-]+)+:\d{1,5}\b|` +
			`\b[A-Za-z0-9-]+(?:\.[A-Za-z0-9-]+)*\.(?:local|lan|home|internal|localdomain|home\.arpa)\b`,
	)
	ipv4Pattern = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}(?::\d{1,5})?\b`)
	ipv6Pattern = regexp.MustCompile(`\[?[0-9A-Fa-f]{0,4}(?::[0-9A-Fa-f]{0,4}){2,7}\]?(?::\d{1,5})?`)
	// homePattern takes a home directory as one segment, because a user name may
	// hold a space that pathPattern would stop at.
	homePattern = regexp.MustCompile(
		`/(?:Users|home)/[^/\n"':]+(?:/[^\s"':/\\]+)+|[A-Za-z]:\\Users\\[^\\\n"':]+(?:\\[^\s"':/\\]+)+`,
	)
	// pathPattern also takes a module path such as github.com/org/repo, which
	// names code rather than a file of the operator's.
	pathPattern = regexp.MustCompile(
		`(?:[A-Za-z0-9-]+(?:\.[A-Za-z0-9-]+)*\.(?:com|org|net|io|in|dev))?(?:[A-Za-z]:\\|/)(?:[^\s"':/\\]+[/\\])+[^\s"':/\\]+`,
	)
	// goHostPattern is the host of a Go module path. Any other dotted prefix is
	// an address of the operator's, such as a webhook without its scheme.
	goHostPattern = regexp.MustCompile(
		`^(?:github\.com|golang\.org|google\.golang\.org|gopkg\.in|go\.uber\.org|go\.yaml\.in)/`,
	)
	longPattern = regexp.MustCompile(`(?:revision=)?\b[A-Za-z0-9+_-]{32,}={0,2}`)

	uuidPattern  = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	hexPattern   = regexp.MustCompile(`^[0-9a-fA-F]+$`)
	digitPattern = regexp.MustCompile(`[0-9]`)
	alphaPattern = regexp.MustCompile(`[A-Za-z]`)
)

const (
	traceIDLen  = 32
	revisionLen = 40
)

// Scrubber is one bundle's redaction: its hash salt and the live secrets it
// sweeps out.
type Scrubber struct {
	salt    []byte
	secrets [][]byte
}

// NewScrubber takes a salt that is random per bundle, so two bundles cannot be
// linked by a hash, and the secret values this install holds.
func NewScrubber(salt []byte, secrets []string) *Scrubber {
	s := &Scrubber{salt: salt}

	for _, secret := range secrets {
		if len(secret) < minSweptSecret {
			continue
		}

		s.secrets = append(s.secrets, []byte(secret))

		s.secrets = append(s.secrets, jsonEscapings(secret)...)
	}

	return s
}

// Hash names a value without revealing it. Within one bundle the same value
// gets the same name.
func (s *Scrubber) Hash(v string) string {
	if v == "" {
		return ""
	}

	mac := hmac.New(sha256.New, s.salt)
	mac.Write([]byte(v))

	return "h:" + hex.EncodeToString(mac.Sum(nil))[:12]
}

// Text replaces every recognisable secret, address and path in free text.
// The order matters: a known secret goes first, before a pattern could cut it
// short and leave its tail, and a URL is reduced before its parts could match
// anything narrower.
func (s *Scrubber) Text(v string) string {
	v = s.sweepText(v)
	v = urlPattern.ReplaceAllStringFunc(v, s.url)
	v = jwtPattern.ReplaceAllString(v, "<jwt>")
	v = bearerPattern.ReplaceAllString(v, "<bearer>")
	v = authPattern.ReplaceAllString(v, "${1}<credentials>")
	v = cookiePattern.ReplaceAllString(v, "${1}<redacted>")
	v = pairPattern.ReplaceAllString(v, "$1$2<redacted>")
	v = emailPattern.ReplaceAllString(v, "<email>")
	v = userPattern.ReplaceAllStringFunc(v, s.user)
	v = macPattern.ReplaceAllString(v, "<mac>")
	v = lookupPattern.ReplaceAllStringFunc(v, s.lookup)
	v = ipv4Pattern.ReplaceAllString(v, "<ip>")
	v = ipv6Pattern.ReplaceAllStringFunc(v, ipv6)
	v = hostPattern.ReplaceAllStringFunc(v, s.host)
	v = homePattern.ReplaceAllStringFunc(v, s.path)
	v = pathPattern.ReplaceAllStringFunc(v, s.path)
	v = longPattern.ReplaceAllStringFunc(v, s.long)

	return v
}

// Sweep removes every known secret from a rendered file, as written and as
// JSON escapes it.
func (s *Scrubber) Sweep(b []byte) []byte {
	for _, secret := range s.secrets {
		b = bytes.ReplaceAll(b, secret, []byte("<secret>"))
	}

	return b
}

func (s *Scrubber) sweepText(v string) string {
	for _, secret := range s.secrets {
		v = strings.ReplaceAll(v, string(secret), "<secret>")
	}

	return v
}

// clip cuts v to maxLineBytes after the sweep, so the cut cannot leave the
// head of a known secret behind.
func (s *Scrubber) clip(v string) string {
	return machinetext.Clip(s.sweepText(v), maxLineBytes)
}

func (s *Scrubber) url(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return "<url>"
	}

	return parsed.Scheme + "://<" + s.Hash(parsed.Hostname()) + ">"
}

func (s *Scrubber) user(m string) string {
	parts := userPattern.FindStringSubmatch(m)

	return parts[1] + "<" + s.Hash(strings.Trim(parts[2], `"'`)) + ">"
}

func (s *Scrubber) lookup(m string) string {
	return "lookup <" + s.Hash(lookupPattern.FindStringSubmatch(m)[1]) + ">"
}

// host keeps a Go source position such as main.go:42, which a stack frame
// needs, and keeps the port of an address, which names nothing of the
// operator's.
func (s *Scrubber) host(m string) string {
	name, port, hasPort := strings.Cut(m, ":")
	if !hasPort {
		return "<" + s.Hash(m) + ">"
	}

	if strings.HasSuffix(name, ".go") {
		return m
	}

	return "<" + s.Hash(name) + ">:" + port
}

// path keeps API routes, which name no file, and the base name of a Go source
// file, which a stack frame needs and which names nothing of the operator's.
func (s *Scrubber) path(p string) string {
	if strings.HasPrefix(p, "/api/") || isCode(p) {
		return p
	}

	if base := path.Base(strings.ReplaceAll(p, `\`, "/")); strings.HasSuffix(base, ".go") {
		return "…/" + base
	}

	return "<path:" + s.Hash(p) + ">"
}

// ipv6 replaces a candidate only when it parses, because the pattern also
// matches a clock time such as 12:34:56.
func ipv6(candidate string) string {
	host := strings.TrimPrefix(candidate, "[")
	if i := strings.Index(host, "]"); i >= 0 {
		host = host[:i]
	}

	if net.ParseIP(host) == nil {
		return candidate
	}

	return "<ip>"
}

// isCode reports a module path or a function frame, neither of which names
// anything of the operator's.
func isCode(p string) bool {
	return goHostPattern.MatchString(p) || strings.Contains(p, ".(") || strings.Contains(p, ").")
}

// long redacts a long opaque token but keeps the identifiers a bug report is
// correlated by: a UUID, a trace id (hashed) and a commit revision.
func (s *Scrubber) long(m string) string {
	token, isRevision := strings.CutPrefix(m, "revision=")

	switch {
	case uuidPattern.MatchString(token):
		return m
	case isRevision && len(token) == revisionLen && hexPattern.MatchString(token):
		return m
	case len(token) == traceIDLen && hexPattern.MatchString(token):
		return s.Hash(token)
	case !digitPattern.MatchString(token) || !alphaPattern.MatchString(token):
		return m
	}

	return "<redacted>"
}

// jsonEscapings returns a secret as a JSON string holds it, with and without
// the HTML escaping (& < >) that encoding/json applies by default.
func jsonEscapings(secret string) [][]byte {
	var out [][]byte

	if b, err := json.Marshal(secret); err == nil {
		out = append(out, b[1:len(b)-1])
	}

	var buf bytes.Buffer

	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)

	if err := enc.Encode(secret); err == nil {
		b := bytes.TrimSuffix(buf.Bytes(), []byte("\n"))
		out = append(out, bytes.Clone(b[1:len(b)-1]))
	}

	return out
}
