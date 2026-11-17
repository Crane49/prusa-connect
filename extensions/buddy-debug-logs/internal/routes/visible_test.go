package routes

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/prusa3d/buddy-debug-logs/internal/hostfake"
)

func TestAReadReusesWhatAPersonMaySeeForAFewSeconds(t *testing.T) {
	t.Parallel()

	devices := &hostfake.Devices{}
	devices.Add("mk4", "MK4", "10:9c:70:00:00:01", "192.168.1.20", "")

	clock := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	s := Service{Devices: devices, seen: newVisibleCache(func() time.Time { return clock })}

	read := httptest.NewRequest(http.MethodGet, "/logs", nil)

	for range 3 {
		if _, err := s.visible(read, "ana"); err != nil {
			t.Fatalf("visible: %v", err)
		}
	}

	if got := devices.Lists(); got != 1 {
		t.Fatalf("three reads listed the fleet %d times, want once", got)
	}

	clock = clock.Add(visibleFor)

	if _, err := s.visible(read, "ana"); err != nil {
		t.Fatalf("visible: %v", err)
	}

	if got := devices.Lists(); got != 2 {
		t.Fatalf("a read after %s listed the fleet %d times in all, want twice", visibleFor, got)
	}

	purge := httptest.NewRequest(http.MethodPost, "/data/purge", nil)
	if _, err := s.visible(purge, "ana"); err != nil {
		t.Fatalf("visible: %v", err)
	}

	if got := devices.Lists(); got != 3 {
		t.Fatalf("a change listed the fleet %d times in all, want it to ask again", got)
	}
}
