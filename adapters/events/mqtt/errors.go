package mqtt

import "errors"

// ErrMissingURL rejects a spec with no broker.
var ErrMissingURL = errors.New("mqtt: config.url is required")

// Route compilation failures that carry no parameters.
var (
	errEmptyFrom         = errors.New("'from' must not be empty")
	errEmptyPlaceholder  = errors.New("empty placeholder {}, a placeholder must be named")
	errMultiLevelRewrite = errors.New(
		"'to' cannot be combined with #: a multi-level wildcard has no fixed shape to substitute")
)
