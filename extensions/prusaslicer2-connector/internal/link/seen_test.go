package link_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/prusa3d/prusaslicer2-connector/internal/link"
	"github.com/prusa3d/prusaslicer2-connector/internal/routes"
)

const seenPath = "/printers/" + printer + "/seen"

// clocked is a rig whose service reads the time from now, and a fresh service
// over the same data folder, as after a restart.
func clocked(t *testing.T, now *time.Time) (*rig, func()) {
	t.Helper()

	r := newRig(t)
	build := func() {
		service := link.New(link.Options{
			Fleet: r.fleet, Access: fakeAccess{allowed: true}, Files: r.files,
			Data: r.data, Version: "0.1.0", Now: func() time.Time { return *now },
		})
		r.handler = routes.New(routes.Host{
			Principal: func(*http.Request) string { return alice },
			BodyFile:  func(*http.Request) string { return r.staged },
		}, func() *link.Service { return service })
	}
	build()

	return r, build
}

// A printer PrusaSlicer has not reached says so, and the connection test
// records the time, which a restart keeps.
func TestTheCardSaysWhenPrusaSlicerLastReachedThePrinter(t *testing.T) {
	now := time.Date(2026, 11, 24, 10, 0, 0, 0, time.UTC)
	r, restart := clocked(t, &now)

	if got := decode(t, r.do(http.MethodGet, seenPath, nil, nil)); got["seen_at"] != nil {
		t.Fatalf("before any request: %v", got)
	}

	if rec := r.do(http.MethodGet, "/printers/"+printer+"/api/version", nil, nil); rec.Code != http.StatusOK {
		t.Fatalf("version: %d", rec.Code)
	}

	restart()

	if got := decode(t, r.do(http.MethodGet, seenPath, nil, nil)); got["seen_at"] != "2026-11-24T10:00:00Z" {
		t.Fatalf("after the test: %v", got)
	}
}

// The time moves on with each request a minute or more later, and the storage
// list counts as a request too.
func TestTheLastSeenTimeMovesOnByTheMinute(t *testing.T) {
	now := time.Date(2026, 11, 24, 10, 0, 0, 0, time.UTC)
	r, _ := clocked(t, &now)

	r.do(http.MethodGet, "/printers/"+printer+"/api/version", nil, nil)

	now = now.Add(20 * time.Second)
	r.do(http.MethodGet, "/printers/"+printer+"/api/v1/storage", nil, nil)

	if got := decode(t, r.do(http.MethodGet, seenPath, nil, nil)); got["seen_at"] != "2026-11-24T10:00:00Z" {
		t.Fatalf("within the minute: %v", got)
	}

	now = now.Add(time.Minute)
	r.do(http.MethodGet, "/printers/"+printer+"/api/v1/storage", nil, nil)

	if got := decode(t, r.do(http.MethodGet, seenPath, nil, nil)); got["seen_at"] != "2026-11-24T10:01:20Z" {
		t.Fatalf("a minute later: %v", got)
	}
}

// A request for a printer the person may not see records nothing, and the
// card's own reads do not count as PrusaSlicer.
func TestOnlyPrusaSlicersRequestsCount(t *testing.T) {
	now := time.Date(2026, 11, 24, 10, 0, 0, 0, time.UTC)
	r, _ := clocked(t, &now)

	r.do(http.MethodGet, "/printers/other/api/version", nil, nil)
	r.do(http.MethodGet, keepPath, nil, nil)
	r.do(http.MethodGet, seenPath, nil, nil)

	if got := decode(t, r.do(http.MethodGet, seenPath, nil, nil)); got["seen_at"] != nil {
		t.Fatalf("seen %v", got)
	}
}
