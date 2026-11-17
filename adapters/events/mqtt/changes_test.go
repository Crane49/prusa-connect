package mqtt

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAnIdenticalPayloadIsNotPublishedTwice(t *testing.T) {
	t.Parallel()

	filter := newChangeFilter()
	topic := DeviceState.For("abc")

	assert.True(t, filter.accept(topic, "PRINTING"), "the first value is always a change")
	assert.False(t, filter.accept(topic, "PRINTING"))
	assert.True(t, filter.accept(topic, "PAUSED"))
	assert.True(t, filter.accept(topic, "PRINTING"), "a value that comes back is a change")
}

// The one degree threshold is not a comparison of temperatures. It is the
// rounding in the payload encoder meeting a filter that compares payloads, and
// this is where the two are checked together.
func TestATemperatureBelowAWholeDegreeIsNotPublished(t *testing.T) {
	t.Parallel()

	filter := newChangeFilter()
	topic := ToolNozzleTemperature.ForTool("abc", "0")

	for _, tc := range []struct {
		celsius float64
		publish bool
		why     string
	}{
		{celsius: 21.0, publish: true, why: "first reading"},
		{celsius: 21.4, publish: false, why: "rounds to 21, unchanged"},
		{celsius: 20.6, publish: false, why: "rounds to 21, unchanged"},
		{celsius: 21.5, publish: true, why: "rounds to 22"},
		{celsius: 22.4, publish: false, why: "rounds to 22, unchanged"},
		{celsius: 20.9, publish: true, why: "rounds to 21"},
	} {
		got := filter.accept(topic, temperaturePayload(tc.celsius))
		assert.Equal(t, tc.publish, got, "%.1f degrees: %s", tc.celsius, tc.why)
	}
}

func TestAFailedPublishIsAttemptedAgain(t *testing.T) {
	t.Parallel()

	filter := newChangeFilter()
	topic := ToolMaterial.ForTool("abc", "0")

	assert.True(t, filter.accept(topic, "PLA"))

	// A message that never left must not be mistaken for one that did, or the
	// value stays wrong on the broker until it happens to change again.
	filter.invalidate(topic)
	assert.True(t, filter.accept(topic, "PLA"))
}

func TestForgettingAPrinterLeavesTheOthersAlone(t *testing.T) {
	t.Parallel()

	filter := newChangeFilter()
	gone, kept := DeviceState.For("gone"), DeviceState.For("kept")

	filter.accept(gone, "IDLE")
	filter.accept(kept, "IDLE")
	filter.accept(JobState.ForJob("gone", 7), "PRINTING")

	filter.forgetPrinter("gone")

	assert.True(t, filter.accept(gone, "IDLE"))
	assert.True(t, filter.accept(JobState.ForJob("gone", 7), "PRINTING"))
	assert.False(t, filter.accept(kept, "IDLE"), "the printer that was kept is still remembered")
}

func TestReconnectingForgetsEverything(t *testing.T) {
	t.Parallel()

	filter := newChangeFilter()
	topic := BedTarget.For("abc")

	filter.accept(topic, "60")

	// A broker that restarted without persistence has lost its retained set,
	// and a filter that remembered across that would leave the value absent
	// until it next changed.
	filter.reset()
	assert.True(t, filter.accept(topic, "60"))
}
