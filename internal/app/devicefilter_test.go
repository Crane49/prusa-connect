package app

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/printer/profile"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
)

// TestTheSearchFindsAMachineByTheModelNameAPageShows is a machine found by the
// name on its card, such as "Core One" for a CORE One L, and not only by the
// spelling its firmware or driver reports. A reported spelling is compared
// folded, the way the card finds its row.
func TestTheSearchFindsAMachineByTheModelNameAPageShows(t *testing.T) {
	t.Parallel()

	catalogue, err := profile.Place(profile.ModelDoc{
		Model: "Voron24", DisplayName: "Voron 2.4", Vendor: "Voron Design",
	})
	require.NoError(t, err)

	for _, tc := range []struct {
		name, model, typed string
		want               bool
	}{
		{name: "the reported spelling", model: "COREONEL", typed: "coreonel", want: true},
		{name: "a part of the card's name", model: "COREONEL", typed: "Core One", want: true},
		{name: "the card's name", model: "COREONEL", typed: "Prusa CORE One L", want: true},
		{name: "a spaced spelling", model: "Core One L", typed: "Prusa CORE One L", want: true},
		{name: "a hyphenated spelling", model: "core-one-l", typed: "Prusa CORE One L", want: true},
		{name: "a trailing space", model: "CoreOneL ", typed: "Prusa CORE One L", want: true},
		{name: "another model's name", model: "COREONEL", typed: "CORE One L INDX", want: false},
		{name: "an unrelated model", model: "COREONEL", typed: "Prusa MK4", want: false},
		{name: "the serial", model: "COREONEL", typed: "3742441632002", want: true},
		{name: "a driver-declared model", model: "voron-2.4", typed: "Voron 2.4", want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			filter, filterErr := newDeviceFilter(httpapi.DeviceQuery{Search: tc.typed}, catalogue)
			require.NoError(t, filterErr)

			facts := deviceFacts{name: "Bench", model: tc.model, serial: "10589-3742441632002"}
			require.Equal(t, tc.want, filter.matchesSearch(facts), "search %q for %q", tc.typed, tc.model)
		})
	}
}
