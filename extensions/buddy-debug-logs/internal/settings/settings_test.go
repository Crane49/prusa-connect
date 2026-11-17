package settings_test

import (
	"errors"
	"testing"

	"github.com/prusa3d/buddy-debug-logs/internal/settings"
	"github.com/prusa3d/buddy-debug-logs/internal/store"
)

func TestTheDefaultsAreInRange(t *testing.T) {
	t.Parallel()

	if _, err := settings.Defaults().Checked(); err != nil {
		t.Fatalf("the defaults are refused: %v", err)
	}
}

func TestAValueOutOfRangeIsRefused(t *testing.T) {
	t.Parallel()

	for _, v := range []settings.Values{
		{RetentionHours: 0, MaxMiB: 128},
		{RetentionHours: 24*30 + 1, MaxMiB: 128},
		{RetentionHours: 24, MaxMiB: 3},
		{RetentionHours: 24, MaxMiB: 5000},
	} {
		if _, err := v.Checked(); !errors.Is(err, settings.ErrInvalid) {
			t.Errorf("%+v: err = %v", v, err)
		}
	}
}

// The log lines keep a quarter of the room, so metric points never push
// them out.
func TestTheLogLinesKeepAQuarterOfTheRoom(t *testing.T) {
	t.Parallel()

	limits := settings.Values{RetentionHours: 24, MaxMiB: 128}.Limits()

	if limits.Bytes[store.Syslog] != 32<<20 || limits.Bytes[store.Metrics] != 96<<20 || limits.Hours != 24 {
		t.Errorf("limits = %+v", limits)
	}
}
