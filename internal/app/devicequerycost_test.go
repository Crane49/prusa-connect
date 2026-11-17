package app

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/app/hostschema"
	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/kernel/sqlstore"
	"github.com/prusa3d/prusa-connect/internal/platform/devicestore"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/internal/transport/stream"
)

// TestTheFleetListingCostsNoStatementPerPrinter is the measurement, not an
// assertion about the code.
//
// A page that says what is wrong with a fleet is the shape that becomes a
// query per device: read the printers, then ask each one whether it is well.
// Nothing here does that, and the way to know is to count the statements the
// database actually received rather than to read the source and believe it.
//
// The listing walks the authorized directory in windows of fleetScanWindow, so
// the cost is one statement per window and nothing else: an issue is read from
// the entry this process already holds. The bound below is written from the
// window rather than from the observed numbers, so a walk that grew a statement
// per printer fails at every size rather than at the size somebody remembered.
func TestTheFleetListingCostsNoStatementPerPrinter(t *testing.T) {
	t.Parallel()

	for _, fleet := range []int{1, 100, 400} {
		t.Run(fmt.Sprintf("%d printers", fleet), func(t *testing.T) {
			t.Parallel()

			counted, plane := countedPlane(t)

			for i := range fleet {
				session := arriveSpeaking(t, plane, fmt.Sprintf("SIM-%04d", i))

				// A tenth of the fleet is stopped and waiting for somebody, so
				// the walk is doing the work this test is about rather than
				// finding nothing wrong and returning early.
				if i%10 == 0 {
					raiseDialog(t, plane, session, "Filament runout")
				}
			}

			// The window is what bounds the walk: one statement per window, and
			// one more for the window that comes back short. A page's worth of
			// rows is one statement whatever the page is. The verbs a grant
			// gives the caller are one more, read once for every row.
			windows := fleet/fleetScanWindow + 1
			bound := windows + 1

			for _, probe := range []struct {
				name  string
				query httpapi.DeviceQuery
			}{
				{"a page and the rollup", httpapi.DeviceQuery{}},
				{"only the printers with issues", httpapi.DeviceQuery{WithIssues: true}},
				{"the far end of the fleet", httpapi.DeviceQuery{
					Page: httpapi.Page{Limit: 50, Offset: 350},
				}},
			} {
				counted.Store(0)

				page, err := plane.List(t.Context(), fleetOwner, probe.query)
				require.NoError(t, err)

				statements := counted.Load()

				t.Logf("observed: %d printers, %s: %d statements, %d items, %d affected",
					fleet, probe.name, statements, len(page.Items), page.Issues.Affected)

				require.LessOrEqualf(t, statements, int64(bound),
					"%s cost %d statements over %d printers, and the listing is bounded at %d",
					probe.name, statements, fleet, bound)

				require.NotZerof(t, statements, "%s reached the database not at all, so this "+
					"measurement is of nothing", probe.name)
			}

			// Every tenth printer starting at the first, which is a tenth of the
			// fleet rounded up.
			require.Equal(t, (fleet+9)/10, mustList(t, plane).Issues.Affected,
				"the fleet this was measured over is not the fleet that was built")
		})
	}
}

// mustList is the unfiltered page, for the assertion that the fleet under the
// measurement is the one the test set up.
func mustList(t *testing.T, plane *devices) httpapi.DevicePage {
	t.Helper()

	page, err := plane.List(t.Context(), fleetOwner, httpapi.DeviceQuery{})
	require.NoError(t, err)

	return page
}

