package authz

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// listCase is one list and the exact set it owes, in the order the query sorts
// it. An empty want is as much of a test as a full one.
type listCase struct {
	name    string
	subject string
	action  Action
	want    []string
}

var listCases = []listCase{
	{
		name:    "team operator sees its floor, the nested team's printer and the camera inside one",
		subject: "user:alice",
		action:  ActionObserve,
		want:    []string{"p-alpha", "cam-alpha", "p-beta", "p-gamma"},
	},
	{
		name:    "and commands only what it operates",
		subject: "user:alice",
		action:  ActionOperate,
		want:    []string{"p-alpha", "cam-alpha", "p-beta"},
	},
	{
		// bob reaches p-gamma and not p-alpha, so the camera inside p-alpha is
		// not his either: a device inside a device is reached through its
		// parent and through nothing else.
		name:    "outer team member sees one printer and no camera",
		subject: "user:bob",
		action:  ActionObserve,
		want:    []string{"p-gamma"},
	},
	{
		name:    "and commands none",
		subject: "user:bob",
		action:  ActionOperate,
		want:    nil,
	},
	{
		name:    "a direct grant lists one printer",
		subject: "user:dana",
		action:  ActionObserve,
		want:    []string{"p-delta"},
	},
	{
		name:    "an org viewer lists everything except the blocked printer",
		subject: "user:olga",
		action:  ActionObserve,
		want:    []string{"p-alpha", "cam-alpha", "p-beta", "p-delta"},
	},
	{
		name:    "an org viewer commands nothing",
		subject: "user:olga",
		action:  ActionOperate,
		want:    nil,
	},
	{
		// The camera carries no org tuple, so the only thing that puts it here
		// is the edge to a printer the org does place.
		name:    "an org admin manages the whole fleet",
		subject: "user:root",
		action:  ActionAdminister,
		want:    []string{"p-alpha", "cam-alpha", "p-beta", "p-delta", "p-gamma"},
	},
	{
		name:    "a service account lists its three printers and one camera",
		subject: "service_account:sa-ui",
		action:  ActionOperate,
		want:    []string{"p-alpha", "cam-alpha", "p-beta", "p-delta"},
	},
	{
		// A device reaches the device it is the agent of and nothing else, so
		// the camera inside that device is not on this list either.
		name:    "an agent lists its own printer and not the camera inside it",
		subject: "device_credential:sn-alpha",
		action:  ActionObserve,
		want:    []string{"p-alpha"},
	},
	{
		name:    "an agent commands nothing",
		subject: "device_credential:sn-alpha",
		action:  ActionOperate,
		want:    nil,
	},
	{
		name:    "a device with no agent tuple lists nothing",
		subject: "device_credential:sn-gamma",
		action:  ActionObserve,
		want:    nil,
	},
	{
		name:    "a peer node lists what it was granted",
		subject: "node:site-b",
		action:  ActionObserve,
		want:    []string{"p-gamma"},
	},
	{
		name:    "a principal with no grant lists nothing",
		subject: "user:mallory",
		action:  ActionObserve,
		want:    nil,
	},
}

func TestListPrinters(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)

		for _, tc := range listCases {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				rows, err := store.ListDevices(t.Context(), subjectOf(t, tc.subject), tc.action, Page{})
				require.NoError(t, err)
				require.Equal(t, tc.want, ids(rows))

				total, err := store.CountDevices(t.Context(), subjectOf(t, tc.subject), tc.action)
				require.NoError(t, err)
				require.Equal(t, len(tc.want), total, "the count has to agree with the rows")
			})
		}
	})
}

func TestListPrintersIsSortedAndPaginated(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)
		root := subjectOf(t, "user:root")

		t.Run("sorted by name, not by id", func(t *testing.T) {
			t.Parallel()

			rows, err := store.ListDevices(t.Context(), root, ActionObserve, Page{})
			require.NoError(t, err)
			require.Equal(t, []string{"Alpha", "Alpha camera", "Beta", "Delta", "Gamma"}, names(rows))
		})

		t.Run("pages do not overlap and do not lose a row", func(t *testing.T) {
			t.Parallel()

			var seen []string

			for offset := 0; offset < len(fixtureDirectory); offset += 2 {
				rows, err := store.ListDevices(t.Context(), root, ActionObserve, Page{Limit: 2, Offset: offset})
				require.NoError(t, err)
				require.NotEmpty(t, rows)
				require.LessOrEqual(t, len(rows), 2, "a page came back wider than its limit")

				seen = append(seen, ids(rows)...)
			}

			require.Equal(t, []string{"p-alpha", "cam-alpha", "p-beta", "p-delta", "p-gamma"}, seen)
		})

		t.Run("past the end is empty, not an error", func(t *testing.T) {
			t.Parallel()

			rows, err := store.ListDevices(t.Context(), root, ActionObserve, Page{Limit: 2, Offset: 99})
			require.NoError(t, err)
			require.Empty(t, rows)
		})

		t.Run("an unregistered printer never appears", func(t *testing.T) {
			t.Parallel()

			rows, err := store.ListDevices(t.Context(), subjectOf(t, "user:dana"), ActionAdminister, Page{})
			require.NoError(t, err)
			require.Empty(t, rows, "p-hidden has a manager grant and no directory row")
		})
	})
}

