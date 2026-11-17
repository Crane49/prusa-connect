package sqlstore_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/kernel/sqlstore"
)

// An Open that names no pool still gets one. A handle with no bound lets a
// burst open one connection per waiting request.
func TestAnOpenThatNamesNoPoolGetsTheDefaults(t *testing.T) {
	t.Parallel()

	require.Equal(t, sqlstore.DefaultPool(), sqlstore.PoolOr(nil))
	require.Equal(t, sqlstore.DefaultPool(), sqlstore.PoolOr([]sqlstore.Pool{}))

	named := sqlstore.Pool{MaxOpen: 3, MaxIdle: 1, MaxLifetime: time.Minute}
	require.Equal(t, named, sqlstore.PoolOr([]sqlstore.Pool{named}))
}

// The statement timeout reaches the server as a connection setting in the DSN,
// the only place every connection the pool opens reads.
func TestTheStatementTimeoutReachesBothDSNForms(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		dsn     string
		timeout time.Duration
		want    string
	}{
		"key and value": {
			"host=db user=someone dbname=connect", 30 * time.Second,
			"host=db user=someone dbname=connect statement_timeout=30000",
		},
		"a url with no query": {
			"postgres://db:5432/connect", 5 * time.Second,
			"postgres://db:5432/connect?statement_timeout=5000",
		},
		"a url that already has a query": {
			"postgres://db:5432/connect?sslmode=disable", 5 * time.Second,
			"postgres://db:5432/connect?sslmode=disable&statement_timeout=5000",
		},
		// What the operator wrote stands, in whichever form their file uses.
		"a dsn that already names it": {
			"postgres://db:5432/connect?statement_timeout=1000", 5 * time.Second,
			"postgres://db:5432/connect?statement_timeout=1000",
		},
		"no timeout asked for": {
			"postgres://db:5432/connect", 0, "postgres://db:5432/connect",
		},
		"a dsn nothing can parse": {
			"postgres://%%%", 5 * time.Second, "postgres://%%%",
		},
		"nothing at all": {"", 5 * time.Second, ""},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tc.want, sqlstore.WithStatementTimeout(tc.dsn, tc.timeout))
		})
	}
}

// The page cache reaches SQLite as a DSN parameter in KiB, and a pool that
// names none leaves SQLite's own size alone.
func TestThePageCacheBecomesADSNParameterInKiB(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		bytes int
		want  string
	}{
		"none named":       {0, ""},
		"a negative value": {-1, ""},
		"half a MiB":       {512 << 10, "&_pragma=cache_size(-512)"},
		"one byte":         {1, "&_pragma=cache_size(-1)"},
		"not a whole KiB":  {1025, "&_pragma=cache_size(-2)"},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tc.want, sqlstore.Pool{PageCacheBytes: tc.bytes}.PageCachePragma())
		})
	}
}
