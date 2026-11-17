package extension

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/prusa3d/prusa-connect/sdk/event"
	"github.com/prusa3d/prusa-connect/sdk/vocabulary"
)

// The errors a declaration reports. Callers inspect them with errors.Is.
var (
	// ErrInvalidName reports a name that is not a DNS-1123 label.
	ErrInvalidName = errors.New("extension: invalid name")
	// ErrInvalidKind reports a kind outside the closed set.
	ErrInvalidKind = errors.New("extension: invalid kind")
	// ErrUndocumented reports a declaration missing a mandatory descriptor
	// field.
	ErrUndocumented = errors.New("extension: undocumented registration")
	// ErrInvalidVersion reports a version that is not MAJOR.MINOR.PATCH.
	ErrInvalidVersion = errors.New("extension: invalid version")
	// ErrIncompatibleContract reports an entry compiled against a contract this
	// host does not serve.
	ErrIncompatibleContract = errors.New("extension: incompatible contract version")
	// ErrInvalidText reports a descriptor string that cannot be published:
	// invalid UTF-8, a control character, a line break, or a value outside
	// the grammar of its field.
	ErrInvalidText = errors.New("extension: invalid descriptor text")
)

const (
	// CapabilityArgumentSeparator joins a capability verb to the name of one of
	// its arguments. It is not "." because argument names contain dots.
	// sdk/connectorapi uses this constant for its capability keys.
	CapabilityArgumentSeparator = "#"

	// MaxNameLen is the DNS-1123 label limit. A host that publishes a bound
	// on a name uses this one.
	MaxNameLen = 63

	// maxCapabilityLen bounds a capability key, verb and argument together.
	maxCapabilityLen = 253

	// lineSeparator and paragraphSeparator end a line without being control
	// characters, so unicode.IsControl does not catch them.
	lineSeparator      = '\u2028'
	paragraphSeparator = '\u2029'
)

// Kind is the extension point a registration serves. The set is closed. The
// host has one registry per kind, and a name is unique within a kind and a
// namespace.
type Kind string

const (
	// KindConnector is a printer dialect, registered with sdk/connectorapi.
	KindConnector Kind = "connector"
	// KindStorage is a storage backend, registered with sdk/storageapi.
	KindStorage Kind = "storage"
	// KindIdentity is a human identity provider, registered with sdk/authapi.
	KindIdentity Kind = "identity"
	// KindEventSink is a consumer of domain events, registered with
	// sdk/eventsapi.
	KindEventSink Kind = "event-sink"
	// KindRoutes is a set of additional HTTP routes, registered with
	// sdk/routesapi.
	KindRoutes Kind = "routes"
	// KindDatabase is the record backend behind sdk/stateapi: the store of a
	// module's key/value namespace.
	KindDatabase Kind = "database"
	// KindHistory is a history provider, registered with sdk/historyapi: the
	// long-term store of metrics and events.
	KindHistory Kind = "history"
	// KindCamera is a camera source.
	KindCamera Kind = "camera"
	// KindDeviceKind is a class of device a driver declares, with its verbs
	// and the class each verb takes. It is separate from KindConnector, so a
	// driver and a device class may have the same name.
	KindDeviceKind Kind = "device-kind"
	// KindLink is a link to one parent site. It sends this site's state up
	// and the parent's commands down. Only the application registers one.
	KindLink Kind = "link"
)

// Kinds returns every extension point, in the order the reference publishes
// them.
func Kinds() []Kind {
	return []Kind{
		KindConnector, KindStorage, KindIdentity, KindEventSink,
		KindRoutes, KindDatabase, KindHistory, KindCamera, KindDeviceKind, KindLink,
	}
}

// Valid reports whether k is a member of the closed set.
func (k Kind) Valid() bool { return slices.Contains(Kinds(), k) }

// String returns the kind as the reference and the config name it.
func (k Kind) String() string { return string(k) }

