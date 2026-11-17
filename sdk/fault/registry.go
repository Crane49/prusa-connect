package fault

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/prusa3d/prusa-connect/sdk/vocabulary"
)

// Errors reported by TryRegister.
var (
	// ErrDuplicateCode reports a code registered twice.
	ErrDuplicateCode = errors.New("fault: code already registered")
	// ErrUndocumented reports a registration missing part of its documentation.
	ErrUndocumented = errors.New("fault: undocumented code")
	// ErrInvalidStatus reports a status outside the HTTP error range.
	ErrInvalidStatus = errors.New("fault: invalid status")
	// ErrInvalidParamName reports a param name outside lowercase snake_case.
	ErrInvalidParamName = errors.New("fault: invalid param name")
)

// NoAction is the action text for a code the caller cannot act on.
const NoAction = "Nothing the caller can do. Quote the trace id when reporting it."

const (
	statusInternal = 500

	minStatus = 400
	maxStatus = 599

	maxParamNameLen = 64 // AIP-193
)

// globalRegistry is the process-wide registry. Registration is init-time and
// the map is replaced atomically, so building a fault takes no lock.
var globalRegistry = &Registry{}

// Retry says whether repeating the operation can succeed. The HTTP status
// does not decide it.
type Retry uint8

const (
	// RetryNever means the same call fails the same way. It is the zero value.
	RetryNever Retry = iota
	// RetryAllowed means the caller may repeat the call with backoff.
	RetryAllowed
)

// Redaction says whether the occurrence detail may leave the process.
type Redaction uint8

const (
	// RedactDetail keeps the message and the params in the log line only, and
	// renders code, title, status and trace id. It is the zero value.
	RedactDetail Redaction = iota
	// ShowDetail sends the message and the params to the client.
	ShowDetail
)

// Valid reports whether the hint is one of the defined values.
func (r Retry) Valid() bool { return r <= RetryAllowed }

// MarshalJSON encodes the hint as the payload's retryable boolean.
func (r Retry) MarshalJSON() ([]byte, error) {
	if r == RetryAllowed {
		return []byte("true"), nil
	}

	return []byte("false"), nil
}

// Valid reports whether the setting is one of the defined values.
func (d Redaction) Valid() bool { return d <= ShowDetail }

// MarshalJSON encodes the setting as a redacted boolean.
func (d Redaction) MarshalJSON() ([]byte, error) {
	if d == RedactDetail {
		return []byte("true"), nil
	}

	return []byte("false"), nil
}

// Doc is the documentation a code cannot be registered without. Document is the
// only way to build one.
type Doc struct {
	summary    string
	action     string
	retry      Retry
	redaction  Redaction
	params     []string
	deprecated *vocabulary.Deprecation
}

// Deprecating returns a copy of the documentation marked as going away. since
// is the application release the marker ships in. replacedBy is the code to
// move to, or empty. The code stays in use until a later release removes it.
func (d Doc) Deprecating(since, replacedBy string) Doc {
	d.deprecated = vocabulary.Deprecate(since, replacedBy)

	return d
}

// Document states what went wrong, what the caller should do about it
// (NoAction when the answer is nothing), whether a retry can succeed, whether
// the occurrence detail may be sent to a client, and the param names the code
// interpolates. A param the entry does not name is dropped from the rendering.
func Document(summary, action string, retry Retry, redaction Redaction, params ...string) Doc {
	return Doc{
		summary:   summary,
		action:    action,
		retry:     retry,
		redaction: redaction,
		params:    cloneNames(params),
	}
}

// Entry is one registered code with its documentation. It round-trips through
// JSON.
type Entry struct {
	Code      Code      `json:"code"`
	Status    int       `json:"status"`
	Summary   string    `json:"summary"`
	Action    string    `json:"action"`
	Retry     Retry     `json:"retryable"`
	Redaction Redaction `json:"redacted"`
	Params    []string  `json:"params"`
	// Deprecated marks a code that is going away, and is nil otherwise. The
	// code is removed only in a later release.
	Deprecated *vocabulary.Deprecation `json:"deprecated,omitempty"`
}

// UnmarshalJSON reads an entry back and applies the code grammar.
func (e *Entry) UnmarshalJSON(data []byte) error {
	var wire struct {
		Code       string                  `json:"code"`
		Status     int                     `json:"status"`
		Summary    string                  `json:"summary"`
		Action     string                  `json:"action"`
		Retryable  bool                    `json:"retryable"`
		Redacted   bool                    `json:"redacted"`
		Params     []string                `json:"params"`
		Deprecated *vocabulary.Deprecation `json:"deprecated"`
	}

	if err := json.Unmarshal(data, &wire); err != nil {
		return fmt.Errorf("fault: decode entry: %w", err)
	}

	code, err := parseCode(wire.Code)
	if err != nil {
		return err
	}

	*e = Entry{
		Code:       code,
		Status:     wire.Status,
		Summary:    wire.Summary,
		Action:     wire.Action,
		Retry:      retryOf(wire.Retryable),
		Redaction:  redactionOf(wire.Redacted),
		Params:     cloneNames(wire.Params),
		Deprecated: wire.Deprecated,
	}

	return nil
}

func retryOf(allowed bool) Retry {
	if allowed {
		return RetryAllowed
	}

	return RetryNever
}

func redactionOf(redacted bool) Redaction {
	if redacted {
		return RedactDetail
	}

	return ShowDetail
}

// Definition is a registered code. Faults are built from it.
type Definition struct {
	entry Entry
}

// New returns a Fault for this code with an English message describing this
// occurrence.
func (d *Definition) New(message string) *Fault {
	if d == nil {
		return unregisteredFault(Code{}, message)
	}

	return &Fault{
		def:       d,
		message:   message,
		retryable: d.entry.Retry == RetryAllowed,
	}
}

