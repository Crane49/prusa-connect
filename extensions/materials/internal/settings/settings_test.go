package settings_test

import (
	"errors"
	"testing"

	"github.com/prusa3d/materials/internal/settings"
)

func TestTheZeroValueAnswersTheDefaults(t *testing.T) {
	t.Parallel()

	var live settings.Live

	if held := live.Get(); held != settings.Defaults() {
		t.Errorf("a module that was sent no settings runs on %+v", held)
	}
}

func TestASettingOutOfRangeIsRefusedAndKeepsWhatStood(t *testing.T) {
	t.Parallel()

	var live settings.Live

	if err := live.Set(settings.Values{MaxSpools: 50}); err != nil {
		t.Fatalf("a valid set was refused: %v", err)
	}

	if err := live.Set(settings.Values{MaxSpools: 0}); !errors.Is(err, settings.ErrInvalid) {
		t.Fatalf("max_spools 0 answered %v, want ErrInvalid", err)
	}

	if got := live.Get().MaxSpools; got != 50 {
		t.Errorf("a refused set changed the settings to %d", got)
	}
}
