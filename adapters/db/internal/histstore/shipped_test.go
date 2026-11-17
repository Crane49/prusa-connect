package histstore_test

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"testing"
)

// A database a published release made records the checksum of each file it
// applied, and a build that ships the file with another checksum refuses that
// database. A comment edited after a release is enough to do it, so the
// files a release shipped are pinned to the checksums it recorded.
func TestTheShippedMigrationsAreTheReleasedFiles(t *testing.T) {
	t.Parallel()

	shipped := map[string]string{
		"migrations/sqlite/0001_schema.sql":   "7726b7fe97fd005ad0f252d48558b32c6c73e6ab026652d71b58541eb1360d4e",
		"migrations/postgres/0001_schema.sql": "38d2179f25ae24820ae3f5c8e1308a2a5346b1804064caf079b585149ef633fe",
	}

	for file, want := range shipped {
		body, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}

		sum := sha256.Sum256(body)
		if got := hex.EncodeToString(sum[:]); got != want {
			t.Errorf("%s has the checksum %s and v0.0.1-alpha.6 recorded %s. A shipped migration is never "+
				"edited, not even a comment: put the change in a new migration", file, got, want)
		}
	}
}
