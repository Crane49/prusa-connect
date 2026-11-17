package files

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// CheckName returns nil when name is valid UTF-8, not empty, has no "/" and
// no U+0000, and is neither "." nor "..". It does not check Normalization
// Form C. The error wraps ErrBadName.
func CheckName(name string) error {
	switch {
	case name == "":
		return fmt.Errorf("%w: the name is empty", ErrBadName)
	case name == "." || name == "..":
		return fmt.Errorf("%w: %q is not a name", ErrBadName, name)
	case !utf8.ValidString(name):
		return fmt.Errorf("%w: %q is not valid UTF-8", ErrBadName, name)
	case strings.ContainsAny(name, "/\x00"):
		return fmt.Errorf("%w: %q holds a slash or U+0000", ErrBadName, name)
	}
	return nil
}