// Stability is what an operator may assume about an entry's surface between
// releases.
type Stability string

const (
	// StabilityExperimental means the name, the config keys and the events may
	// change in any release.
	StabilityExperimental Stability = "experimental"
	// StabilityStable means a break needs a major release and a migration note.
	StabilityStable Stability = "stable"
	// StabilityDeprecated means the entry still works and will be removed. It
	// is the one level that requires ReplacedBy.
	StabilityDeprecated Stability = "deprecated"
)

// Valid reports whether s is a member of the closed set.
func (s Stability) Valid() bool {
	switch s {
	case StabilityExperimental, StabilityStable, StabilityDeprecated:
		return true
	}

	return false
}

// String returns the level as the reference names it.
func (s Stability) String() string { return string(s) }

// Version is MAJOR.MINOR.PATCH. It takes no prerelease and no build
// identifier.
type Version string

// String returns the version as text.
func (v Version) String() string { return string(v) }

// Parse reads the three fields, reporting ErrInvalidVersion for anything else.
func (v Version) Parse() (major, minor, patch int, err error) {
	majorText, rest, found := strings.Cut(string(v), ".")
	if !found {
		return 0, 0, 0, fmt.Errorf("%w: %q is not MAJOR.MINOR.PATCH", ErrInvalidVersion, v)
	}

	minorText, patchText, found := strings.Cut(rest, ".")
	if !found || strings.Contains(patchText, ".") {
		return 0, 0, 0, fmt.Errorf("%w: %q is not MAJOR.MINOR.PATCH", ErrInvalidVersion, v)
	}

	if major, err = versionField(v, majorText); err != nil {
		return 0, 0, 0, err
	}

	if minor, err = versionField(v, minorText); err != nil {
		return 0, 0, 0, err
	}

	if patch, err = versionField(v, patchText); err != nil {
		return 0, 0, 0, err
	}

	return major, minor, patch, nil
}

// versionField refuses a zero-padded field, so "1.02.0" and "1.2.0" cannot both
// name one version.
func versionField(v Version, field string) (int, error) {
	if field == "" || (len(field) > 1 && field[0] == '0') {
		return 0, fmt.Errorf("%w: %q has an empty or zero-padded field", ErrInvalidVersion, v)
	}

	n, err := strconv.Atoi(field)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("%w: %q is not MAJOR.MINOR.PATCH", ErrInvalidVersion, v)
	}

	return n, nil
}

// CompatibleWith reports whether an entry compiled against v can run on a host
// serving contract version host. The major must match, and the entry may not
// ask for a minor the host does not have.
func (v Version) CompatibleWith(host Version) error {
	major, minor, _, err := v.Parse()
	if err != nil {
		return err
	}

	hostMajor, hostMinor, _, err := host.Parse()
	if err != nil {
		return fmt.Errorf("host contract %w", err)
	}

	if major != hostMajor {
		return fmt.Errorf("%w: built against %s, this host serves %s", ErrIncompatibleContract, v, host)
	}

	if minor > hostMinor {
		return fmt.Errorf("%w: built against %s, this host serves %s and has none of what %s added",
			ErrIncompatibleContract, v, host, v)
	}

	return nil
}

// OptionType is the shape of one config value. A settings form is rendered
// from it.
type OptionType string

const (
	// OptionString is free text.
	OptionString OptionType = "string"
	// OptionInt is a whole number.
	OptionInt OptionType = "int"
	// OptionNumber is a decimal.
	OptionNumber OptionType = "number"
	// OptionBool is true or false.
	OptionBool OptionType = "bool"
	// OptionDuration is a duration as time.ParseDuration reads one.
	OptionDuration OptionType = "duration"
	// OptionURL is an absolute URL.
	OptionURL OptionType = "url"
	// OptionSecret is a credential. A form masks it and it is never logged.
	OptionSecret OptionType = "secret"
	// OptionList is a sequence of values.
	OptionList OptionType = "list"
	// OptionObject is a nested block the entry parses itself.
	OptionObject OptionType = "object"
)

