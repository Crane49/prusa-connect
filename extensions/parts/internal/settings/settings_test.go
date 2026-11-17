package settings_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/prusa3d/parts/internal/settings"
)

func TestTheZeroValueAnswersTheDefaults(t *testing.T) {
	t.Parallel()

	var live settings.Live

	if held := live.Get(); held != settings.Defaults() {
		t.Errorf("a module that was sent no settings runs on %+v", held)
	}
}

// A setting of the wrong shape is refused, so the host shows the instance as
// failed with the reason. A default chosen in silence would put the files
// somewhere the operator never named.
func TestASettingOfTheWrongShapeIsRefused(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		sent settings.Values
	}{
		{name: "an empty storage", sent: settings.Values{Storage: "  ", Root: "parts"}},
		{name: "a storage with a space", sent: settings.Values{Storage: "my disk", Root: "parts"}},
		{name: "a storage with a slash", sent: settings.Values{Storage: "a/b", Root: "parts"}},
		{
			name: "a storage longer than a name",
			sent: settings.Values{Storage: strings.Repeat("x", settings.MaxStorageBytes+1), Root: "parts"},
		},
		{name: "an empty root", sent: settings.Values{Storage: "local", Root: " / "}},
		{name: "a root that climbs out", sent: settings.Values{Storage: "local", Root: "../etc"}},
		{name: "a root with a dot segment", sent: settings.Values{Storage: "local", Root: "a/./b"}},
	}

	for _, one := range cases {
		t.Run(one.name, func(t *testing.T) {
			t.Parallel()

			var live settings.Live

			if err := live.Set(settings.Values{Storage: "kept", Root: "kept"}); err != nil {
				t.Fatalf("a valid set answered %v", err)
			}

			if err := live.Set(one.sent); !errors.Is(err, settings.ErrInvalid) {
				t.Errorf("%+v answered %v", one.sent, err)
			}

			if held := live.Get(); held.Storage != "kept" || held.Root != "kept" {
				t.Errorf("a refused set changed the settings to %+v", held)
			}
		})
	}
}

func TestASettingIsTrimmed(t *testing.T) {
	t.Parallel()

	var live settings.Live

	if err := live.Set(settings.Values{Storage: " archive ", Root: "/shop/parts/"}); err != nil {
		t.Fatalf("a valid set answered %v", err)
	}

	if held := live.Get(); held.Storage != "archive" || held.Root != "shop/parts" {
		t.Errorf("the module runs on %+v", held)
	}
}
