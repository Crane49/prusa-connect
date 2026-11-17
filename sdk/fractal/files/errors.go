package files

import "errors"

// The reasons this package refuses an input. Every error it returns wraps one
// of them, so a caller tests the reason with errors.Is.
var (
	// ErrMalformed reports an input that is not in the form the extension
	// defines.
	ErrMalformed = errors.New("malformed")
	// ErrUnsupportedVersion reports a listing or a join entry whose version
	// this package does not support. A reader acts on no part of it.
	ErrUnsupportedVersion = errors.New("unsupported version")
	// ErrUnsupportedAlgorithm reports an identity whose algorithm this
	// version does not define.
	ErrUnsupportedAlgorithm = errors.New("unsupported algorithm")
	// ErrBadName reports a name that breaks the rule for names, or a name a
	// directory already has.
	ErrBadName = errors.New("bad name")
)