// Valid reports whether t is a member of the closed set.
func (t OptionType) Valid() bool {
	switch t {
	case OptionString, OptionInt, OptionNumber, OptionBool,
		OptionDuration, OptionURL, OptionSecret, OptionList, OptionObject:
		return true
	}

	return false
}

// String returns the type as the reference names it.
func (t OptionType) String() string { return string(t) }

// Option is one key an entry reads from its private config block.
type Option struct {
	// Key is the key as an operator writes it, in lower_snake_case.
	Key string `json:"key"`
	// Type is the shape of the value.
	Type OptionType `json:"type"`
	// Required says whether the entry refuses to build without it.
	Required bool `json:"required"`
	// Default is what a missing optional key means, rendered as the operator
	// would write it. It is empty for a required key.
	Default string `json:"default,omitempty"`
	// Summary is one line saying what the key does.
	Summary string `json:"summary"`
}

// Emission is one event type an entry publishes onto the host's event bus,
// with the version of that type's payload. Declare validates Type with
// event.ValidateType.
type Emission struct {
	// Type is the CloudEvents type, in reverse-DNS form.
	Type string `json:"type"`
	// Version is the payload version, bumped when the data member changes
	// shape.
	Version Version `json:"version"`
	// Summary is one line saying when the event is published.
	Summary string `json:"summary"`
}

// Destination is one place an entry publishes to on a transport of its own: an
// MQTT topic, an AMQP routing key, a Kafka message kind, the discriminator in a
// webhook body. A subscriber of that transport matches on it. A type in the
// host's event vocabulary belongs in Emits.
type Destination struct {
	// Address is the topic, routing key, subject or discriminator, with the
	// placeholders that transport writes left in place.
	Address string `json:"address"`
	// Version is the payload version, bumped when the message body changes
	// shape.
	Version Version `json:"version"`
	// Summary is one line saying when the message is published.
	Summary string `json:"summary"`
}

// Requirement is one thing an entry cannot run without. The host refuses an
// entry whose requirement it cannot meet, before the entry is built. A
// requirement names one subject, and Declare refuses one that names two.
type Requirement struct {
	// Capability is the host capability, in the vocabulary the host publishes.
	Capability string `json:"capability,omitempty"`
	// Type is another entry, as "namespace/name". It is met when one enabled
	// instance of that type is ready.
	Type string `json:"type,omitempty"`
	// Core is the range of core versions the entry runs on: an exact version,
	// ^, ~, a comparison, a hyphen range, a wildcard, or alternatives joined
	// by ||. This package does not parse it.
	Core string `json:"core,omitempty"`
	// Since is the contract version that introduced it.
	Since Version `json:"since"`
}

// Subject returns the one thing the requirement names, and the empty string
// for a requirement that names none.
func (r Requirement) Subject() string {
	switch {
	case r.Capability != "":
		return r.Capability
	case r.Type != "":
		return r.Type
	}

	return r.Core
}

