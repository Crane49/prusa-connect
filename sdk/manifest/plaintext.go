package manifest

import (
	"fmt"
	"unicode"
)

// PlainText reports whether text is safe to show a person as it stands: no
// control character, no format character such as a bidirectional override or
// a zero-width space, and no line or paragraph separator. A manifest's free
// text reaches a terminal and a web page, where such characters could hide
// or reorder what the person reads.
func PlainText(text string) bool {
	for _, r := range text {
		if unicode.IsControl(r) || unicode.In(r, unicode.Cf, unicode.Zl, unicode.Zp) {
			return false
		}
	}

	return true
}

// plainText refuses a value PlainText does not allow.
func plainText(where, key, value string) error {
	if !PlainText(value) {
		return fmt.Errorf("%w: %s holds a control or invisible character", ErrInvalid, at(where, key))
	}

	return nil
}
