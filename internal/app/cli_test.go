package app

import (
	"bytes"
	"database/sql"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/builtin"
	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/kernel/admission"
	"github.com/prusa3d/prusa-connect/internal/kernel/sqlstore"
	"github.com/prusa3d/prusa-connect/internal/platform/auth"
	"github.com/prusa3d/prusa-connect/internal/platform/auth/dbstore"
	"github.com/prusa3d/prusa-connect/internal/platform/devicestore"
)

// cliConfig writes an operator's file for a self-hosted deployment and returns
// its path. It is the file a headless box has: a database, a principal and the
// password login, and nothing else.
func cliConfig(t *testing.T) (string, config.Core) {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	body := strings.Join([]string{
		"core:",
		"  principal: operator",
		"  authz:",
		"    database: '" + filepath.Join(dir, "authz.db") + "'",
		"  overlay: '" + filepath.Join(dir, "resources.json") + "'",
		"  auth:",
		"    local: true",
		"",
	}, "\n")

	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))

	core, err := config.Load(path)
	require.NoError(t, err)

	return path, core
}

// TestTheBootstrapCommandCreatesTheFirstAdministrator is finding CL-05: before
// this there was no way to create the first administrator except through the
// running server's setup path, which is open to whoever reaches it first.
func TestTheBootstrapCommandCreatesTheFirstAdministrator(t *testing.T) {
	t.Parallel()

	path, core := cliConfig(t)

	var out bytes.Buffer

	require.NoError(t, Admin(t.Context(), &out,
		[]string{"create", "--config", path, "--name", "ada@example.com", "--password", testPassword}))
	require.Contains(t, out.String(), "first administrator")

	// The server starts against the same file and finds an administrator, so
	// the first-run path is closed before anything is served.
	host := testHostAt(t, core.Authz.Database)

	local, verifier := wiredAccountsOn(t, core, host)

	info, err := local.Info(t.Context())
	require.NoError(t, err)
	require.False(t, info.NeedsSetup, "the command created an account and the setup path is still open")

	// And the person it created can log in and is the principal the graph's
	// first grant was written for, which is what makes them an administrator
	// rather than somebody with a password and no reach.
	session, err := local.Login(t.Context(), "ada@example.com", testPassword)
	require.NoError(t, err)

	cred, err := verifier.Verify(t.Context(), auth.Token(session.Token), auth.TransportHTTP)
	require.NoError(t, err)
	require.Equal(t, core.Principal, cred.Subject)
}

// A second create is an ordinary account. Only the first one takes the
// principal the graph bootstrapped.
func TestASecondCreateIsAnOrdinaryAccount(t *testing.T) {
	t.Parallel()

	path, _ := cliConfig(t)

	var out bytes.Buffer

	require.NoError(t, Admin(t.Context(), &out,
		[]string{"create", "--config", path, "--name", "ada@example.com", "--password", testPassword}))

	out.Reset()
	require.NoError(t, Admin(t.Context(), &out,
		[]string{"create", "--config", path, "--name", "grace@example.com", "--password", testPassword}))
	require.Contains(t, out.String(), "holds no grant yet")

	out.Reset()
	require.NoError(t, Admin(t.Context(), &out, []string{"list", "--config", path}))
	require.Contains(t, out.String(), "ada@example.com")
	require.Contains(t, out.String(), "grace@example.com")
	require.NotContains(t, out.String(), testPassword, "the listing printed a password")
	require.NotContains(t, out.String(), "argon2", "the listing printed a hash")
}

// Somebody who has locked themselves out gets back in from a shell on the
// machine, which is the only credential this command can ask for.
func TestResetPasswordReplacesAPasswordFromTheShell(t *testing.T) {
	t.Parallel()

	path, core := cliConfig(t)

	var out bytes.Buffer

	require.NoError(t, Admin(t.Context(), &out,
		[]string{"create", "--config", path, "--name", "ada@example.com", "--password", testPassword}))

	const replaced = "a replaced password"

	out.Reset()
	// The username is folded, so the case somebody types is not a second
	// account and not a miss.
	require.NoError(t, Admin(t.Context(), &out,
		[]string{"reset-password", "--config", path, "--name", "ADA@example.com", "--password", replaced}))

	local, _ := wiredAccountsOn(t, core, testHostAt(t, core.Authz.Database))

	_, err := local.Login(t.Context(), "ada@example.com", replaced)
	require.NoError(t, err, "the replaced password does not log in")

	_, err = local.Login(t.Context(), "ada@example.com", testPassword)
	require.True(t, auth.Refused(err), "the old password still logs in")
}

