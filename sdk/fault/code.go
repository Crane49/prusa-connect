package fault

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Errors reported when a domain or a code is built. Callers inspect them with
// errors.Is.
var (
	// ErrInvalidDomain reports a domain that is not a dotted lowercase name.
	ErrInvalidDomain = errors.New("fault: invalid domain")
	// ErrReservedDomain reports a domain inside the core namespace.
	ErrReservedDomain = errors.New("fault: reserved domain")
	// ErrInvalidReason reports a reason outside [A-Z][A-Z0-9_]*[A-Z0-9].
	ErrInvalidReason = errors.New("fault: invalid reason")
	// ErrInvalidCode reports text that is not a domain/REASON pair.
	ErrInvalidCode = errors.New("fault: invalid code")
)

const (
	coreNamespace = "core"

	// TypeBaseURI is the published error reference. The RFC 9457 type URI of a
	// code is this page plus the code's anchor.
	TypeBaseURI = "https://beta.prusa3d.com/connect-local/docs/error-codes/"

	codeSeparator = "/"

	// anchorSeparator joins the page to a section of it.
	anchorSeparator = "#"

	maxDomainLen = 253
	maxLabelLen  = 63
	minLabels    = 2
	maxReasonLen = 63 // AIP-193
	minReasonLen = 2
)

// Domain is the namespace half of a Code. NewDomain refuses the core namespace,
// so an extension's codes cannot collide with the core's. It is not a security
// boundary: code compiled into the binary can call the core accessors.
type Domain struct {
	name string
}

// NewDomain returns the domain token a host hands to an extension. The name is
// a dotted lowercase name its author owns, reverse-DNS by convention
// (io.github.acme.s3).
func NewDomain(name string) (Domain, error) {
	if name == coreNamespace || strings.HasPrefix(name, coreNamespace+".") {
		return Domain{}, fmt.Errorf("%w: %q", ErrReservedDomain, name)
	}

	if err := validateDomainName(name); err != nil {
		return Domain{}, err
	}

	return Domain{name: name}, nil
}

// DomainPrinter is the core domain for printer connectivity and commands.
func DomainPrinter() Domain { return Domain{name: "core.printer"} }

// DomainJob is the core domain for print jobs and the queue.
func DomainJob() Domain { return Domain{name: "core.job"} }

// DomainAuth is the core domain for authentication and authorization.
func DomainAuth() Domain { return Domain{name: "core.auth"} }

// DomainStorage is the core domain for file storage and transfers.
func DomainStorage() Domain { return Domain{name: "core.storage"} }

// DomainValidation is the core domain for a malformed request. The caller has
// to change what it sent.
func DomainValidation() Domain { return Domain{name: "core.validation"} }

// DomainResource is the core domain for a named resource instance: one that is
// missing, one whose version changed, one the API does not write. The request
// itself is well formed.
func DomainResource() Domain { return Domain{name: "core.resource"} }

// DomainInternal is the core domain for failures a client can do nothing about.
func DomainInternal() Domain { return Domain{name: "core.internal"} }

// String returns the domain name, empty for the zero value.
func (d Domain) String() string { return d.name }

// IsZero reports whether the domain names nothing.
func (d Domain) IsZero() bool { return d.name == "" }

// Owns reports whether c belongs to this domain.
func (d Domain) Owns(c Code) bool { return !d.IsZero() && c.domain == d.name }

// Code identifies a failure as domain/REASON. A caller branches on it, and it
// is the key of a client's message bundle. The zero value names nothing and
// refuses to marshal.
type Code struct {
	domain string
	reason string
}

// NewCode is the only exported way to build a code. The reason follows
// AIP-193: [A-Z][A-Z0-9_]*[A-Z0-9], at most 63 characters.
func NewCode(domain Domain, reason string) (Code, error) {
	if domain.IsZero() {
		return Code{}, fmt.Errorf("%w: empty", ErrInvalidDomain)
	}

	if err := validateReason(reason); err != nil {
		return Code{}, err
	}

	return Code{domain: domain.name, reason: reason}, nil
}

// String returns domain/REASON, empty for the zero value.
func (c Code) String() string {
	if c.IsZero() {
		return ""
	}

	return c.domain + codeSeparator + c.reason
}

// IsZero reports whether the code names nothing.
func (c Code) IsZero() bool { return c.domain == "" || c.reason == "" }

