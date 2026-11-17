package mqtt

import (
	"strconv"
	"strings"
)

// Base is the root of the frozen namespace. Every per-printer topic hangs off
// it, and the `v1/` prefix is the version marker a subscriber matched on.
const Base = "v1/devices/printers"

// Placeholder segments used by the patterns below. A pattern is not an MQTT
// subscription filter: `{printerId}` is substituted, not matched.
const (
	printerPlaceholder = "{printerId}"
	jobPlaceholder     = "{jobId}"
	toolPlaceholder    = "{toolId}"
	// The two the fact channel uses. An origin substitutes as its path form,
	// "printer/mk4s-01", so it is two matchable segments rather than one.
	originPlaceholder = "{origin}"
	typePlaceholder   = "{eventType}"
)

// Topic is one channel of the frozen contract: where a fact is published, at
// which quality of service, and whether the broker keeps the last value for a
// subscriber that arrives later.
//
// QoS and Retain are not tuning knobs. A subscriber that joined an hour ago
// sees a retained topic immediately and a non-retained one only on the next
// change, so flipping either changes what an installed client observes.
type Topic struct {
	// Name is the stable key this channel is known by, matching the key the
	// shipping server's registry and the generated AsyncAPI document use.
	Name string
	// Pattern is the full topic with `{printerId}`, `{jobId}` and `{toolId}`
	// placeholders left in place.
	Pattern string
	// QoS is the delivery guarantee this channel is published at.
	QoS byte
	// Retain says whether the broker stores the last payload for new
	// subscribers.
	Retain bool
}

// The frozen channels. Existing subscribers read these today, so a
// changed pattern, QoS or retain flag is a migration rather than a decision.
// ADR-20261025-frz made one: the tool channels replaced the
// printer-wide material and nozzle channels and the slot channels.
// TestTheTopicTableMatchesTheShippingAsyncAPIDocument holds this table against
// the published document.
var (
	// ServerOnline carries "1" while the server holds a connection and "0"
	// when it does not, whether it left cleanly or the broker fired the will.
	ServerOnline = Topic{Name: "serverOnline", Pattern: "v1/server/online", QoS: 1, Retain: true}

	// DeviceState carries the printer state machine value: IDLE, PRINTING,
	// PAUSED, STOPPED and the rest of the vocabulary the core speaks.
	DeviceState = Topic{Name: "state", Pattern: printerTopic("data/state"), QoS: 1, Retain: true}

	// PrinterOnline is per-printer reachability, "1" or "0".
	PrinterOnline = Topic{Name: "online", Pattern: printerTopic("data/online"), QoS: 1, Retain: true}

	// Speed is the print speed override percentage.
	Speed = Topic{Name: "speed", Pattern: printerTopic("data/speed"), QoS: 1}

	// BedTemperature is the current heatbed temperature in whole degrees.
	BedTemperature = Topic{Name: "tempBedCurrent", Pattern: printerTopic("data/temp/heatbed/current")}

	// BedTarget is the requested heatbed temperature in whole degrees.
	BedTarget = Topic{Name: "tempBedTarget", Pattern: printerTopic("data/temp/heatbed/target"), QoS: 1}

	// ChamberTemperature is the current chamber temperature in whole degrees.
	ChamberTemperature = Topic{Name: "tempChamberCurrent", Pattern: printerTopic("data/temp/chamber/current")}

	// AxisX is the X carriage position in millimetres, two decimal places.
	AxisX = Topic{Name: "axisX", Pattern: printerTopic("data/axis-x"), Retain: true}

	// AxisY is the Y carriage position in millimetres, two decimal places.
	AxisY = Topic{Name: "axisY", Pattern: printerTopic("data/axis-y"), Retain: true}

	// AxisZ is the Z carriage position in millimetres, two decimal places.
	AxisZ = Topic{Name: "axisZ", Pattern: printerTopic("data/axis-z"), Retain: true}

	// BlockingReasons is a nudge to re-query, not a carrier: the payload is a
	// unix timestamp and holds no reasons. It is not retained, because a
	// stored nudge makes every new subscriber refetch for a change it can
	// already see.
	BlockingReasons = Topic{Name: "blockingReasons", Pattern: printerTopic("data/blocking-reasons"), QoS: 1}

	// CurrentJob is the printer-reported id of the running job, or 0 for none.
	CurrentJob = Topic{Name: "currentJob", Pattern: printerTopic("data/current-job"), QoS: 1, Retain: true}

	// JobProgress is the running job's percentage, published at printer level.
	// Its per-job twin is JobProgressByID, and both carry the same number:
	// section 7 freezes the pair, not either one alone.
	JobProgress = Topic{Name: "jobProgress", Pattern: printerTopic("data/job-progress")}

	// JobState is the per-job state in the Connect vocabulary: PRINTING,
	// PAUSED, FIN_OK, FIN_STOPPED, FIN_ERROR.
	JobState = Topic{Name: "jobState", Pattern: jobTopic("state"), QoS: 1, Retain: true}

	// JobProgressByID is the per-job twin of JobProgress.
	JobProgressByID = Topic{Name: "jobProgressById", Pattern: jobTopic("progress")}

	// JobTimeRemaining is the per-job estimate in seconds.
	JobTimeRemaining = Topic{Name: "jobTimeRemaining", Pattern: jobTopic("time-remaining")}

	// Dialog is the printer's attention or error prompt as a JSON object. It
	// is the one channel published at QoS 2: a duplicate prompt is a duplicate
	// alert in front of an operator.
	Dialog = Topic{
		Name:    "dialog",
		Pattern: Base + "/" + printerPlaceholder + "/dialog",
		QoS:     qosExactlyOnce,
		Retain:  true,
	}

	// The tool channels carry the tool tree (ADR-20261025-pze, ADR-20261025-frz). `{toolId}`
	// is the tree's id: "0" for the first head, "1" for the second, and "0.2"
	// for slot 2 of the feeder on head 0. A head has all four channels. A
	// feeder slot has no nozzle, so it has the material and the active
	// channel only. Each channel has an empty payload while its value is not
	// known: see tools.go.

	// ToolActive is "1" while the tool is active, as the tool tree marks it,
	// and "0" while it is not.
	ToolActive = Topic{Name: "toolActive", Pattern: toolTopic("active"), QoS: 1, Retain: true}

	// ToolMaterial is the short name of the material loaded in the tool, such
	// as PLA or PETG. It is an empty payload while nothing is loaded.
	ToolMaterial = Topic{Name: "toolMaterial", Pattern: toolTopic("material"), QoS: 1, Retain: true}

	// ToolNozzleTemperature is the current temperature of a head's nozzle, in
	// whole degrees.
	ToolNozzleTemperature = Topic{Name: "toolTempNozzleCurrent", Pattern: toolTopic("temp/nozzle/current")}

	// ToolNozzleTarget is the temperature a head's nozzle is heating to, in
	// whole degrees. It is 0 while the heater is off.
	ToolNozzleTarget = Topic{Name: "toolTempNozzleTarget", Pattern: toolTopic("temp/nozzle/target"), QoS: 1}
)

