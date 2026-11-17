package fractal

import (
	"errors"
	"strconv"
)

// The maximum lengths the protocol sets, in characters.
const (
	// MaxNodeIDLength is the maximum length of a Node ID.
	MaxNodeIDLength = 64
	// MaxNodePathLength is the maximum length of a Node Path.
	MaxNodePathLength = 255
)

// The reasons a name is invalid. A Validate method and a Parse function in this
// package return a *NameError that wraps one of these. Test with errors.Is.
var (
	// ErrEmpty reports that the name is the empty string.
	ErrEmpty = errors.New("empty")
	// ErrTooLong reports that the name is over its maximum length.
	ErrTooLong = errors.New("longer than the bound")
	// ErrBadCharacter reports that the name has a character its grammar does
	// not allow.
	ErrBadCharacter = errors.New("character outside the grammar")
	// ErrBadFirstCharacter reports that the name starts with a character its
	// grammar allows only after the first position: a digit in an Identifier,
	// a hyphen in a Node ID.
	ErrBadFirstCharacter = errors.New("first character outside the grammar")
	// ErrEmptySegment reports that a Node Path has two slashes in a row, or a
	// slash at either end.
	ErrEmptySegment = errors.New("empty path segment")
)

// NameError says which name is invalid and why. Err is one of the errors above,
// and Unwrap returns it.
type NameError struct {
	// Kind names the grammar the value was checked against, for example
	// "node id".
	Kind string
	// Value is the invalid string.
	Value string
	// Err is the reason the string is invalid.
	Err error
}

// Error returns the kind, the quoted value and the reason.
func (e *NameError) Error() string {
	return e.Kind + " " + strconv.Quote(e.Value) + ": " + e.Err.Error()
}

// Unwrap returns the reason the name is invalid.
func (e *NameError) Unwrap() error { return e.Err }

// nameErr builds a *NameError, or returns nil when err is nil.
func nameErr(kind, value string, err error) error {
	if err == nil {
		return nil
	}
	return &NameError{Kind: kind, Value: value, Err: err}
}

// identifierByte reports whether c may appear in an Identifier.
func identifierByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_'
}

// digitByte reports whether c is an ASCII digit.
func digitByte(c byte) bool { return c >= '0' && c <= '9' }

// checkIdentifier applies the Identifier grammar. When digitStart is true, the
// first character may be a digit. Only the Interface Instance ID allows that.
func checkIdentifier(s string, digitStart bool) error {
	if s == "" {
		return ErrEmpty
	}
	if digitByte(s[0]) && !digitStart {
		return ErrBadFirstCharacter
	}
	for i := range len(s) {
		if !identifierByte(s[i]) {
			return ErrBadCharacter
		}
	}
	return nil
}

// Identifier is the grammar the other names build on: ASCII letters, digits and
// underscores, and the first character is a letter or an underscore.
type Identifier string

// Validate applies the Identifier grammar.
func (n Identifier) Validate() error {
	return nameErr("identifier", string(n), checkIdentifier(string(n), false))
}

// String returns the identifier unchanged.
func (n Identifier) String() string { return string(n) }

// ParseIdentifier validates s and returns it as an Identifier.
func ParseIdentifier(s string) (Identifier, error) {
	n := Identifier(s)
	return n, n.Validate()
}

// InterfaceName names an interface, which is a set of properties and methods.
// An interface name is unique in the whole network and follows the Identifier
// grammar.
type InterfaceName string

// Validate applies the Identifier grammar.
func (n InterfaceName) Validate() error {
	return nameErr("interface name", string(n), checkIdentifier(string(n), false))
}

// String returns the name unchanged.
func (n InterfaceName) String() string { return string(n) }

// ParseInterfaceName validates s and returns it as an InterfaceName.
func ParseInterfaceName(s string) (InterfaceName, error) {
	n := InterfaceName(s)
	return n, n.Validate()
}

// InstanceID identifies one instance of an interface on a node. A node may have
// several instances of the same interface, for example one per filament slot.
// An InstanceID follows the Identifier grammar and may start with a digit.
type InstanceID string

// Validate applies the Identifier grammar and allows a digit as the first
// character.
func (n InstanceID) Validate() error {
	return nameErr("instance id", string(n), checkIdentifier(string(n), true))
}

// String returns the id unchanged.
func (n InstanceID) String() string { return string(n) }

// ParseInstanceID validates s and returns it as an InstanceID.
func ParseInstanceID(s string) (InstanceID, error) {
	n := InstanceID(s)
	return n, n.Validate()
}

// PropertyName names a property within its interface. It follows the Identifier
// grammar.
type PropertyName string

// Validate applies the Identifier grammar.
func (n PropertyName) Validate() error {
	return nameErr("property name", string(n), checkIdentifier(string(n), false))
}

// String returns the name unchanged.
func (n PropertyName) String() string { return string(n) }

// ParsePropertyName validates s and returns it as a PropertyName.
func ParsePropertyName(s string) (PropertyName, error) {
	n := PropertyName(s)
	return n, n.Validate()
}

// MethodName names a method within its interface. It follows the Identifier
// grammar.
type MethodName string

// Validate applies the Identifier grammar.
func (n MethodName) Validate() error {
	return nameErr("method name", string(n), checkIdentifier(string(n), false))
}

// String returns the name unchanged.
func (n MethodName) String() string { return string(n) }

// ParseMethodName validates s and returns it as a MethodName.
func ParseMethodName(s string) (MethodName, error) {
	n := MethodName(s)
	return n, n.Validate()
}

// checkNodeIDSegment applies the character rules of a Node ID. It does not
// check the length. checkNodeID does that before it calls this function.
func checkNodeIDSegment(s string) error {
	if s == "" {
		return ErrEmpty
	}
	if s[0] == '-' {
		return ErrBadFirstCharacter
	}
	for i := range len(s) {
		if c := s[i]; !identifierByte(c) && c != '-' {
			return ErrBadCharacter
		}
	}
	return nil
}

// checkNodeID applies the whole Node ID grammar.
func checkNodeID(s string) error {
	if len(s) > MaxNodeIDLength {
		return ErrTooLong
	}
	return checkNodeIDSegment(s)
}

// NodeID identifies a node among the nodes that joined the same node. It is
// made of ASCII letters, digits, underscores and hyphens, does not start with a
// hyphen, and has at most MaxNodeIDLength characters.
type NodeID string

// Validate applies the Node ID grammar.
func (n NodeID) Validate() error {
	return nameErr("node id", string(n), checkNodeID(string(n)))
}

// String returns the id unchanged.
func (n NodeID) String() string { return string(n) }

// ParseNodeID validates s and returns it as a NodeID.
func ParseNodeID(s string) (NodeID, error) {
	n := NodeID(s)
	return n, n.Validate()
}
