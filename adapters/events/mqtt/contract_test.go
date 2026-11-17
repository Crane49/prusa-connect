package mqtt

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/sdk/eventsapi"
)

const shippingRecord = "testdata/shipping-publisher.tsv"

// timestampPayload marks a field whose value is the current time, so the
// comparison checks its shape instead of its digits.
const timestampPayload = "<unix-seconds>"

type record struct {
	payload string
	qos     byte
	retain  bool
}

// TestWhatIsPublishedMatchesTheShippingServerFieldByField is the check that a
// rewrite has not quietly changed a payload.
//
// The record it compares against was captured by running the shipping server's
// own telemetry publisher against a broker, not by reading its source: an earlier
// study was written by hand and has already been wrong once, so a transcription is
// not evidence. Both sides are observed through the same broker, from the same
// inputs, and every field of every topic is compared.
func TestWhatIsPublishedMatchesTheShippingServerFieldByField(t *testing.T) {
	address := testBroker(t)
	printer := uniquePrinter(t)

	expected := loadShippingRecord(t, printer)

	p := connectedProcessor(t, address)

	live := subscribeAt(t, address, Base+"/"+printer+"/#")

	publishOneOfEach(p.Publisher, printer)
	live.await(t, len(expected))

	late := subscribeAt(t, address, Base+"/"+printer+"/#")
	late.settle()

	retained := map[string]bool{}
	for _, message := range late.seen() {
		retained[message.topic] = message.retain
	}

	got := make(map[string]record, len(expected))
	for _, message := range live.seen() {
		got[message.topic] = record{payload: message.payload, qos: message.qos, retain: retained[message.topic]}
	}

	for topic, want := range expected {
		mine, published := got[topic]
		if !assert.True(t, published, "the shipping server publishes %s and this adapter did not", topic) {
			continue
		}

		if want.payload == timestampPayload {
			assertUnixSeconds(t, topic, mine.payload)
		} else {
			assert.Equal(t, want.payload, mine.payload, "payload on %s", topic)
		}

		assert.Equal(t, want.qos, mine.qos, "QoS on %s", topic)
		assert.Equal(t, want.retain, mine.retain, "retain on %s", topic)
	}

	for topic := range got {
		_, known := expected[topic]
		assert.True(t, known, "this adapter published %s, which the shipping server does not", topic)
	}

	t.Logf("compared %d topics field by field against the shipping publisher", len(expected))
}

func loadShippingRecord(t *testing.T, printer string) map[string]record {
	t.Helper()

	raw, err := os.ReadFile(filepath.Clean(shippingRecord))
	require.NoError(t, err)

	expected := map[string]record{}

	for line := range strings.SplitSeq(string(raw), "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		fields := strings.Split(line, "\t")
		require.Len(t, fields, 4, "malformed record line: %q", line)

		qos, atoiErr := strconv.Atoi(fields[2])
		require.NoError(t, atoiErr)

		topic := strings.ReplaceAll(fields[0], printerPlaceholder, printer)
		expected[topic] = record{payload: fields[1], qos: byte(qos), retain: fields[3] == "true"}
	}

	return expected
}

func assertUnixSeconds(t *testing.T, topic, payload string) {
	t.Helper()

	seconds, err := strconv.ParseInt(payload, 10, 64)
	require.NoError(t, err, "payload on %s should be a unix timestamp, got %q", topic, payload)

	assert.WithinDuration(t, time.Now(), time.Unix(seconds, 0), time.Minute, "timestamp on %s", topic)
}