// DomainName returns the namespace half.
func (c Code) DomainName() string { return c.domain }

// Reason returns the reason half.
func (c Code) Reason() string { return c.reason }

// TypeURI returns the RFC 9457 type URI, empty for the zero value. It is the
// error reference page plus this code's anchor. A client never has to fetch
// it.
func (c Code) TypeURI() string {
	if c.IsZero() {
		return ""
	}

	return TypeBaseURI + anchorSeparator + c.Anchor()
}

// Anchor returns the section of the error reference this code is published
// under: lowercase, and every run of anything else one hyphen.
// core.printer/GCODE_NOT_ALLOWED is core-printer-gcode-not-allowed.
func (c Code) Anchor() string {
	if c.IsZero() {
		return ""
	}

	var out strings.Builder

	identifier := c.String()
	out.Grow(len(identifier))

	pendingHyphen := false

	for _, char := range strings.ToLower(identifier) {
		switch {
		case (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9'):
			if pendingHyphen && out.Len() > 0 {
				out.WriteByte('-')
			}

			pendingHyphen = false

			out.WriteRune(char)
		default:
			pendingHyphen = true
		}
	}

	return out.String()
}

// MarshalJSON encodes the code as domain/REASON, and fails for the zero value.
func (c Code) MarshalJSON() ([]byte, error) {
	if c.IsZero() {
		return nil, fmt.Errorf("%w: zero value", ErrInvalidCode)
	}

	encoded, err := json.Marshal(c.String())
	if err != nil {
		return nil, fmt.Errorf("fault: encode code: %w", err)
	}

	return encoded, nil
}

// UnmarshalJSON reads domain/REASON back, applying the grammar NewCode
// applies.
func (c *Code) UnmarshalJSON(data []byte) error {
	var text string

	if err := json.Unmarshal(data, &text); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidCode, err)
	}

	parsed, err := parseCode(text)
	if err != nil {
		return err
	}

	*c = parsed

	return nil
}

// parseCode applies the grammar NewCode applies to domain/REASON text.
func parseCode(text string) (Code, error) {
	domain, reason, found := strings.Cut(text, codeSeparator)
	if !found {
		return Code{}, fmt.Errorf("%w: %q", ErrInvalidCode, text)
	}

	if err := validateDomainName(domain); err != nil {
		return Code{}, err
	}

	if err := validateReason(reason); err != nil {
		return Code{}, err
	}

	return Code{domain: domain, reason: reason}, nil
}

func validateDomainName(name string) error {
	if name == "" || len(name) > maxDomainLen {
		return fmt.Errorf("%w: %q", ErrInvalidDomain, name)
	}

	labels := strings.Split(name, ".")
	if len(labels) < minLabels {
		return fmt.Errorf("%w: %q needs at least %d labels", ErrInvalidDomain, name, minLabels)
	}

	for _, label := range labels {
		if !validLabel(label) {
			return fmt.Errorf("%w: %q", ErrInvalidDomain, name)
		}
	}

	return nil
}

// validLabel applies the DNS-1123 label rules: a lowercase letter or a digit at
// each end, '-' between, 63 bytes at most. A module namespace passes it.
func validLabel(label string) bool {
	if label == "" || len(label) > maxLabelLen {
		return false
	}

	for i := range len(label) {
		c := label[i]

		switch {
		case isLower(c), isDigit(c):
		case c == '-' && i > 0 && i < len(label)-1:
		default:
			return false
		}
	}

	return true
}

func validateReason(reason string) error {
	if len(reason) < minReasonLen || len(reason) > maxReasonLen {
		return fmt.Errorf("%w: %q", ErrInvalidReason, reason)
	}

	last := reason[len(reason)-1]
	if !isUpper(reason[0]) || (!isUpper(last) && !isDigit(last)) {
		return fmt.Errorf("%w: %q", ErrInvalidReason, reason)
	}

	for i := range len(reason) {
		c := reason[i]
		if !isUpper(c) && !isDigit(c) && c != '_' {
			return fmt.Errorf("%w: %q", ErrInvalidReason, reason)
		}
	}

	return nil
}

func isLower(c byte) bool { return c >= 'a' && c <= 'z' }

func isUpper(c byte) bool { return c >= 'A' && c <= 'Z' }

func isDigit(c byte) bool { return c >= '0' && c <= '9' }
