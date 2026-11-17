// Package stateapi is the key/value store an extension keeps its own data in.
//
// The host gives each extension a namespace of its own. No other extension
// can read or write it. A value survives a restart, an upgrade and the
// removal of the extension.
//
// Each call is atomic on one key. There is no transaction across keys, no
// query over values and no watch. Use CompareAndPut when a key has more than
// one writer.
//
// A namespace is limited to DefaultNamespaceBytes unless the operator granted
// more at install. Values are opaque bytes.
package stateapi

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// The limits of one namespace. The host enforces them.
const (
	// MaxKeyBytes is the longest key, in bytes of UTF-8.
	MaxKeyBytes = 150

	// MaxValueBytes is the largest value. Keep anything larger as a file in a
	// storage.
	MaxValueBytes = 64 << 10

	// DefaultNamespaceBytes is the limit on the total of every key and value
	// in one namespace, unless the operator granted more.
	DefaultNamespaceBytes = 8 << 20

	// MaxGrantedNamespaceBytes is the most an operator can grant one
	// namespace.
	MaxGrantedNamespaceBytes = 1 << 30
)

// NamespaceMiB is the unit of a namespace limit in a manifest and in a grant:
// one mebibyte.
const NamespaceMiB = 1 << 20

// Usage is how much one namespace stores, and its limit.
type Usage struct {
	// Bytes is the sum of Cost over every record in the namespace.
	Bytes int64 `json:"bytes"`
	// Limit is the most the namespace may store: DefaultNamespaceBytes, or
	// what the operator granted.
	Limit int64 `json:"limit"`
}

// The errors a Records call returns. Callers inspect them with errors.Is.
var (
	// ErrNoRecord reports a key with no record. Get, Delete and Version
	// return it.
	ErrNoRecord = errors.New("stateapi: the namespace holds no record under that key")

	// ErrVersion reports a CompareAndPut whose expected version is not the
	// stored version. Read the record again before the next attempt.
	ErrVersion = errors.New("stateapi: the record is at a different version")

	// ErrKey reports a key that Key.Validate refuses.
	ErrKey = errors.New("stateapi: the key is not one this door accepts")

	// ErrValueTooLarge reports a value over MaxValueBytes.
	ErrValueTooLarge = errors.New("stateapi: the value is over the limit for one record")

	// ErrNamespaceFull reports a write that would take the namespace over its
	// limit. Nothing is written. Delete a record to make room.
	ErrNamespaceFull = errors.New("stateapi: the namespace is at its byte limit")

	// ErrNotReady reports a call made before the host marked the module
	// ready. It can happen during the module's start. Wait a moment and call
	// again.
	ErrNotReady = errors.New("stateapi: the host has not marked this module ready")

	// ErrBusy reports a call refused because the module already has as many
	// state calls running as the host allows it at once. The limit is per
	// module. Retry the call.
	ErrBusy = errors.New("stateapi: too many of this module's state calls are already running")
)

// Key names one record inside a namespace. The namespace is never part of a
// key.
type Key string

// keySeparator separates the levels of a key hierarchy.
const keySeparator = "/"

// Validate reports whether a key is valid: not empty, valid UTF-8, at most
// MaxKeyBytes, with no control character and no leading or trailing "/".
func (k Key) Validate() error {
	switch {
	case k == "":
		return fmt.Errorf("%w: it is empty", ErrKey)
	case len(k) > MaxKeyBytes:
		return fmt.Errorf("%w: it is %d bytes and the limit is %d", ErrKey, len(k), MaxKeyBytes)
	case !utf8.ValidString(string(k)):
		return fmt.Errorf("%w: it is not valid UTF-8", ErrKey)
	case strings.HasPrefix(string(k), keySeparator), strings.HasSuffix(string(k), keySeparator):
		return fmt.Errorf("%w: it opens or ends with %q", ErrKey, keySeparator)
	}

	for _, r := range k {
		if r < ' ' || r == 0x7f {
			return fmt.Errorf("%w: it carries a control character", ErrKey)
		}
	}

	return nil
}

// Value is one stored record: the bytes and their version, read together.
type Value struct {
	// Bytes is what the last writer stored.
	Bytes []byte
	// Version is the record's version. Pass it to CompareAndPut. It rises by
	// one on every write. It starts again at 1 when a key is deleted and
	// written again, so a version can repeat across a delete.
	Version uint64
}

// AbsentVersion is the version of a record that does not exist. Pass it to
// CompareAndPut to write only when there is no record.
const AbsentVersion uint64 = 0

// Records is the key/value store of one extension, in one namespace. The host
// sets the namespace, and no argument names it. Every method returns an error
// that errors.Is matches against the errors of this package.
type Records interface {
	// Get returns the record under key, and ErrNoRecord when there is none.
	Get(ctx context.Context, key Key) (Value, error)

	// Put writes value under key, whatever is there, and returns the new
	// version. Use it for a key with one writer.
	Put(ctx context.Context, key Key, value []byte) (uint64, error)

	// CompareAndPut writes value under key only if the record is at expected,
	// and returns the new version. AbsentVersion expects no record. On
	// ErrVersion, read the record again and decide again.
	CompareAndPut(ctx context.Context, key Key, expected uint64, value []byte) (uint64, error)

	// Delete removes the record under key, and returns ErrNoRecord when there
	// is none. A key deleted and written again starts a new version line at 1.
	Delete(ctx context.Context, key Key) error

	// List returns every key under prefix, sorted, without values. The empty
	// prefix lists every key in the namespace.
	List(ctx context.Context, prefix Key) ([]Key, error)

	// Version returns the version of the record under key without reading its
	// bytes, and ErrNoRecord when there is none.
	Version(ctx context.Context, key Key) (uint64, error)

	// Usage returns how much the namespace stores, and its limit.
	Usage(ctx context.Context) (Usage, error)
}

// ValidatePrefix reports whether a prefix is one List accepts. Unlike a key,
// a prefix may be empty and may end with "/".
func ValidatePrefix(prefix Key) error {
	if prefix == "" {
		return nil
	}

	if len(prefix) > MaxKeyBytes {
		return fmt.Errorf("%w: the prefix is %d bytes and the limit is %d",
			ErrKey, len(prefix), MaxKeyBytes)
	}

	if !utf8.ValidString(string(prefix)) {
		return fmt.Errorf("%w: the prefix is not valid UTF-8", ErrKey)
	}

	for _, r := range prefix {
		if r < ' ' || r == 0x7f {
			return fmt.Errorf("%w: the prefix carries a control character", ErrKey)
		}
	}

	return nil
}

// ValidateValue reports whether a value is at most MaxValueBytes. A nil value
// is valid and stores a record of zero bytes.
func ValidateValue(value []byte) error {
	if len(value) > MaxValueBytes {
		return fmt.Errorf("%w: it is %d bytes and the limit is %d",
			ErrValueTooLarge, len(value), MaxValueBytes)
	}

	return nil
}

// Cost is what one record counts toward the namespace's limit: the length of
// its key plus the length of its value.
func Cost(key Key, value []byte) int64 {
	return int64(len(key)) + int64(len(value))
}