// countedPlane is the device plane over an authorization graph whose every
// statement is counted.
//
// It builds the graph by hand rather than through openGraph, because openGraph
// opens the handle itself and the counter has to be under it.
func countedPlane(t *testing.T) (*atomic.Int64, *devices) {
	t.Helper()

	counted := &atomic.Int64{}
	path := filepath.Join(t.TempDir(), "authz.db")

	db, err := sql.Open(countingDriver(t, counted), path+"?"+countedPragmas)
	require.NoError(t, err)

	t.Cleanup(func() { require.NoError(t, db.Close()) })

	graph, err := authz.Open(db, authz.DialectSQLite)
	require.NoError(t, err)

	ctx := t.Context()
	migrator, err := hostschema.Migrator(db, sqlstore.EngineSQLite, "0.0.0-test")
	require.NoError(t, err)
	require.NoError(t, migrator.Migrate(ctx))
	require.NoError(t, graph.Reconcile(ctx))

	core := config.Default()
	require.NoError(t, bootstrapGraph(ctx, core, discard(), graph, core.Authz.Database))

	hub, err := stream.New(stream.Options{
		Site: "local", Authorizer: visibility{graph: graph}, Logger: discard(),
	})
	require.NoError(t, err)

	t.Cleanup(hub.Close)

	stored, err := devicestore.New(db, sqlstore.EngineSQLite)
	require.NoError(t, err)

	plane := newDevices(graph, stored, hub, discard(), bootstrapOrg, staging{}, fleetMeters{}, 0)
	t.Cleanup(plane.Close)

	// The record flush is taken off this handle. It is a background writer on a
	// timer of its own, so its statements would land inside whichever window
	// the counter happens to be open over and the measurement would be of two
	// things at once. What the flush costs is measured where it can be measured
	// properly, by BenchmarkDeviceRecordFlush in internal/authz, against both
	// engines and a fleet of four hundred.
	plane.records = quietRecords{}

	return counted, plane
}

// quietRecords is a record store that stores nothing, for a measurement about
// something else. It is not a stub standing in for behaviour under test:
// nothing in this file reads a record back.
type quietRecords struct{}

func (quietRecords) SaveDeviceRecords(context.Context, ...devicestore.DeviceRecord) error { return nil }

func (quietRecords) DeviceRecords(context.Context) ([]devicestore.DeviceRecord, error) {
	return nil, nil
}

func (quietRecords) UnfinishedDeviceRecords(context.Context) (int64, error) { return 0, nil }

func (quietRecords) SaveDeviceJobs(context.Context, ...devicestore.PrinterJob) error { return nil }

func (quietRecords) DeviceJobs(context.Context, string, int) ([]devicestore.PrinterJob, error) {
	return nil, nil
}

func (quietRecords) LatestDeviceJobs(context.Context) (map[string]devicestore.PrinterJob, error) {
	return map[string]devicestore.PrinterJob{}, nil
}

func (quietRecords) PrinterJobByID(context.Context, string) (devicestore.PrinterJob, bool, error) {
	return devicestore.PrinterJob{}, false, nil
}

func (quietRecords) EndPrinterJob(context.Context, string, time.Time, string) error { return nil }

func (quietRecords) EndedJobs(
	context.Context, authz.Subject, devicestore.JobWindow,
) ([]devicestore.PrinterJob, error) {
	return nil, nil
}

func (quietRecords) FleetJobs(
	context.Context, authz.Subject, devicestore.JobFilter,
) ([]devicestore.PrinterJob, error) {
	return nil, nil
}

// countedPragmas is how internal/authz opens SQLite, transcribed because this
// test opens the handle itself. foreign_keys is the load-bearing one: Migrate
// refuses a handle without it.
const countedPragmas = "_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_txlock=immediate"

// drivers is the counting drivers registered so far. database/sql refuses a
// name twice for the length of the process, and every parallel case here needs
// a counter of its own.
var drivers struct {
	mu   sync.Mutex
	next int
}

// countingDriver registers a driver that counts every statement it carries to
// the engine behind it, and returns its name.
//
// It wraps whatever modernc.org/sqlite registered rather than reaching for the
// type, so the measurement is over the engine the product ships on.
func countingDriver(t *testing.T, counted *atomic.Int64) string {
	t.Helper()

	probe, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)

	inner := probe.Driver()
	require.NoError(t, probe.Close())

	drivers.mu.Lock()
	defer drivers.mu.Unlock()

	drivers.next++
	name := fmt.Sprintf("sqlite-counted-%d", drivers.next)

	sql.Register(name, countedDriver{inner: inner, counted: counted})

	return name
}

