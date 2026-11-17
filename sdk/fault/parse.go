package fault

// ParseCode reads a code from its domain/REASON text, applying the grammar
// NewCode applies. It reports ErrInvalidCode, ErrInvalidDomain or
// ErrInvalidReason. It does not consult the registry. Registered does.
func ParseCode(text string) (Code, error) { return parseCode(text) }
