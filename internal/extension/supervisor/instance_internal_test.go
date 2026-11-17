package supervisor

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The bound on a condition message.
//
// The message crosses the event bus to every subscriber, to a community sink
// and to a server above this one, and it arrives there inside a JSON string.
// Much of what a driver writes into an error is not English, so a cut at a byte
// offset can land inside a character and reach the encoder as U+FFFD.

// TestALongMessageIsCutOnARuneBoundaryAndSaysItWasCut builds a message whose
// limit falls in the middle of a multi-byte character, so a cut by byte offset
// would leave half of that character behind.
func TestALongMessageIsCutOnARuneBoundaryAndSaysItWasCut(t *testing.T) {
	t.Parallel()

	straddle := strings.Repeat("a", maxMessageLen-1) + "ř" + strings.Repeat("b", 40)
	require.Greater(t, len(straddle), maxMessageLen, "the fixture has to be over the bound")
	require.False(t, utf8.RuneStart(straddle[maxMessageLen]),
		"byte %d has to fall inside a character, or this test proves nothing", maxMessageLen)

	got := message(errors.New(straddle))

	assert.True(t, utf8.ValidString(got), "the bound cut a character in half")
	assert.True(t, strings.HasSuffix(got, "…"), "a cut value has to say it was cut")
	assert.NotContains(t, got, "�", "the encoder would write the replacement character")
	assert.Less(t, len(got), len(straddle), "an oversized message has to be cut")
}

// TestAMessageInsideTheBoundIsTheDriversOwnWord covers the case that must not
// be cut. A rule that cut everything would pass the test above.
func TestAMessageInsideTheBoundIsTheDriversOwnWord(t *testing.T) {
	t.Parallel()

	assert.Empty(t, message(nil))
	assert.Equal(t, "resource is not reachable", message(errors.New("resource is not reachable")))

	exact := strings.Repeat("a", maxMessageLen)
	assert.Equal(t, exact, message(errors.New(exact)), "a message at the bound is not cut")
}