type countedDriver struct {
	inner   driver.Driver
	counted *atomic.Int64
}

func (d countedDriver) Open(name string) (driver.Conn, error) {
	conn, err := d.inner.Open(name)
	if err != nil {
		return nil, err
	}

	return &countedConn{inner: conn, counted: d.counted}, nil
}

// countedConn counts a statement where it is carried out. Both routes are
// covered: database/sql calls QueryContext when the connection offers one, and
// falls back to preparing a statement when it does not, so counting only one of
// the two would report a listing as free.
type countedConn struct {
	inner   driver.Conn
	counted *atomic.Int64
}

func (c *countedConn) Prepare(query string) (driver.Stmt, error) {
	stmt, err := c.inner.Prepare(query)
	if err != nil {
		return nil, err
	}

	return &countedStmt{inner: stmt, counted: c.counted}, nil
}

func (c *countedConn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	preparer, ok := c.inner.(driver.ConnPrepareContext)
	if !ok {
		return c.Prepare(query)
	}

	stmt, err := preparer.PrepareContext(ctx, query)
	if err != nil {
		return nil, err
	}

	return &countedStmt{inner: stmt, counted: c.counted}, nil
}

func (c *countedConn) Close() error { return c.inner.Close() }

//nolint:staticcheck // the interface database/sql requires.
func (c *countedConn) Begin() (driver.Tx, error) { return c.inner.Begin() }

func (c *countedConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	beginner, ok := c.inner.(driver.ConnBeginTx)
	if !ok {
		return c.Begin()
	}

	return beginner.BeginTx(ctx, opts)
}

func (c *countedConn) QueryContext(
	ctx context.Context, query string, args []driver.NamedValue,
) (driver.Rows, error) {
	queryer, ok := c.inner.(driver.QueryerContext)
	if !ok {
		return nil, driver.ErrSkip
	}

	rows, err := queryer.QueryContext(ctx, query, args)
	if errors.Is(err, driver.ErrSkip) {
		// database/sql will prepare instead, and countedStmt counts it there.
		return nil, err
	}

	c.counted.Add(1)

	return rows, err
}

func (c *countedConn) ExecContext(
	ctx context.Context, query string, args []driver.NamedValue,
) (driver.Result, error) {
	execer, ok := c.inner.(driver.ExecerContext)
	if !ok {
		return nil, driver.ErrSkip
	}

	result, err := execer.ExecContext(ctx, query, args)
	if errors.Is(err, driver.ErrSkip) {
		return nil, err
	}

	c.counted.Add(1)

	return result, err
}

type countedStmt struct {
	inner   driver.Stmt
	counted *atomic.Int64
}

func (s *countedStmt) Close() error { return s.inner.Close() }

func (s *countedStmt) NumInput() int { return s.inner.NumInput() }

// Exec and Query are the deprecated half of driver.Stmt, and database/sql calls
// them when the inner statement offers no context form.
//
//nolint:staticcheck // the interface database/sql requires.
func (s *countedStmt) Exec(args []driver.Value) (driver.Result, error) {
	s.counted.Add(1)

	return s.inner.Exec(args)
}

//nolint:staticcheck // see Exec above.
func (s *countedStmt) Query(args []driver.Value) (driver.Rows, error) {
	s.counted.Add(1)

	return s.inner.Query(args)
}

func (s *countedStmt) ExecContext(
	ctx context.Context, args []driver.NamedValue,
) (driver.Result, error) {
	execer, ok := s.inner.(driver.StmtExecContext)
	if !ok {
		return nil, driver.ErrSkip
	}

	s.counted.Add(1)

	return execer.ExecContext(ctx, args)
}

func (s *countedStmt) QueryContext(
	ctx context.Context, args []driver.NamedValue,
) (driver.Rows, error) {
	queryer, ok := s.inner.(driver.StmtQueryContext)
	if !ok {
		return nil, driver.ErrSkip
	}

	s.counted.Add(1)

	return queryer.QueryContext(ctx, args)
}
