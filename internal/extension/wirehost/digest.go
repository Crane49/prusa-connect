package wirehost

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// The refusals a digest returns.
var (
	// ErrDigestFormat reports a recorded digest that is not sha256 and 64 hex
	// characters.
	ErrDigestFormat = errors.New("wirehost: the recorded digest is not sha256:<64 hex>")

	// ErrDigestMismatch reports an artefact whose bytes are not the ones the
	// recorded digest covers. Nothing is executed after this.
	ErrDigestMismatch = errors.New("wirehost: the artefact does not match its recorded digest")
)

// digestPrefix is the one algorithm a recorded digest names.
const digestPrefix = "sha256:"

// digestHexLength is the length of a sha-256 digest written as hex.
const digestHexLength = 2 * sha256.Size

// Digest is the recorded checksum of an installed artefact, written as
// sha256:<64 hex>.
//
// It is verified before every launch and not only at install, so a file swapped
// after installation is refused instead of run. The empty Digest records
// nothing and verifies nothing.
type Digest string

// Verify reads the file at path and reports whether its bytes are the ones the
// digest covers.
func (d Digest) Verify(path string) error {
	want, err := d.recorded()
	if err != nil {
		return err
	}

	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open the artefact: %w", err)
	}

	defer func() {
		_ = file.Close()
	}()

	sum := sha256.New()
	if _, err = io.Copy(sum, file); err != nil {
		return fmt.Errorf("read the artefact: %w", err)
	}

	if got := hex.EncodeToString(sum.Sum(nil)); got != want {
		return fmt.Errorf("%s is %s%s: %w", path, digestPrefix, got, ErrDigestMismatch)
	}

	return nil
}

// recorded returns the hex part of the digest, and refuses anything the format
// does not allow.
func (d Digest) recorded() (string, error) {
	rest, found := strings.CutPrefix(string(d), digestPrefix)
	if !found || len(rest) != digestHexLength {
		return "", fmt.Errorf("%q: %w", string(d), ErrDigestFormat)
	}

	if _, err := hex.DecodeString(rest); err != nil {
		return "", fmt.Errorf("%q: %w", string(d), ErrDigestFormat)
	}

	return strings.ToLower(rest), nil
}
