package deviceapi_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/deviceapi"
	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

// A history read naming no device is a page over the whole fleet, which is the
// question a report asks first, so it passes with no params at all.
func TestAHistoryReadWithNoParamsIsTheWholeFleet(t *testing.T) {
	t.Parallel()

	fleet := &fakeFleet{history: deviceapi.JobPage{Limit: deviceapi.DefaultPageLimit}}

	if _, err := call(t, deviceapi.Handlers(fleet), deviceapi.MethodJobHistory, nil); err != nil {
		t.Fatalf("job history: %v", err)
	}

	if fleet.sawHistory.ID != "" {
		t.Fatalf("the host was asked about %q, want the whole fleet", fleet.sawHistory.ID)
	}
}

// A range that starts after it ends holds no print, so it is refused rather
// than answered with an empty page that looks like a fleet that printed
// nothing.
func TestAHistoryRangeThatStartsAfterItEndsIsRefused(t *testing.T) {
	t.Parallel()

	params := deviceapi.JobHistoryParams{
		From: time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC),
		To:   time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC),
	}

	if err := params.Validate(); !errors.Is(err, deviceapi.ErrInvalidQuery) {
		t.Fatalf("Validate = %v, want %v", err, deviceapi.ErrInvalidQuery)
	}

	fleet := &fakeFleet{}

	_, err := call(t, deviceapi.Handlers(fleet), deviceapi.MethodJobHistory, params)

	var coded *wirerpc.Error
	if !errors.As(err, &coded) || coded.Code != deviceapi.CodeInvalidQuery {
		t.Fatalf("the host answered %v, want code %d", err, deviceapi.CodeInvalidQuery)
	}
}

// Every bound this contract publishes is checked on the host as well, because
// a limit checked only in a client is a limit a peer that is not this client
// never made.
func TestAHistoryReadOutsideTheBoundsIsRefused(t *testing.T) {
	t.Parallel()

	cases := map[string]deviceapi.JobHistoryParams{
		"the limit":  {Limit: deviceapi.MaxPageLimit + 1},
		"the cursor": {Cursor: strings.Repeat("a", deviceapi.MaxCursorBytes+1)},
		"the device": {ID: strings.Repeat("a", deviceapi.MaxIDBytes+1)},
	}

	for name, params := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if err := params.Validate(); err == nil {
				t.Fatal("a read outside the published bounds was accepted")
			}

			fleet := &fakeFleet{}

			if _, err := call(t, deviceapi.Handlers(fleet),
				deviceapi.MethodJobHistory, params); err == nil {
				t.Fatal("the host answered a read outside the published bounds")
			}
		})
	}
}

// The host reads the params as the module wrote them, and the page comes back
// with the cursor that continues it.
func TestTheHostReadsAHistoryReadAsItWasWritten(t *testing.T) {
	t.Parallel()

	fleet := &fakeFleet{history: deviceapi.JobPage{
		Items:  []deviceapi.Job{{ID: "p1:7"}},
		Cursor: "next",
		Limit:  10,
	}}

	params := deviceapi.JobHistoryParams{
		Principal: "user:alpha", ID: "p1",
		From:  time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC),
		To:    time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC),
		Limit: 10, Cursor: "held",
	}

	answered, err := call(t, deviceapi.Handlers(fleet), deviceapi.MethodJobHistory, params)
	if err != nil {
		t.Fatalf("job history: %v", err)
	}

	if !fleet.sawHistory.From.Equal(params.From) || !fleet.sawHistory.To.Equal(params.To) {
		t.Fatalf("the host was asked for %+v, want the range the module wrote", fleet.sawHistory)
	}

	if fleet.sawHistory.Cursor != "held" || fleet.sawHistory.Limit != 10 {
		t.Fatalf("the host was asked for %+v, want the page the module wrote", fleet.sawHistory)
	}

	page, ok := answered.(deviceapi.JobPage)
	if !ok || page.Cursor != "next" || len(page.Items) != 1 {
		t.Fatalf("the answer is %+v, want the page the host read", answered)
	}
}