// TestWalkPrintersVisitsTheSameSetAsTheListing holds the walk to the list. Two
// statements answering one question about a caller's population is two chances
// for one of them to answer about somebody else's.
func TestWalkPrintersVisitsTheSameSetAsTheListing(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)

		for _, tc := range listCases {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				var walked []string

				err := store.WalkDevices(t.Context(), subjectOf(t, tc.subject), tc.action, 1,
					func(row DeviceRow) error {
						walked = append(walked, row.ID)

						return nil
					})
				require.NoError(t, err)
				require.Equal(t, tc.want, walked, "the walk and the listing disagree about this caller")
			})
		}
	})
}

// TestWalkPrintersDoesNotLoseARowToAPrinterRegisteredDuringIt is the reason the
// walk takes each window by where the last one ended.
//
// A window taken by its offset reads rows 0 to n, and a printer registered
// before that position moves every row after it along by one. The window at
// offset n then starts on the row the last one finished with: one row is
// visited twice and the row after it is never visited at all. The write here
// lands between two windows, which is where an arrival lands on a fleet that is
// being dialled into.
func TestWalkPrintersDoesNotLoseARowToAPrinterRegisteredDuringIt(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)
		root := subjectOf(t, "user:root")

		seen := map[string]int{}
		visited := 0

		err := store.WalkDevices(t.Context(), root, ActionObserve, 2,
			func(row DeviceRow) error {
				seen[row.ID]++
				visited++

				if visited == 2 {
					// "Aardvark" sorts ahead of every name in the fixture, so
					// this row lands at the front and pushes the rest along.
					// The org tuple is what makes it visible to this caller: a
					// directory row nobody may see moves nothing.
					if err := store.UpsertDevice(t.Context(), "p-arrived", "Aardvark"); err != nil {
						return err
					}

					return store.Write(t.Context(), mustTuple(t, "device:p-arrived#org@org:acme"))
				}

				return nil
			})
		require.NoError(t, err)

		t.Logf("observed: a printer registered mid-walk left %d visits over %d printers",
			visited, len(seen))

		for id, count := range seen {
			require.Equal(t, 1, count, "%s was visited twice in one walk", id)
		}

		for _, id := range []string{"p-alpha", "p-beta", "p-delta", "p-gamma"} {
			require.Contains(t, seen, id, "the walk skipped %s, which was there before it started", id)
		}
	})
}

func TestListPrintersRejectsWhatIsNotCheckable(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)

		t.Run("an unknown action", func(t *testing.T) {
			t.Parallel()

			_, err := store.ListDevices(t.Context(), subjectOf(t, "user:root"), Action("be_root"), Page{})
			requireCode(t, err, CodeUnknownAction())
		})

		t.Run("a userset", func(t *testing.T) {
			t.Parallel()

			userset, err := NewUserset(TypeGroup, "maintenance", RelMember)
			require.NoError(t, err)

			_, err = store.ListDevices(t.Context(), userset, ActionObserve, Page{})
			requireCode(t, err, CodeInvalidSubject())
		})

		t.Run("a type that holds grants rather than making requests", func(t *testing.T) {
			t.Parallel()

			for _, text := range []string{"org:acme", "device_group:floor-one", "device:p-alpha", "group:maintenance"} {
				_, err := store.ListDevices(t.Context(), subjectOf(t, text), ActionObserve, Page{})
				requireCode(t, err, CodeInvalidSubject())

				total, countErr := store.CountDevices(t.Context(), subjectOf(t, text), ActionObserve)
				requireCode(t, countErr, CodeInvalidSubject())
				require.Zero(t, total)
			}
		})
	})
}

