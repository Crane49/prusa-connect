package sqlstore_test

import (
	"strconv"
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/internal/kernel/sqlstore"
)

// The cases cover the shapes the stores build: no parameter, one, a row of
// them, and the multi-row insert that takes the count past nine.
func TestRenumberWritesTheNumbersPostgresExpects(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		query string
		want  string
	}{
		{
			name:  "no placeholder is returned as it stands",
			query: `SELECT name FROM schema_migration`,
			want:  `SELECT name FROM schema_migration`,
		},
		{
			name:  "one placeholder",
			query: `SELECT checksum FROM history_migration WHERE name = ?`,
			want:  `SELECT checksum FROM history_migration WHERE name = $1`,
		},
		{
			name:  "a row of them counts up",
			query: `INSERT INTO module_state_migration (name, checksum, applied_at) VALUES (?, ?, ?)`,
			want:  `INSERT INTO module_state_migration (name, checksum, applied_at) VALUES ($1, $2, $3)`,
		},
		{
			name:  "past nine the number is two digits",
			query: `VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
			want:  `VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		},
	}

	for _, each := range cases {
		t.Run(each.name, func(t *testing.T) {
			t.Parallel()

			if got := sqlstore.Renumber(each.query); got != each.want {
				t.Errorf("Renumber(%q) = %q, want %q", each.query, got, each.want)
			}
		})
	}
}

// The rewrite touches one byte only, so a statement carrying a non-ASCII
// literal comes back with that literal intact.
func TestRenumberLeavesTextThatIsNotAPlaceholderAlone(t *testing.T) {
	t.Parallel()

	const query = `SELECT id FROM t WHERE label = ? AND note = 'přenos'`

	want := `SELECT id FROM t WHERE label = $1 AND note = 'přenos'`
	if got := sqlstore.Renumber(query); got != want {
		t.Errorf("Renumber(%q) = %q, want %q", query, got, want)
	}
}

// The reserved slack is sized for this case: the multi-row VALUES list
// internal/authz builds for an access projection, where the last number is
// three digits wide.
func TestRenumberCountsEveryPlaceholderInABatch(t *testing.T) {
	t.Parallel()

	const params = 400

	query := "INSERT INTO resource_access VALUES " + strings.Repeat("(?),", params-1) + "(?)"

	got := sqlstore.Renumber(query)

	if want := "$" + strconv.Itoa(params) + ")"; !strings.HasSuffix(got, want) {
		t.Errorf("Renumber() over %d placeholders ends %q, want it to end %q", params, got[len(got)-8:], want)
	}

	if n := strings.Count(got, "$"); n != params {
		t.Errorf("Renumber() wrote %d numbered placeholders, want %d", n, params)
	}
}