// FactObserved is where a durable event off the host's bus is republished, one
// topic per origin and event type.
//
// It is deliberately not in Topics, and that is not a loophole. The gate above
// holds Topics equal to the shipping AsyncAPI document in both directions: a
// pattern the document does not carry fails the run with "this adapter
// publishes a topic the shipping server does not, which puts a channel into a
// frozen namespace". That check is right and this channel must not weaken it,
// so the frozen set stays exactly what shipped and this one is declared beside
// it as an addition of this rewrite.
//
// The addition sits under `v1/events/` rather than under `v1/devices/printers/`
// for the same reason. An existing subscriber reads the printer subtree, and
// putting bus facts inside it would hand every installed client
// traffic it never asked for and cannot parse. Under its own root a subscriber
// opts in, and the frozen subtree stays exactly as wide as it was.
//
// The pattern is `v1/events/<origin path>/<event type>`, so the origin is two
// matchable segments, "printer/mk4s-01", and a subscriber filters by machine
// with `v1/events/printer/+/#` or by fact with
// `v1/events/+/+/com.prusa.connect.device.command.settled`.
//
// It is published at QoS 1 and not retained. A fact is something that happened
// once: a retained one would be replayed to every new subscriber as though it
// had just happened, which for a command settling is an alert an operator sees
// again for every dashboard they open.
var FactObserved = Topic{
	Name:    "factObserved",
	Pattern: "v1/events/" + originPlaceholder + "/" + typePlaceholder,
	QoS:     1,
}

// Topics is every frozen channel, in the order the shipping registry declares
// them. The divergence test walks it.
var Topics = []Topic{
	ServerOnline,
	DeviceState,
	PrinterOnline,
	Speed,
	BedTemperature,
	BedTarget,
	ChamberTemperature,
	AxisX,
	AxisY,
	AxisZ,
	BlockingReasons,
	CurrentJob,
	JobProgress,
	JobState,
	JobProgressByID,
	JobTimeRemaining,
	Dialog,
	ToolActive,
	ToolMaterial,
	ToolNozzleTemperature,
	ToolNozzleTarget,
}

// For resolves a per-printer topic.
func (t Topic) For(printerID string) string {
	return strings.ReplaceAll(t.Pattern, printerPlaceholder, printerID)
}

// ForJob resolves a per-job topic. A job id of zero has no topic, which is why
// every caller guards on it before reaching here.
func (t Topic) ForJob(printerID string, jobID int) string {
	return strings.ReplaceAll(t.For(printerID), jobPlaceholder, strconv.Itoa(jobID))
}

// ForTool resolves a per-tool topic. The tool id is the tool tree's.
func (t Topic) ForTool(printerID, toolID string) string {
	return strings.ReplaceAll(t.For(printerID), toolPlaceholder, toolID)
}

// ForFact resolves the fact channel for one origin and one event type. It takes
// the rendered origin path rather than an event.Origin, because this file
// describes topics and knows nothing about the contract that feeds them.
func (t Topic) ForFact(originPath, eventType string) string {
	resolved := strings.ReplaceAll(t.Pattern, originPlaceholder, originPath)

	return strings.ReplaceAll(resolved, typePlaceholder, eventType)
}

func printerTopic(suffix string) string {
	return Base + "/" + printerPlaceholder + "/" + suffix
}

func jobTopic(suffix string) string {
	return Base + "/" + printerPlaceholder + "/jobs/" + jobPlaceholder + "/data/" + suffix
}

func toolTopic(suffix string) string {
	return Base + "/" + printerPlaceholder + "/data/tools/" + toolPlaceholder + "/" + suffix
}