// Doc is the documentation an entry cannot be registered without.
//
// Summary, Description, Version, Contract and Stability are mandatory, and a
// deprecated entry must also say what replaces it. Emits, Options and Requires
// are optional. An element present in any of them is validated in full.
type Doc struct {
	// Summary is one line, and the only text a catalogue listing shows.
	Summary string `json:"summary"`
	// Description is the prose an author reads before choosing this entry. It
	// says what the entry talks to and what it does not do.
	Description string `json:"description"`
	// Version is the implementation's own version. It changes when the
	// behaviour does.
	Version Version `json:"version"`
	// Contract is the version of the extension contract this entry was
	// compiled against.
	Contract Version `json:"contract"`
	// Stability is what an operator may assume between releases.
	Stability Stability `json:"stability"`
	// ReplacedBy names the entry to move to. It is mandatory when Stability is
	// deprecated and refused otherwise.
	ReplacedBy string `json:"replaced_by,omitempty"`
	// Deprecated marks an entry that is going away, and is nil otherwise. It
	// is the marker every published set uses. Stability and ReplacedBy say
	// the same for this set.
	Deprecated *vocabulary.Deprecation `json:"deprecated,omitempty"`
	// Emits is the event types this entry publishes onto the host's event bus.
	Emits []Emission `json:"emits,omitempty"`
	// Publishes is the addresses this entry publishes to on a transport of its
	// own.
	Publishes []Destination `json:"publishes,omitempty"`
	// Options is the config keys this entry reads.
	Options []Option `json:"options,omitempty"`
	// Requires is what this entry cannot run without: a host capability,
	// another entry's type, or a range of core versions.
	Requires []Requirement `json:"requires,omitempty"`
	// Consumes is the event classes this registration receives from the
	// host's event bus. Empty means none. It is a subscription filter: it
	// never widens what the registration's principal may see.
	Consumes []event.Class `json:"consumes,omitempty"`
}

// Descriptor is one registered entry: the identity the host derived, and the
// documentation its author wrote. Declare is the only way to build one. It
// round-trips through JSON.
type Descriptor struct {
	kind      Kind
	name      string
	namespace Namespace
	module    string
	doc       Doc
}

// Kind returns the extension point the entry serves.
func (d Descriptor) Kind() Kind { return d.kind }

// Name returns the bare name, as an operator writes it in configuration.
func (d Descriptor) Name() string { return d.name }

// Namespace returns the host-derived owner of the name.
func (d Descriptor) Namespace() Namespace { return d.namespace }

// Module returns the module path the namespace was derived from.
func (d Descriptor) Module() string { return d.module }

// Doc returns a copy of the documentation.
func (d Descriptor) Doc() Doc {
	doc := d.doc
	doc.Emits = slices.Clone(d.doc.Emits)
	doc.Publishes = slices.Clone(d.doc.Publishes)
	doc.Options = slices.Clone(d.doc.Options)
	doc.Requires = slices.Clone(d.doc.Requires)
	doc.Consumes = slices.Clone(d.doc.Consumes)

	return doc
}

// Qualified renders "namespace/name", which is unique within a kind.
func (d Descriptor) Qualified() string { return d.namespace.String() + pathSeparator + d.name }

// IsZero reports whether the descriptor was never declared.
func (d Descriptor) IsZero() bool { return d.kind == "" && d.name == "" }

// String renders the entry as the reference and every error message name it.
func (d Descriptor) String() string { return d.kind.String() + " " + d.Qualified() }

// Declare mints a descriptor for the package that called it. The namespace is
// derived from the calling package's module, so a caller cannot claim another
// module's namespace.
//
// host is the contract version the calling registry serves. An entry compiled
// against an incompatible one is refused.
func Declare(kind Kind, host Version, name string, doc Doc) (Descriptor, error) {
	if !kind.Valid() {
		return Descriptor{}, fmt.Errorf("%w: %q", ErrInvalidKind, kind)
	}

	if err := ValidateName(name); err != nil {
		return Descriptor{}, err
	}

	if err := doc.validate(kind, name, host); err != nil {
		return Descriptor{}, err
	}

	module, namespace, err := callerModule()
	if err != nil {
		return Descriptor{}, err
	}

	return Descriptor{kind: kind, name: name, namespace: namespace, module: module, doc: doc.clone()}, nil
}