// TestAJobMovesTheCurrentJobPointer covers the mapping from the core's job
// vocabulary onto the frozen one, which is where a rewrite is most likely to
// change what a dashboard reads.
func TestAJobMovesTheCurrentJobPointer(t *testing.T) {
	address := testBroker(t)
	printer := uniquePrinter(t)

	p := connectedProcessor(t, address)
	observer := subscribeAt(t, address, Base+"/"+printer+"/#")

	// A job that has not reached the printer has no state in the frozen
	// vocabulary, and the shipping server publishes nothing for it.
	p.JobUpdated(eventsapi.JobInfo{PrinterID: printer, ExternalID: 0, State: "QUEUED"})
	p.JobUpdated(eventsapi.JobInfo{PrinterID: printer, ExternalID: 0, State: "DOWNLOADING"})

	observer.settle()
	assert.Empty(t, observer.seen(), "a queued job has no topic")

	p.JobUpdated(eventsapi.JobInfo{PrinterID: printer, ExternalID: 42, State: "PRINTING"})
	p.JobUpdated(eventsapi.JobInfo{PrinterID: printer, ExternalID: 42, State: "PAUSED"})
	p.JobUpdated(eventsapi.JobInfo{PrinterID: printer, ExternalID: 42, State: "FINISHED"})

	observer.settle()

	states := observer.on(JobState.ForJob(printer, 42))
	require.Len(t, states, 3)
	assert.Equal(t, "PRINTING", states[0].payload)
	assert.Equal(t, "PAUSED", states[1].payload)
	assert.Equal(t, "FIN_OK", states[2].payload, "the frozen vocabulary spells a finished job FIN_OK")

	pointer := observer.on(CurrentJob.For(printer))
	require.Len(t, pointer, 2, "the pointer moves once to the job and once back to none")
	assert.Equal(t, "42", pointer[0].payload)
	assert.Equal(t, "0", pointer[1].payload, "a terminal job clears the pointer")

	t.Logf("job states: %v", states)
	t.Logf("current-job: %v", pointer)
}

func TestATerminalJobStateIsSpelledTheFrozenWay(t *testing.T) {
	t.Parallel()

	for state, want := range map[string]string{
		"PRINTING":    "PRINTING",
		"PAUSED":      "PAUSED",
		"FINISHED":    "FIN_OK",
		"STOPPED":     "FIN_STOPPED",
		"FAILED":      "FIN_ERROR",
		"ABANDONED":   "FIN_ERROR",
		"QUEUED":      "",
		"DOWNLOADING": "",
		"NONSENSE":    "",
	} {
		assert.Equal(t, want, connectJobState(state), "job state %q", state)
	}
}

// TestADeletedPrinterIsMarkedOffline is the one thing this adapter does with a
// printer that goes away. The retained topics stay on the broker, which is what
// the shipping server leaves behind too.
func TestADeletedPrinterIsMarkedOffline(t *testing.T) {
	address := testBroker(t)
	printer := uniquePrinter(t)

	p := connectedProcessor(t, address)
	observer := subscribeAt(t, address, Base+"/"+printer+"/#")

	p.PrinterStateChanged(eventsapi.PrinterState{ID: printer, State: "IDLE"})
	p.PrinterDeleted(printer)

	observer.settle()

	offline := observer.on(PrinterOnline.For(printer))
	require.Len(t, offline, 1)
	assert.Equal(t, "0", offline[0].payload)
	assert.Equal(t, byte(1), offline[0].qos)

	// Forgetting the printer means the same state published again is a change,
	// which is what stops the filter growing for the life of the process.
	p.PrinterStateChanged(eventsapi.PrinterState{ID: printer, State: "IDLE"})
	observer.settle()

	assert.Len(t, observer.on(DeviceState.For(printer)), 2)
}

// TestALabelChangeIsNotPublished pins a deliberate absence. The frozen
// namespace has no label channel, and inventing one would put a topic no client
// asked for into a namespace whose value is that it does not change.
//
// The test listens to the whole namespace, so it sees a label topic wherever
// one would be put. The broker is shared, and other tests publish into the same
// namespace while this one runs. So the test counts only messages whose topic
// or payload names this run's printer or labels, and no other client can send
// those.
func TestALabelChangeIsNotPublished(t *testing.T) {
	address := testBroker(t)
	printer := uniquePrinter(t)
	labels := []string{printer + "-label-a", printer + "-label-b"}

	p := connectedProcessor(t, address)
	observer := subscribeAt(t, address, "v1/#")

	p.PrinterLabelsChanged(eventsapi.PrinterLabels{ID: printer, Mode: "replace", Labels: labels})
	p.LabelDeleted(labels[0])

	observer.settle()

	var ours []observed

	for _, message := range observer.seen() {
		for _, name := range append([]string{printer}, labels...) {
			if strings.Contains(message.topic, name) || strings.Contains(message.payload, name) {
				ours = append(ours, message)

				break
			}
		}
	}

	assert.Empty(t, ours, "neither label event has a topic in the frozen namespace")
}
