package discovery

import (
	"log/slog"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

// announcedKeys is the whole TXT record of ADR-20260916-b3x rule 4 and ADR-20261025-gve
// rule 6. The test below
// refuses any key outside it, because a TXT record is read by every machine on
// the link and rule 5 keeps a secret out of it by having nowhere to put one.
var announcedKeys = []string{keyPath, keyVersion, keyAPI, keyTLS, keyID}

// TestTheRecordCarriesTheFiveKeysAndNoOther is ADR-20260916-b3x rules 4 and 5 and
// ADR-20261025-gve rule 6. A sixth key is how the deployment starts describing itself
// to the link, so the test fails on a key it was not told about rather than on
// a key somebody remembered to ban.
func TestTheRecordCarriesTheFiveKeysAndNoOther(t *testing.T) {
	t.Parallel()

	const id = "8f14e45f-ceea-4e7a-9c2b-3d1f6a5b7c90"

	text := Config{Version: "1.2.3", ID: id, TLS: true}.text()

	require.Equal(t, "/", text[keyPath])
	require.Equal(t, "1.2.3", text[keyVersion])
	require.Equal(t, "v1", text[keyAPI])
	require.Equal(t, "1", text[keyTLS])
	require.Equal(t, id, text[keyID])

	for key := range text {
		require.True(t, slices.Contains(announcedKeys, key),
			"the record carries %q, which neither ADR-20260916-b3x rule 4 nor ADR-20261025-gve rule 6 lists", key)
	}

	require.Len(t, text, len(announcedKeys))
}

// TestAPlainListenerAnnouncesNoTLSKey is the second half of rule 4. The key is
// absent rather than zero, because absent is what every client already reads as
// "no", and one value fewer is one value fewer to get wrong.
func TestAPlainListenerAnnouncesNoTLSKey(t *testing.T) {
	t.Parallel()

	text := Config{Version: "0.0.0-dev", ID: "an-id"}.text()

	require.NotContains(t, text, keyTLS)
	require.Len(t, text, len(announcedKeys)-1)
}

// TestABlockSwitchedOffAnnouncesNothing is ADR-20260916-b3x rule 7: enabled: false is
// the one way to switch the announcer off, and it opens no socket at all.
func TestABlockSwitchedOffAnnouncesNothing(t *testing.T) {
	t.Parallel()

	announcer := New(Config{Enabled: false, Services: []string{"_prusalink._tcp"}}, quiet(), NewLink(quiet(), nil))

	require.Equal(t, StateOff, announcer.State())

	announcer.Start(t.Context(), 8080)

	require.Equal(t, StateOff, announcer.State())

	announcer.Stop(t.Context())
}

// TestAnAnnouncerWhosePacketsNeverLeaveReadsAsFailed is the visibility a
// macOS process without Local Network permission reports: the responder runs,
// and nothing it sends reaches another machine.
func TestAnAnnouncerWhosePacketsNeverLeaveReadsAsFailed(t *testing.T) {
	t.Parallel()

	mute := true
	announcer := &Announcer{state: StateAnnouncing, muted: func() bool { return mute }}

	require.Equal(t, StateFailed, announcer.State())

	mute = false

	require.Equal(t, StateAnnouncing, announcer.State())
}

// TestAProcessThatBuiltNoAnnouncerReadsAsOff keeps the readiness report
// answerable in a build that wires none. The report asks the announcer for its
// state on every request, and a nil one is not a panic there.
func TestAProcessThatBuiltNoAnnouncerReadsAsOff(t *testing.T) {
	t.Parallel()

	var none *Announcer

	require.Equal(t, StateOff, none.State())

	none.Start(t.Context(), 8080)
	none.Stop(t.Context())
}

// quiet is a logger that writes nowhere. These cases read return values rather
// than log output.
func quiet() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}