// ValidateName reports whether name is a DNS-1123 label: lowercase
// alphanumerics and '-', starting and ending with an alphanumeric, at most 63
// bytes. Such a name is legal as a config key, a metric label value, a URL
// path segment and a messaging subject token.
func ValidateName(name string) error {
	if name == "" {
		return fmt.Errorf("%w: the name is empty", ErrInvalidName)
	}

	if len(name) > MaxNameLen {
		return fmt.Errorf("%w: %q is %d bytes, and the limit is %d",
			ErrInvalidName, name, len(name), MaxNameLen)
	}

	for i := range len(name) {
		c := name[i]

		alphanumeric := (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9')
		if alphanumeric || (c == '-' && i > 0 && i < len(name)-1) {
			continue
		}

		return fmt.Errorf("%w: %q, which is not lowercase alphanumerics and inner hyphens",
			ErrInvalidName, name)
	}

	return nil
}

func (d Doc) clone() Doc {
	out := d
	out.Emits = slices.Clone(d.Emits)
	out.Publishes = slices.Clone(d.Publishes)
	out.Options = slices.Clone(d.Options)
	out.Requires = slices.Clone(d.Requires)
	out.Consumes = slices.Clone(d.Consumes)

	return out
}

func (d Doc) validate(kind Kind, name string, host Version) error {
	where := kind.String() + " " + name

	if err := d.validateText(where); err != nil {
		return err
	}

	switch {
	case blank(d.Summary):
		return fmt.Errorf("%w: %s has no summary", ErrUndocumented, where)
	case blank(d.Description):
		return fmt.Errorf("%w: %s has no description", ErrUndocumented, where)
	case d.Version == "":
		return fmt.Errorf("%w: %s declares no version", ErrUndocumented, where)
	case d.Contract == "":
		return fmt.Errorf("%w: %s declares no contract version", ErrUndocumented, where)
	case !d.Stability.Valid():
		return fmt.Errorf("%w: %s declares stability %q, want one of experimental, stable, deprecated",
			ErrUndocumented, where, d.Stability)
	case d.Stability == StabilityDeprecated && blank(d.ReplacedBy):
		return fmt.Errorf("%w: %s is deprecated and names nothing that replaces it", ErrUndocumented, where)
	case d.Stability != StabilityDeprecated && !blank(d.ReplacedBy):
		return fmt.Errorf("%w: %s is %s and names a replacement, which only a deprecated entry carries",
			ErrUndocumented, where, d.Stability)
	}

	if _, _, _, err := d.Version.Parse(); err != nil {
		return fmt.Errorf("%s: %w", where, err)
	}

	if err := d.Contract.CompatibleWith(host); err != nil {
		return fmt.Errorf("%s: %w", where, err)
	}

	return d.validateMembers(where)
}

// validateText refuses text the published reference cannot show, such as a
// line break or a control character.
func (d Doc) validateText(where string) error {
	for _, field := range []struct {
		name  string
		value string
		span  bool
	}{
		{name: "summary", value: d.Summary},
		{name: "description", value: d.Description},
		{name: "replacement", value: d.ReplacedBy, span: true},
	} {
		if err := publishable(where, field.name, field.value, field.span); err != nil {
			return err
		}
	}

	if !blank(d.ReplacedBy) && !isName(d.ReplacedBy) {
		return fmt.Errorf("%w: %s names %q as its replacement, which is not a name or a namespace/name",
			ErrInvalidText, where, d.ReplacedBy)
	}

	for _, option := range d.Options {
		if err := option.validateText(where); err != nil {
			return err
		}
	}

	for _, emission := range d.Emits {
		if err := emission.validateText(where); err != nil {
			return err
		}
	}

	for _, destination := range d.Publishes {
		if err := destination.validateText(where); err != nil {
			return err
		}
	}

	for _, requirement := range d.Requires {
		if err := requirement.validateText(where); err != nil {
			return err
		}
	}

	return nil
}

// validateText checks each subject against the grammar of the thing it names.
func (r Requirement) validateText(where string) error {
	for _, field := range []struct{ name, value string }{
		{name: "capability", value: r.Capability},
		{name: "required type", value: r.Type},
		{name: "core range", value: r.Core},
	} {
		if err := publishable(where, field.name, field.value, true); err != nil {
			return err
		}
	}

	if !blank(r.Capability) && !IsCapabilityKey(r.Capability) {
		return fmt.Errorf("%w: %s requires %q, which is not a capability key",
			ErrInvalidText, where, r.Capability)
	}

	if !blank(r.Type) && !isName(r.Type) {
		return fmt.Errorf("%w: %s requires type %q, which is not a name or a namespace/name",
			ErrInvalidText, where, r.Type)
	}

	return nil
}

func (d Doc) validateMembers(where string) error {
	for _, option := range d.Options {
		if err := option.validate(where); err != nil {
			return err
		}
	}

	for _, emission := range d.Emits {
		if err := emission.validate(where); err != nil {
			return err
		}
	}

	for _, destination := range d.Publishes {
		if err := destination.validate(where); err != nil {
			return err
		}
	}

	for _, requirement := range d.Requires {
		if err := requirement.validate(where); err != nil {
			return err
		}
	}

	// An unknown class is refused, because a filter naming one would match
	// nothing.
	for _, class := range d.Consumes {
		if !class.Valid() {
			return fmt.Errorf("%w: %s consumes %q, which is not a message class",
				ErrInvalidText, where, class)
		}
	}

	return nil
}

func (o Option) validateText(where string) error {
	if err := publishable(where, "config key", o.Key, true); err != nil {
		return err
	}

	if !blank(o.Key) && !isConfigKey(o.Key) {
		return fmt.Errorf("%w: %s declares config key %q, which is not lower_snake_case",
			ErrInvalidText, where, o.Key)
	}

	if err := publishable(where, "the default of "+o.Key, o.Default, true); err != nil {
		return err
	}

	return publishable(where, "the summary of "+o.Key, o.Summary, false)
}

func (o Option) validate(where string) error {
	switch {
	case blank(o.Key):
		return fmt.Errorf("%w: %s declares a config key with no name", ErrUndocumented, where)
	case !o.Type.Valid():
		return fmt.Errorf("%w: %s declares config key %q with type %q", ErrUndocumented, where, o.Key, o.Type)
	case blank(o.Summary):
		return fmt.Errorf("%w: %s declares config key %q with no summary", ErrUndocumented, where, o.Key)
	case o.Required && o.Default != "":
		return fmt.Errorf("%w: %s declares config key %q as required and gives it a default",
			ErrUndocumented, where, o.Key)
	}

	return nil
}

func (e Emission) validateText(where string) error {
	if err := publishable(where, "event type", e.Type, true); err != nil {
		return err
	}

	// ValidateType also refuses an unregistered type under the core
	// namespace, as the publish path does.
	if !blank(e.Type) {
		if err := event.ValidateType(e.Type); err != nil {
			return fmt.Errorf("%w: %s emits %q: %w", ErrInvalidText, where, e.Type, err)
		}
	}

	return publishable(where, "the summary of "+e.Type, e.Summary, false)
}

func (t Destination) validateText(where string) error {
	if err := publishable(where, "address", t.Address, true); err != nil {
		return err
	}

	return publishable(where, "the summary of "+t.Address, t.Summary, false)
}

func (t Destination) validate(where string) error {
	if blank(t.Address) {
		return fmt.Errorf("%w: %s declares a destination with no address", ErrUndocumented, where)
	}

	if blank(t.Summary) {
		return fmt.Errorf("%w: %s publishes to %q with no summary", ErrUndocumented, where, t.Address)
	}

	if _, _, _, err := t.Version.Parse(); err != nil {
		return fmt.Errorf("%s destination %q: %w", where, t.Address, err)
	}

	return nil
}

func (e Emission) validate(where string) error {
	if blank(e.Type) {
		return fmt.Errorf("%w: %s declares an event with no type", ErrUndocumented, where)
	}

	if blank(e.Summary) {
		return fmt.Errorf("%w: %s declares event %q with no summary", ErrUndocumented, where, e.Type)
	}

	if _, _, _, err := e.Version.Parse(); err != nil {
		return fmt.Errorf("%s event %q: %w", where, e.Type, err)
	}

	return nil
}

func (r Requirement) validate(where string) error {
	named := 0

	for _, subject := range []string{r.Capability, r.Type, r.Core} {
		if !blank(subject) {
			named++
		}
	}

	if named != 1 {
		return fmt.Errorf("%w: %s declares a requirement naming %d of capability, type and core, "+
			"and one requirement names exactly one of them", ErrUndocumented, where, named)
	}

	if _, _, _, err := r.Since.Parse(); err != nil {
		return fmt.Errorf("%s requirement %q: %w", where, r.Subject(), err)
	}

	return nil
}

// blank reports whether a field is empty or only spaces.
func blank(value string) bool { return strings.TrimSpace(value) == "" }

// publishable refuses text the reference cannot show. span says the value is
// published inside a markdown code span, where a backtick is refused.
func publishable(where, field, value string, span bool) error {
	if !utf8.ValidString(value) {
		return fmt.Errorf("%w: the %s of %s is not valid UTF-8", ErrInvalidText, field, where)
	}

	for _, r := range value {
		switch {
		case unicode.IsControl(r), r == lineSeparator, r == paragraphSeparator:
			return fmt.Errorf("%w: the %s of %s carries %q, which ends the line it is published on",
				ErrInvalidText, field, where, r)
		case span && r == '`':
			return fmt.Errorf("%w: the %s of %s carries a backtick, and it is published inside a code span",
				ErrInvalidText, field, where)
		}
	}

	return nil
}

// IsConfigKey reports whether key is lower_snake_case.
func IsConfigKey(key string) bool { return isConfigKey(key) }

// isConfigKey reports whether key is lower_snake_case.
func isConfigKey(key string) bool {
	if key == "" || len(key) > MaxNameLen || key[0] < 'a' || key[0] > 'z' {
		return false
	}

	for i := range len(key) {
		c := key[i]
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '_' {
			return false
		}
	}

	return true
}

// IsCapabilityKey reports whether key is one in the host's capability
// vocabulary: dotted segments of lowercase alphanumerics with '-' and '_'
// between, optionally followed by CapabilityArgumentSeparator and the name of
// one argument of that verb, at most 253 bytes.
func IsCapabilityKey(key string) bool {
	if key == "" || len(key) > maxCapabilityLen {
		return false
	}

	verb, argument, hasArgument := strings.Cut(key, CapabilityArgumentSeparator)
	if !isDottedName(verb) {
		return false
	}

	if hasArgument {
		return isDottedName(argument)
	}

	return true
}

// isDottedName reports whether s is one or more label segments joined by dots.
func isDottedName(s string) bool {
	if s == "" {
		return false
	}

	for segment := range strings.SplitSeq(s, namespaceSeparator) {
		if !isLabel(segment) {
			return false
		}
	}

	return true
}

// isLabel reports whether s is a lowercase alphanumeric at each end with '-' or
// '_' between: a DNS-1123 label that also allows the underscore.
func isLabel(s string) bool {
	if s == "" {
		return false
	}

	for i := range len(s) {
		c := s[i]

		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9':
		case (c == '-' || c == '_') && i > 0 && i < len(s)-1:
		default:
			return false
		}
	}

	return true
}

// isName reports whether value is a bare name or a qualified namespace/name.
func isName(value string) bool {
	namespace, name, qualified := strings.Cut(value, pathSeparator)
	if !qualified {
		return ValidateName(value) == nil
	}

	if ValidateName(name) != nil {
		return false
	}

	for label := range strings.SplitSeq(namespace, namespaceSeparator) {
		if ValidateName(label) != nil {
			return false
		}
	}

	return true
}