// A reset from the shell is the lost-or-leaked password case, so it ends every
// session and token the person holds, and says how many. The server may be
// running while it does, and that server refuses them at their next request.
func TestResetPasswordEndsTheSessionsAndTokensOfTheAccount(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	running := start(t, dir, bearerStreamConfig(dir))

	owner := signUpTheOwner(t, running)
	script := mintOver(t, running, owner, `{"name":"script","scopes":["administer"]}`)
	service := mintOver(t, running, owner, `{"name":"ci","scopes":["administer"],"subject":"service_account:ci"}`)

	paired := send(t, running, http.MethodPost, "/api/v1/auth/pairings",
		`{"provider":"prusa-account","subject":"someone-else","principal":"user:operator"}`, owner)
	require.Equal(t, http.StatusCreated, paired.status, paired.body)

	var out bytes.Buffer

	require.NoError(t, Admin(t.Context(), &out, []string{
		"reset-password", "--config", filepath.Join(dir, "config.yaml"),
		"--name", testOwner, "--password", "a replaced password",
	}))
	require.Contains(t, out.String(), "1 session and 1 api credential")
	require.Contains(t, out.String(),
		"1 api credential it created for somebody else, directly or down a chain it started, ended too.")
	require.Contains(t, out.String(), "1 pairing to it, written by it or down a chain it started, ended too.")
	require.Contains(t, out.String(), "Review the accounts and grants")
	require.NotContains(t, out.String(), "name nobody as their creator",
		"the reset warned about older rows on a server that holds none")

	requireRefusedOver(t, running, "the session cookie", owner)
	requireRefusedOver(t, running, "the person's own token", bearerOf(script))
	requireRefusedOver(t, running, "the token the person minted for a service account", bearerOf(service))

	logInOver(t, running, testOwner, "a replaced password")
}

// The account commands refuse a command line they cannot act on, and say so as
// a usage error rather than as a failure, so a script can tell a typo from an
// outage.
func TestTheAccountCommandsRefuseALineTheyCannotActOn(t *testing.T) {
	t.Parallel()

	path, _ := cliConfig(t)

	var out bytes.Buffer

	for _, args := range [][]string{
		{},
		{"frobnicate", "--config", path},
		{"create", "--config", path, "--name", "ada@example.com"},
		{"reset-password", "--config", path, "--password", testPassword},
	} {
		out.Reset()
		require.ErrorIs(t, Admin(t.Context(), &out, args), ErrUsage, "args %v", args)
	}

	// A person nobody holds is a failure and not a usage error: the line was
	// well formed and the answer is no.
	err := Admin(t.Context(), &out,
		[]string{"reset-password", "--config", path, "--name", "nobody", "--password", testPassword})
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrUsage)
}

// The registration commands are what a headless box has instead of a browser.
func TestTheRegistrationCommandsListApproveAndReject(t *testing.T) {
	t.Parallel()

	path, core := cliConfig(t)

	host := testHostAt(t, core.Authz.Database)

	store, err := devicestore.NewRegistrations(host.db, host.engine, nil)
	require.NoError(t, err)

	// Relative to the clock the listing reads, because the word the row carries
	// is decided against it: a fixed date is a registration that reads waiting
	// until the day it expires and expired for ever afterwards.
	issued := time.Now().UTC().Add(-time.Hour)

	require.NoError(t, store.Save(t.Context(), admission.Record{
		Connector:   string(builtin.ShippingConnector()),
		Code:        "ACDEF23456",
		Fingerprint: "FP-1",
		Serial:      "SN-1",
		Model:       "MK4",
		Kind:        "printer",
		IssuedAt:    issued,
		ExpiresAt:   issued.Add(24 * time.Hour),
		Seq:         1,
	}))

	var out bytes.Buffer

	require.NoError(t, Registrations(t.Context(), &out, []string{"list", "--config", path}))
	require.Contains(t, out.String(), "ACDEF23456")
	require.Contains(t, out.String(), "waiting")

	out.Reset()
	require.NoError(t, Registrations(t.Context(), &out,
		[]string{"approve", "--config", path, "--by", "user:ada", "acdef23456"}))
	require.Contains(t, out.String(), "approved ACDEF23456")

	rows, err := store.Load(t.Context(), string(builtin.ShippingConnector()))
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.True(t, rows[0].Approved)
	require.Equal(t, "user:ada", rows[0].AdmittedBy)
	require.True(t, rows[0].ExpiresAt.IsZero(), "an approved registration still expires")

	out.Reset()
	require.NoError(t, Registrations(t.Context(), &out,
		[]string{"reject", "--config", path, "ACDEF23456"}))

	rows, err = store.Load(t.Context(), string(builtin.ShippingConnector()))
	require.NoError(t, err)
	require.Empty(t, rows)

	out.Reset()
	require.NoError(t, Registrations(t.Context(), &out, []string{"list", "--config", path}))
	require.Contains(t, out.String(), "no device is registered")

	require.ErrorIs(t, Registrations(t.Context(), &out, nil), ErrUsage)
	require.ErrorIs(t, Registrations(t.Context(), &out, []string{"approve", "--config", path}), ErrUsage)
}

