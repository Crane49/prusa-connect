package authz

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

// BenchmarkCan measures the point check on the fleet fixture: one indexed row
// lookup in the projection, for a user who reaches the printer through a team
// and two levels of printer group.
func BenchmarkCan(b *testing.B) {
	for _, e := range benchEngines(b) {
		b.Run(e.Name, func(b *testing.B) {
			store := seedFleet(b, e)
			who := subjectOf(b, fleetTeamUser)
			object := deviceOf(b, fleetDeviceID(0))
			ctx := b.Context()

			for b.Loop() {
				allowed, err := store.Can(ctx, who, ActionOperate, object)
				if err != nil || !allowed {
					require.NoError(b, err)
					require.True(b, allowed)
				}
			}
		})
	}
}

// BenchmarkCanDenied measures the refusal, which is the other half of the hot
// path: the same index probe, returning nothing.
func BenchmarkCanDenied(b *testing.B) {
	for _, e := range benchEngines(b) {
		b.Run(e.Name, func(b *testing.B) {
			store := seedFleet(b, e)
			who := subjectOf(b, fleetTeamUser)
			object := deviceOf(b, fleetDeviceID(1))
			ctx := b.Context()

			for b.Loop() {
				allowed, err := store.Can(ctx, who, ActionOperate, object)
				if err != nil || allowed {
					require.NoError(b, err)
					require.False(b, allowed)
				}
			}
		})
	}
}

// BenchmarkListPrintersFleetAdmin measures the query the printer list depends
// on: page one of fifty, sorted by name, joined to the directory, for the
// principal who can see all four hundred printers.
func BenchmarkListPrintersFleetAdmin(b *testing.B) {
	benchmarkList(b, fleetOrgAdmin, ActionObserve, defaultPageLimit, defaultPageLimit)
}

// BenchmarkListPrintersTeamUser measures the same query for a principal with
// forty visible printers, the common case.
func BenchmarkListPrintersTeamUser(b *testing.B) {
	benchmarkList(b, fleetTeamUser, ActionObserve, defaultPageLimit, fleetTeamPrinters)
}

// BenchmarkListPrintersLastPage measures the deepest offset of the fleet, where
// an offset scan costs the most.
func BenchmarkListPrintersLastPage(b *testing.B) {
	for _, e := range benchEngines(b) {
		b.Run(e.Name, func(b *testing.B) {
			store := seedFleet(b, e)
			who := subjectOf(b, fleetOrgAdmin)
			page := Page{Limit: defaultPageLimit, Offset: fleetDevices - defaultPageLimit}
			ctx := b.Context()

			for b.Loop() {
				rows, err := store.ListDevices(ctx, who, ActionObserve, page)
				if err != nil || len(rows) != defaultPageLimit {
					require.NoError(b, err)
					require.Len(b, rows, defaultPageLimit)
				}
			}
		})
	}
}

// BenchmarkCountPrinters measures the total a paginated response carries.
func BenchmarkCountPrinters(b *testing.B) {
	for _, e := range benchEngines(b) {
		b.Run(e.Name, func(b *testing.B) {
			store := seedFleet(b, e)
			who := subjectOf(b, fleetOrgAdmin)
			ctx := b.Context()

			for b.Loop() {
				total, err := store.CountDevices(ctx, who, ActionObserve)
				if err != nil || total != fleetDevices {
					require.NoError(b, err)
					require.Equal(b, fleetDevices, total)
				}
			}
		})
	}
}

// BenchmarkRebuild measures the write path: recompiling the whole projection
// from the tuples, which is what every grant and revoke pays.
func BenchmarkRebuild(b *testing.B) {
	for _, e := range benchEngines(b) {
		b.Run(e.Name, func(b *testing.B) {
			store := seedFleet(b, e)
			ctx := b.Context()

			for b.Loop() {
				require.NoError(b, store.Rebuild(ctx))
			}
		})
	}
}

// BenchmarkProject measures the compiler alone, with no database in the way.
func BenchmarkProject(b *testing.B) {
	tuples := mustTuples(b, fleetTuples()...)

	printers := make([]string, 0, fleetDevices)
	for _, row := range fleetDirectory() {
		printers = append(printers, row.ID)
	}

	b.ReportMetric(float64(len(tuples)), "tuples")

	var rows int

	for b.Loop() {
		rows = len(project(tuples, printerDirectory(printers...), configurableKinds(), verbIndex{}).Devices)
	}

	b.ReportMetric(float64(rows), "rows")
}

