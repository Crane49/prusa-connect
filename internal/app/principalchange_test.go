package app

import (
	"bytes"
	"database/sql"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/app/hostschema"
	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/kernel/sqlstore"
)

// A first start with the default core.principal, then a restart with the
// subject an issuer names, which is what the setup wizard writes. The new
// principal administers the org afterwards: without it a person who signs in
// through the issuer holds no admin role and no call can grant one.
func TestAChangedCorePrincipalAdministersTheOrg(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "authz.db")+"?"+countedPragmas)
	require.NoError(t, err)

	t.Cleanup(func() { require.NoError(t, db.Close()) })

	graph, err := authz.Open(db, authz.DialectSQLite)
	require.NoError(t, err)

	migrator, err := hostschema.Migrator(db, sqlstore.EngineSQLite, "0.0.0-test")
	require.NoError(t, err)
	require.NoError(t, migrator.Migrate(ctx))
	require.NoError(t, graph.Reconcile(ctx))

	first := config.Default()
	require.NoError(t, bootstrapGraph(ctx, first, discard(), graph, "test"))

	second := config.Default()
	second.Principal = "1d77e74b-a4f2-4085-9848-4c942da9e2f1"
	require.NoError(t, bootstrapGraph(ctx, second, discard(), graph, "test"))

	org, err := authz.NewObject(authz.TypeOrg, bootstrapOrg)
	require.NoError(t, err)

	person, err := authz.NewSubject(authz.TypeUser, second.Principal)
	require.NoError(t, err)

	_, err = graph.Grants(ctx, person, org)
	require.NoError(t, err, "the changed core.principal does not administer the org")
}

// An issuer's subject the graph cannot name, such as authentik's default
// hashed id, still administers the org. The grant goes to the id a login with
// that subject resolves to (ADR-20261023-wgv).
func TestASubjectTheGraphCannotNameAdministersTheOrgUnderItsDerivedID(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "authz.db")+"?"+countedPragmas)
	require.NoError(t, err)

	t.Cleanup(func() { require.NoError(t, db.Close()) })

	graph, err := authz.Open(db, authz.DialectSQLite)
	require.NoError(t, err)

	migrator, err := hostschema.Migrator(db, sqlstore.EngineSQLite, "0.0.0-test")
	require.NoError(t, err)
	require.NoError(t, migrator.Migrate(ctx))
	require.NoError(t, graph.Reconcile(ctx))

	core := config.Default()
	core.Auth.Issuer = "http://127.0.0.1:58190/application/o/prusa-connect/"
	core.Auth.ClientID = "prusa-connect"
	core.Principal = strings.Repeat("a", 64)
	require.NoError(t, bootstrapGraph(ctx, core, discard(), graph, "test"))

	org, err := authz.NewObject(authz.TypeOrg, bootstrapOrg)
	require.NoError(t, err)

	// The value is the one oidc.TestPrincipalIDIsStable computes elsewhere.
	person, err := authz.NewSubject(authz.TypeUser, "oidc-wyj7c7bxbvgxunmhhus7ehtwaoxuz7r3")
	require.NoError(t, err)

	_, err = graph.Grants(ctx, person, org)
	require.NoError(t, err, "the derived id does not administer the org")
}

// An issuer added to config.yaml by hand leaves core.principal at its default,
// and nobody at the issuer administers the server. Start-up says so, and says
// nothing once core.principal names a subject.
func TestADefaultCorePrincipalWithAnIssuerIsWarnedAbout(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name      string
		principal string
		warned    bool
	}{
		{name: "default", principal: config.DefaultPrincipal, warned: true},
		{name: "a subject", principal: "ulli@home.lan", warned: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := t.Context()

			db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "authz.db")+"?"+countedPragmas)
			require.NoError(t, err)

			t.Cleanup(func() { require.NoError(t, db.Close()) })

			graph, err := authz.Open(db, authz.DialectSQLite)
			require.NoError(t, err)

			migrator, err := hostschema.Migrator(db, sqlstore.EngineSQLite, "0.0.0-test")
			require.NoError(t, err)
			require.NoError(t, migrator.Migrate(ctx))
			require.NoError(t, graph.Reconcile(ctx))

			core := config.Default()
			core.Auth.Issuer = "https://id.home.lan/"
			core.Auth.ClientID = "prusa-connect"
			core.Principal = tc.principal

			var out bytes.Buffer

			logger := slog.New(slog.NewTextHandler(&out, nil))
			require.NoError(t, bootstrapGraph(ctx, core, logger, graph, "test"))

			require.Equal(t, tc.warned, strings.Contains(out.String(), "core.principal is still the default"),
				"log was:\n%s", out.String())
		})
	}
}
