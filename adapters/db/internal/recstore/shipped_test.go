package recstore_test

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
		"migrations/sqlite/0001_schema.sql":   "546e5ff50eb1141add77d83419ee43eec2118248e641da9ca22154393b9bd7db",
		"migrations/postgres/0001_schema.sql": "68dea5e644cb07e4fa34ce9c850a0b9a857b2f17a6fa8d30ce481ee770ede605",
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