// arrivalFleets are the directories one arrival is priced against: an empty
// one, two bare directories of placed printers, and the measured fleet fixture,
// whose grant graph compiles to 9,247 projected rows.
//
// The two bare directories differ only in size, so the pair is the curve the
// gate reads: one arrival must cost the same on four times the fleet. The
// figure rose with the directory until the recompile was scoped to the objects
// a write moves.
var arrivalFleets = []struct {
	name  string
	build func(testing.TB, engine) *Store
}{
	{"empty", func(tb testing.TB, e engine) *Store { return arrivalStore(tb, e, 0) }},
	{"placed=400", func(tb testing.TB, e engine) *Store { return arrivalStore(tb, e, fleetDevices) }},
	{"placed=1600", func(tb testing.TB, e engine) *Store { return arrivalStore(tb, e, 4*fleetDevices) }},
	{"fleet=400", seedFleet},
}

// BenchmarkDeviceArrival prices one printer dialling in: the directory row and
// the org placement, which is what internal/app/devices.go register writes.
//
// The two cases are the same two writes in one transaction and in two. Each
// mutation recompiles, moves the epoch and commits, so the split shape pays for
// all three twice per machine, and the difference is what RegisterDevice exists
// for. Read it beside BenchmarkRebuild, which is the whole recompile on its own.
//
// A fleet arriving after a restart pays the figure below once per machine, and
// the writes serialise because SQLite has one writer. That is the reason the
// figure must not rise with the directory: four hundred machines coming back
// are four hundred of it, one after another.
func BenchmarkDeviceArrival(b *testing.B) {
	for _, e := range benchEngines(b) {
		for _, fleet := range arrivalFleets {
			b.Run(fmt.Sprintf("%s/%s", e.Name, fleet.name), func(b *testing.B) {
				for _, shape := range []struct {
					name  string
					admit func(testing.TB, *Store, Subject, string)
				}{
					{"one_transaction", admitOne},
					{"two_transactions", admitSeparately},
				} {
					b.Run(shape.name, func(b *testing.B) {
						store := fleet.build(b, e)
						tenant := subjectOf(b, "org:acme")

						arrival := 0

						for b.Loop() {
							id := fmt.Sprintf("q%05d", arrival)
							arrival++

							shape.admit(b, store, tenant, id)
						}
					})
				}
			})
		}
	}
}

// BenchmarkVisible prices the read behind a re-authorization: the whole set one
// principal may observe, which every open subscription recomputes when the
// epoch moves. Every arrival moves it, so this is paid once per subscriber per
// coalescing window for as long as the fleet is arriving. How long that is, is
// BenchmarkDeviceArrival times the fleet.
func BenchmarkVisible(b *testing.B) {
	for _, e := range benchEngines(b) {
		b.Run(e.Name, func(b *testing.B) {
			store := seedFleet(b, e)
			who := subjectOf(b, fleetOrgAdmin)
			ctx := b.Context()

			for b.Loop() {
				answer, err := store.Visible(ctx, who)
				if err != nil || len(answer.Devices) != fleetDevices {
					require.NoError(b, err)
					require.Len(b, answer.Devices, fleetDevices)
				}
			}
		})
	}
}

// arrivalStore is a directory already holding this many printers, placed in one
// org, which is the shape the device plane leaves behind.
func arrivalStore(tb testing.TB, e engine, devices int) *Store {
	tb.Helper()

	store := newStore(tb, e)

	if devices == 0 {
		return store
	}

	rows := make([]DeviceRow, 0, devices)
	placements := make([]string, 0, devices)

	for i := range devices {
		id := fleetDeviceID(i)
		rows = append(rows, DeviceRow{ID: id, Name: id})
		placements = append(placements, fmt.Sprintf("device:%s#org@org:acme", id))
	}

	require.NoError(tb, store.UpsertDevices(tb.Context(), rows...))
	require.NoError(tb, store.Write(tb.Context(), mustTuples(tb, placements...)...))

	return store
}

// admitOne is what register writes for one arriving printer: the directory row
// and its placement, in one transaction.
func admitOne(tb testing.TB, store *Store, tenant Subject, id string) {
	tb.Helper()

	require.NoError(tb, store.RegisterDevice(tb.Context(),
		DeviceRow{ID: id, Name: id}, placementOf(tb, tenant, id)))
}