// A kept demo directory gets a new password on every run. The sessions and
// tokens the last run handed out end with it, as on every other password path.
func TestTheDemoPasswordReplacementEndsTheOldCredentials(t *testing.T) {
	t.Parallel()

	path, core := cliConfig(t)

	_, err := demoAccount(t.Context(), path, DemoOptions{Password: testPassword})
	require.NoError(t, err)

	local, verifier := wiredAccountsOn(t, core, testHostAt(t, core.Authz.Database))

	opened, err := local.Login(t.Context(), demoUsername, testPassword)
	require.NoError(t, err)

	_, err = demoAccount(t.Context(), path, DemoOptions{Password: "the next run's password"})
	require.NoError(t, err)

	_, err = verifier.Verify(t.Context(), auth.Token(opened.Token), auth.TransportHTTP)
	require.True(t, auth.Refused(err), "a session from the last demo run survived the new password")
}

// The shell reset runs against a database a server may have open, so it never
// migrates one. A database at an older schema is the normal state right after
// an upgrade, and the reset says to start the new server once first. A
// database at a newer schema belongs to a newer build, and the reset says to
// use the server's version.
func TestResetPasswordRefusesADatabaseOfAnotherVersion(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		change func(t *testing.T, db *sql.DB)
		want   error
		says   string
	}{
		{
			name: "an older schema",
			change: func(t *testing.T, db *sql.DB) {
				t.Helper()

				_, err := db.ExecContext(t.Context(), `DELETE FROM schema_migration WHERE name = ?`,
					newestMigration(t, db))
				require.NoError(t, err)
			},
			want: sqlstore.ErrSchemaOlder,
			says: "start the new server once",
		},
		{
			name: "a newer schema",
			change: func(t *testing.T, db *sql.DB) {
				t.Helper()

				_, err := db.ExecContext(t.Context(),
					`INSERT INTO schema_migration (name, checksum, binary_version, applied_at) VALUES (?, ?, ?, ?)`,
					"authz/9999_from_a_newer_build.sql", "", "9.9.9", time.Now().UTC())
				require.NoError(t, err)
			},
			want: sqlstore.ErrSchemaNewer,
			says: "same version as the server",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			path, core := cliConfig(t)

			var out bytes.Buffer

			require.NoError(t, Admin(t.Context(), &out,
				[]string{"create", "--config", path, "--name", "ada@example.com", "--password", testPassword}))

			db, err := authz.OpenSQLite(t.Context(), core.Authz.Database)
			require.NoError(t, err)

			t.Cleanup(func() { _ = db.Close() })

			tc.change(t, db)

			var before int

			require.NoError(t, db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM schema_migration`).Scan(&before))

			err = Admin(t.Context(), &out, []string{
				"reset-password", "--config", path, "--name", "ada@example.com", "--password", "a replaced password",
			})
			require.ErrorIs(t, err, tc.want)
			require.ErrorContains(t, err, tc.says)

			var after int

			require.NoError(t, db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM schema_migration`).Scan(&after))
			require.Equal(t, before, after, "the reset migrated the database it refused")
		})
	}
}

// newestMigration is the last migration the ledger recorded.
func newestMigration(t *testing.T, db *sql.DB) string {
	t.Helper()

	var name string

	require.NoError(t, db.QueryRowContext(t.Context(),
		`SELECT name FROM schema_migration ORDER BY name DESC LIMIT 1`).Scan(&name))

	return name
}

// A server that holds a service account token or a pairing from before the
// creator was recorded gets told, with the counts and the version, because a
// reset cannot end those.
func TestResetPasswordNamesTheOlderRowsItCannotEnd(t *testing.T) {
	t.Parallel()

	path, core := cliConfig(t)

	var out bytes.Buffer

	require.NoError(t, Admin(t.Context(), &out,
		[]string{"create", "--config", path, "--name", "ada@example.com", "--password", testPassword}))

	host := testHostAt(t, core.Authz.Database)

	store, err := dbstore.NewCredentials(host.db, host.engine)
	require.NoError(t, err)

	pairings, err := dbstore.NewPairings(host.db, host.engine)
	require.NoError(t, err)

	now := time.Now()

	_, _, err = auth.IssueAPI(
		t.Context(),
		store,
		auth.APISpec{Scopes: []string{"administer"}, Subject: "service_account:ci", Name: "old"},
		now,
	)
	require.NoError(t, err)
	_, _, err = auth.IssueAPI(
		t.Context(),
		store,
		auth.APISpec{Scopes: []string{"administer"}, Subject: "user:bob", Name: "made for bob"},
		now,
	)
	require.NoError(t, err)
	require.NoError(t, pairings.Save(t.Context(), auth.Pairing{
		Provider: "prusa-account", Subject: "old", Principal: "service_account:ci", CreatedAt: now,
	}))

	out.Reset()
	require.NoError(t, Admin(t.Context(), &out, []string{
		"reset-password", "--config", path, "--name", "ada@example.com", "--password", "a replaced password",
	}))
	require.Contains(t, out.String(), "2 api credentials and 1 pairing were written by version "+
		creatorRecordedSince+" or earlier and name nobody as their creator")
}