// TestListOrderIsCollationIndependent pins the order across engines. Ordering
// by the name itself would order by the database collation: SQLite compares
// bytes, so "Zulu" would come before "alpha", while a Postgres created with a
// glibc locale compares case-insensitively and would not. Both engines have to
// return this sequence.
func TestListOrderIsCollationIndependent(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)

		require.NoError(t, store.UpsertDevices(t.Context(),
			DeviceRow{ID: "p-one", Name: "Zulu"},
			DeviceRow{ID: "p-two", Name: "alpha"},
			DeviceRow{ID: "p-three", Name: "Bravo"},
			DeviceRow{ID: "p-four", Name: "ALPHA"},
			DeviceRow{ID: "p-five", Name: "_underscore"},
		))
		require.NoError(t, store.Write(t.Context(),
			mustTuple(t, "device:p-one#viewer@user:dana"),
			mustTuple(t, "device:p-two#viewer@user:dana"),
			mustTuple(t, "device:p-three#viewer@user:dana"),
			mustTuple(t, "device:p-four#viewer@user:dana"),
			mustTuple(t, "device:p-five#viewer@user:dana"),
		))

		rows, err := store.ListDevices(t.Context(), subjectOf(t, "user:dana"), ActionObserve, Page{})
		require.NoError(t, err)

		// Case-folded, then byte order, with the printer id breaking the tie
		// between two names that fold to the same key. An underscore is 0x5f,
		// so it lands between the folded letters and everything above them.
		require.Equal(t, []string{"_underscore", "ALPHA", "alpha", "Bravo", "Zulu"}, names(rows))
		require.Equal(t, []string{"p-five", "p-four", "p-two", "p-three", "p-one"}, ids(rows))

		first, err := store.ListDevices(t.Context(), subjectOf(t, "user:dana"), ActionObserve, Page{Limit: 2})
		require.NoError(t, err)
		require.Equal(t, []string{"_underscore", "ALPHA"}, names(first))
	})
}

func TestUpsertPrintersRejectsAnUnusableName(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)

		requireCode(t, store.UpsertDevice(t.Context(), "p-one", ""), CodeInvalidDeviceName())
		requireCode(t,
			store.UpsertDevice(t.Context(), "p-one", strings.Repeat("x", maxDeviceNameLen+1)),
			CodeInvalidDeviceName(),
		)
		require.NoError(t, store.UpsertDevice(t.Context(), "p-one", strings.Repeat("x", maxDeviceNameLen)))

		// ADR-20260829-jdq rule 3. The refusal quotes the name back inside a JSON error
		// body, and a device name is text somebody chose in their own language.
		// The fixture straddles the bound on purpose: byte maxDeviceNameLen
		// falls inside the three-byte character, so a slice expression there
		// leaves half of it in the param.
		straddle := strings.Repeat("x", maxDeviceNameLen-1) + "ř" + "yy"
		require.False(t, utf8.RuneStart(straddle[maxDeviceNameLen]),
			"byte %d has to fall inside a character, or this case proves nothing", maxDeviceNameLen)

		err := store.UpsertDevice(t.Context(), "p-one", straddle)
		requireCode(t, err, CodeInvalidDeviceName())

		// The name is quoted back in a param and not in Error(), so the param
		// is what has to be read. A refusal reaches the caller as JSON, where a
		// cut character becomes the replacement character.
		var refusal *fault.Fault

		require.ErrorAs(t, err, &refusal)

		quoted := ""

		for _, param := range refusal.Params() {
			if param.Key == paramNameName {
				quoted = param.Value.String()
			}
		}

		require.NotEmpty(t, quoted, "the refusal has to quote the name it refused")
		require.True(t, utf8.ValidString(quoted), "the bound cut a character in half")
		require.NotContains(t, quoted, "�", "the encoder would write the replacement character")
	})
}

func TestPageBounds(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		page       Page
		wantLimit  int
		wantOffset int
	}{
		{name: "zero value is one default page", page: Page{}, wantLimit: defaultPageLimit},
		{name: "a negative limit is the default", page: Page{Limit: -1}, wantLimit: defaultPageLimit},
		{name: "an oversized limit is clamped", page: Page{Limit: maxPageLimit * 2}, wantLimit: maxPageLimit},
		{name: "a negative offset is the first page", page: Page{Limit: 10, Offset: -5}, wantLimit: 10},
		{name: "an ordinary page is kept", page: Page{Limit: 25, Offset: 50}, wantLimit: 25, wantOffset: 50},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			limit, offset := tc.page.Bounds()
			require.Equal(t, tc.wantLimit, limit)
			require.Equal(t, tc.wantOffset, offset)
		})
	}
}

func ids(rows []DeviceRow) []string {
	var out []string
	for _, row := range rows {
		out = append(out, row.ID)
	}

	return out
}

func names(rows []DeviceRow) []string {
	var out []string
	for _, row := range rows {
		out = append(out, row.Name)
	}

	return out
}