// admitSeparately is the same two writes as two mutations, which is two
// projection recompiles. It is here as the number RegisterDevice is measured
// against.
func admitSeparately(tb testing.TB, store *Store, tenant Subject, id string) {
	tb.Helper()

	require.NoError(tb, store.UpsertDevice(tb.Context(), id, id))
	require.NoError(tb, store.Write(tb.Context(), placementOf(tb, tenant, id)))
}

// placementOf is the org tuple an arriving device is placed by.
func placementOf(tb testing.TB, tenant Subject, id string) Tuple {
	tb.Helper()

	object, err := NewObject(TypeDevice, id)
	require.NoError(tb, err)

	placement, err := NewTuple(object, RelOrg, tenant)
	require.NoError(tb, err)

	return placement
}

func benchmarkList(b *testing.B, who string, action Action, limit, want int) {
	b.Helper()

	for _, e := range benchEngines(b) {
		b.Run(e.Name, func(b *testing.B) {
			store := seedFleet(b, e)
			subject := subjectOf(b, who)
			page := Page{Limit: limit}
			ctx := b.Context()

			for b.Loop() {
				rows, err := store.ListDevices(ctx, subject, action, page)
				if err != nil || len(rows) != want {
					require.NoError(b, err)
					require.Len(b, rows, want)
				}
			}
		})
	}
}

// BenchmarkDriverFloor measures a query that touches no table, so the numbers
// above can be read against the cost of one call through database/sql and the
// driver. On SQLite that floor is the statement preparation this package pays
// per call; on Postgres it is the round trip.
//
// Worth reading beside BenchmarkCan rather than alone: on one machine the
// pure-Go driver's floor is lower than the cgo driver's (3.9 µs against 4.3 µs)
// while its point check is dearer (15.2 µs against 11.1 µs), so the cost is in
// executing the statement and not in getting to it. A prepared-statement cache
// would buy less than the gap suggests.
func BenchmarkDriverFloor(b *testing.B) {
	for _, e := range benchEngines(b) {
		b.Run(e.Name, func(b *testing.B) {
			store := newStore(b, e)
			ctx := b.Context()

			for b.Loop() {
				var one int
				if err := store.db.QueryRowContext(ctx, "SELECT 1").Scan(&one); err != nil {
					require.NoError(b, err)
				}
			}
		})
	}
}

// BenchmarkWriteAndRevokeGrant measures what a grant costs: the insert, the
// epoch bump, and the projection recompute the two of them force, all in one
// transaction. One iteration is two mutations, a grant and its revocation,
// because writing the same tuple twice is a no-op that recompiles nothing. The
// grant names one device, so both mutations take the scoped recompile;
// BenchmarkOrgGrantWriteAndWithdraw is the same pair on an object that moves
// every device in the fleet and therefore takes the whole one.
func BenchmarkWriteAndRevokeGrant(b *testing.B) {
	for _, e := range benchEngines(b) {
		b.Run(e.Name, func(b *testing.B) {
			store := seedFleet(b, e)
			ctx := b.Context()

			grant := mustTuple(b, "device:p000#viewer@user:u199")
			revoke := []Tuple{grant}

			for b.Loop() {
				require.NoError(b, store.Write(ctx, grant))
				require.NoError(b, store.Delete(ctx, revoke...))
			}
		})
	}
}

// BenchmarkGrantSurfaceWriteAndWithdraw measures the same two mutations taken
// through the surface a caller reaches: everything above, plus the permission
// read and the two containment rules, which read the tuple table once more and
// compile the one object twice. The difference between the two benchmarks is
// what bounding a grant costs, and it is the number to weigh before making the
// rules cheaper than they are correct.
func BenchmarkGrantSurfaceWriteAndWithdraw(b *testing.B) {
	for _, e := range benchEngines(b) {
		b.Run(e.Name, func(b *testing.B) {
			store := seedFleet(b, e)
			ctx := b.Context()

			admin := subjectOf(b, fleetOrgAdmin)
			grant := mustTuple(b, "device:p000#viewer@user:u199")

			for b.Loop() {
				if _, err := store.WriteGrant(ctx, admin, grant); err != nil {
					require.NoError(b, err)
				}

				if err := store.WithdrawGrant(ctx, admin, grant); err != nil {
					require.NoError(b, err)
				}
			}
		})
	}
}
