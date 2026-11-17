package config

import (
	"fmt"
	"strings"
)

// The bounds on a setup code an operator writes into core.auth.setup_code,
// counted after NormalSetupCode. Twelve is the length of the code the server
// makes itself. A code of your own may still be weaker than a made one, so
// it also needs a mix of characters: `aaaaaaaaaaaa` has the length and almost
// nothing to guess.
const (
	MinSetupCodeLength   = 12
	MaxSetupCodeLength   = 64
	MinSetupCodeDistinct = 6
)

// NormalSetupCode is the form two setup codes are compared in. It drops the
// hyphens and spaces a person types between groups, reads lower case as upper
// case, and reads O as zero and I and L as one. The code the server makes
// holds none of those three letters, so a person who misreads one still types
// the right code.
func NormalSetupCode(code string) string {
	var out strings.Builder

	out.Grow(len(code))

	for _, r := range strings.ToUpper(code) {
		switch r {
		case '-', ' ':
			continue
		case 'O':
			r = '0'
		case 'I', 'L':
			r = '1'
		}

		out.WriteRune(r)
	}

	return out.String()
}

// validSetupCode refuses a code a person could not type or that is too short
// to guess slowly. The message names the length and never the code, because
// the error reaches the log.
func validSetupCode(code string) error {
	if code == "" {
		return nil
	}

	// The raw value is checked before it is normalised, because upper-casing
	// turns some letters outside ASCII into ASCII ones: ſ becomes S.
	for _, r := range code {
		if (r < '!' || r > '~') && r != ' ' {
			return fmt.Errorf("%w: core.auth.setup_code holds a character that is not a printable ASCII "+
				"letter, digit or sign, and a person has to type it", ErrInvalid)
		}
	}

	normal := NormalSetupCode(code)

	distinct := make(map[rune]bool)
	for _, r := range normal {
		distinct[r] = true
	}

	if len(distinct) < MinSetupCodeDistinct {
		return fmt.Errorf("%w: core.auth.setup_code has %d different characters without its hyphens and "+
			"spaces, expected at least %d, so it is too easy to guess", ErrInvalid, len(distinct), MinSetupCodeDistinct)
	}

	if len(normal) < MinSetupCodeLength || len(normal) > MaxSetupCodeLength {
		return fmt.Errorf("%w: core.auth.setup_code has %d characters without its hyphens and spaces, "+
			"expected %d to %d", ErrInvalid, len(normal), MinSetupCodeLength, MaxSetupCodeLength)
	}

	return nil
}