// Code returns the identifier.
func (d *Definition) Code() Code {
	if d == nil {
		return Code{}
	}

	return d.entry.Code
}

// Entry returns a copy of the registered documentation.
func (d *Definition) Entry() Entry {
	if d == nil {
		return Entry{}
	}

	entry := d.entry
	entry.Params = cloneNames(entry.Params)

	return entry
}

func (d *Definition) declares(name string) bool {
	return d != nil && slices.Contains(d.entry.Params, name)
}

// Registry is the set of codes a binary can return. The zero value is an empty
// registry, ready to use.
type Registry struct {
	write sync.Mutex
	codes atomic.Pointer[map[Code]*Definition]
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry { return &Registry{} }

// Global returns the process-wide registry that the package-level Register and
// New use.
func Global() *Registry { return globalRegistry }

// Register documents a code in the global registry and returns its definition.
// It panics on a rejected registration, so call it from a package var or an
// init function. Use TryRegister after startup.
func Register(domain Domain, reason string, status int, doc Doc) *Definition {
	return globalRegistry.Register(domain, reason, status, doc)
}

// TryRegister documents a code in the global registry after startup, reporting
// a rejected registration instead of stopping the process.
func TryRegister(domain Domain, reason string, status int, doc Doc) (*Definition, error) {
	return globalRegistry.TryRegister(domain, reason, status, doc)
}

// Register documents a code and returns its definition, panicking if the
// registration is rejected. See the package-level Register.
func (r *Registry) Register(domain Domain, reason string, status int, doc Doc) *Definition {
	definition, err := r.TryRegister(domain, reason, status, doc)
	if err != nil {
		panic(err.Error())
	}

	return definition
}

// TryRegister documents a code and returns its definition. It reports
// ErrDuplicateCode, ErrUndocumented, ErrInvalidStatus, ErrInvalidParamName,
// ErrInvalidDomain or ErrInvalidReason.
func (r *Registry) TryRegister(domain Domain, reason string, status int, doc Doc) (*Definition, error) {
	code, err := NewCode(domain, reason)
	if err != nil {
		return nil, err
	}

	if docErr := doc.validate(code, status); docErr != nil {
		return nil, docErr
	}

	r.write.Lock()
	defer r.write.Unlock()

	current := r.definitions()
	if _, exists := current[code]; exists {
		return nil, fmt.Errorf("%w: %s", ErrDuplicateCode, code)
	}

	definition := &Definition{entry: Entry{
		Code:       code,
		Status:     status,
		Summary:    doc.summary,
		Action:     doc.action,
		Retry:      doc.retry,
		Redaction:  doc.redaction,
		Params:     cloneNames(doc.params),
		Deprecated: doc.deprecated,
	}}

	next := make(map[Code]*Definition, len(current)+1)
	maps.Copy(next, current)
	next[code] = definition
	r.codes.Store(&next)

	return definition, nil
}

// All returns every entry sorted by code.
func (r *Registry) All() []Entry {
	current := r.definitions()

	out := make([]Entry, 0, len(current))
	for _, definition := range current {
		out = append(out, definition.Entry())
	}

	slices.SortFunc(out, func(a, b Entry) int { return strings.Compare(a.Code.String(), b.Code.String()) })

	return out
}

// Lookup returns the entry registered for c.
func (r *Registry) Lookup(c Code) (Entry, bool) {
	definition, ok := r.definitions()[c]
	if !ok {
		return Entry{}, false
	}

	return definition.Entry(), true
}

// Registered reports whether c is documented here.
func (r *Registry) Registered(c Code) bool {
	_, ok := r.definitions()[c]

	return ok
}

// Snapshot serialises the registry as sorted, indented JSON ending in a
// newline. Identical registrations produce identical bytes.
func (r *Registry) Snapshot() ([]byte, error) {
	encoded, err := json.MarshalIndent(r.All(), "", "  ")
	if err != nil {
		return nil, fmt.Errorf("fault: snapshot registry: %w", err)
	}

	return append(encoded, '\n'), nil
}

func (r *Registry) definitions() map[Code]*Definition {
	if current := r.codes.Load(); current != nil {
		return *current
	}

	return nil
}

func (d Doc) validate(code Code, status int) error {
	if d.summary == "" {
		return fmt.Errorf("%w: %s has no summary", ErrUndocumented, code)
	}

	if d.action == "" {
		return fmt.Errorf("%w: %s has no action, pass NoAction when there is nothing to do", ErrUndocumented, code)
	}

	if !d.retry.Valid() || !d.redaction.Valid() {
		return fmt.Errorf("%w: %s has no retry or redaction setting", ErrUndocumented, code)
	}

	if status < minStatus || status > maxStatus {
		return fmt.Errorf("%w: %s declares %d, outside %d to %d", ErrInvalidStatus, code, status, minStatus, maxStatus)
	}

	for _, name := range d.params {
		if !validParamName(name) {
			return fmt.Errorf("%w: %s declares %q", ErrInvalidParamName, code, name)
		}
	}

	return nil
}

// cloneNames copies param names and never returns nil, so an entry with no
// params serialises as [] however it was built.
func cloneNames(names []string) []string {
	out := make([]string, len(names))
	copy(out, names)

	return out
}

func validParamName(name string) bool {
	if name == "" || len(name) > maxParamNameLen || !isLower(name[0]) {
		return false
	}

	for i := range len(name) {
		c := name[i]
		if !isLower(c) && !isDigit(c) && c != '_' {
			return false
		}
	}

	return true
}
