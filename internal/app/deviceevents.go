package app

import (
	"encoding/json"
	"time"

	"github.com/prusa3d/prusa-connect/internal/kernel/machinetext"
	"github.com/prusa3d/prusa-connect/internal/platform/devicestore"
	"github.com/prusa3d/prusa-connect/internal/printer/capability"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/deviceapi"
)

// arrival is what a subscriber reads when a device joins: who it said it is,
// and what this server resolved it can do.
//
// The capability digest is here rather than the set itself. It is the stable
// hash of what was resolved, so "which capabilities were in force" is a lookup
// against the device rather than a payload repeated on every arrival.
type arrival struct {
	Printer        string `json:"device_id"`
	Driver         string `json:"driver"`
	Vendor         string `json:"vendor"`
	Model          string `json:"model"`
	Firmware       string `json:"firmware"`
	Serial         string `json:"serial"`
	Capabilities   int    `json:"capabilities"`
	CapabilityHash string `json:"capability_digest"`
	Epoch          uint64 `json:"capability_epoch"`
	ProfileMatched bool   `json:"profile_matched"`
}

// departure is what a subscriber reads when a device's session ends.
//
// The reason is here so that a reader marking the device as gone can say why
// without asking, which is the whole of what the listing then carries under
// departed.
type departure struct {
	Printer string `json:"device_id"`
	Driver  string `json:"driver"`
	Reason  string `json:"reason"`
}

// presence is what a subscriber reads when a device becomes unreachable or
// reachable again.
//
// It carries the state and not the edge, so one frame is the whole answer: a
// reader that missed the frame before it is correct again on the next one, and
// a resume that replays two of them out of a window ends on the truth either
// way.
type presence struct {
	Printer   string `json:"device_id"`
	Driver    string `json:"driver"`
	Connected bool   `json:"connected"`
}

// linkReading is what a subscriber reads when what this server makes of a
// device's frames changes.
//
// The reading is nested under one member and is the same shape the listing
// carries, so a reader holding a device from the listing replaces that member
// and is correct, rather than rebuilding it field by field.
type linkReading struct {
	Printer string             `json:"device_id"`
	Driver  string             `json:"driver"`
	Link    httpapi.DeviceLink `json:"link"`
}

// issuesChange is what a subscriber reads when what the host has found wrong
// with a device changes. The issues are the listing's own shape, so a reader
// holding the device from the listing replaces that member, and the dialog is
// the one a module reads off the device record.
type issuesChange struct {
	Printer string                `json:"device_id"`
	Driver  string                `json:"driver"`
	Issues  []httpapi.DeviceIssue `json:"issues"`
	Dialog  *deviceapi.Dialog     `json:"dialog,omitempty"`
}

// storagesChange is what a subscriber reads when the storage devices a device
// reported change. It nests the same shape the storages route answers with, so a
// reader holding that answer replaces it whole.
type storagesChange struct {
	httpapi.DeviceStorages

	Printer string `json:"device_id"`
	Driver  string `json:"driver"`
}

// filesChange is what a subscriber reads when a read of one device folder
// begins or lands. It carries no rows: a reader reads the folder again with
// refresh off, which answers what this server holds.
type filesChange struct {
	Printer string `json:"device_id"`
	Driver  string `json:"driver"`
	Path    string `json:"path"`
	Reading bool   `json:"reading"`
	Failed  bool   `json:"failed"`
}

// recordChange is what a subscriber reads when the row this server stores about
// a device changed.
//
// It carries the row and names what moved. With the whole row, one frame is
// the whole answer: a reader that missed the frame before it is correct again
// on the next, and a history provider storing the payload whole holds the
// record as the store holds it. With the changed list, a reader needs no diff.
//
// The state tree is not here. Every path in it is already published one patch
// at a time as com.prusa.connect.device.telemetry.sampled, and a history
// provider stores a sample under that path as the metric name. Repeating the
// tree would put every value in the store twice, once as a sample row and once
// inside a fact payload.
//
// last_seen is carried and never triggers. It moves on every telemetry frame,
// so a frame per movement would be one durable event per device per flush, and
// the frame that moved it is already on the bus.
type recordChange struct {
	DeviceID string `json:"device_id"`
	// Name is what an operator called this device, and is empty for one nobody
	// has named. It is the one member here beside the tags a person wrote
	// rather than a machine reported.
	Name      string    `json:"name"`
	Driver    string    `json:"driver"`
	Vendor    string    `json:"vendor"`
	Model     string    `json:"model"`
	Serial    string    `json:"serial"`
	UnitUID   string    `json:"unit_uid"`
	Firmware  string    `json:"firmware"`
	Departure string    `json:"departure"`
	Connected bool      `json:"connected"`
	LastSeen  time.Time `json:"last_seen"`

	// Tags are the operator's own, in the operator's order. An empty list is
	// an empty array rather than null, so a consumer needs no null check.
	Tags []string `json:"tags"`

	Changed []string `json:"changed"`
}

// sample is one node of the addressable state tree, carried as the printer
// encoded it. Re-encoding a number here would change it.
type sample struct {
	Path  string          `json:"path"`
	Value json.RawMessage `json:"value"`
}

// arrivalOf renders a device's arrival. A payload that cannot be encoded is
// published as an empty object rather than dropped: the event itself is what a
// subscriber is waiting for, and every field in it came from this process.
func arrivalOf(entry *device, resolution capability.Resolution) json.RawMessage {
	return encoded(arrival{
		Printer:        entry.id,
		Driver:         entry.driver,
		Vendor:         entry.identity.Vendor,
		Model:          entry.identity.Model,
		Firmware:       firmwareText(entry.identity.Firmware),
		Serial:         entry.identity.Serial,
		Capabilities:   len(resolution.Capabilities),
		CapabilityHash: resolution.Digest,
		Epoch:          resolution.Epoch,
		ProfileMatched: resolution.ProfileMatched,
	})
}

func departureOf(entry *device) json.RawMessage {
	reason := departureSessionEnded
	if went := entry.departure(); went != nil {
		reason = went.Reason
	}

	return encoded(departure{Printer: entry.id, Driver: entry.driver, Reason: reason})
}

func presenceOf(entry *device, connected bool) json.RawMessage {
	return encoded(presence{Printer: entry.id, Driver: entry.driver, Connected: connected})
}

func storagesChangeOf(entry *device, storages httpapi.DeviceStorages) json.RawMessage {
	return encoded(storagesChange{Printer: entry.id, Driver: entry.driver, DeviceStorages: storages})
}

// filesChangePath bounds a files.changed frame whatever folder a caller asked
// for.
const filesChangePath = 1 << 10

func filesChangeOf(entry *device, path string, reading, failed bool) json.RawMessage {
	return encoded(filesChange{
		Printer: entry.id, Driver: entry.driver,
		Path: machinetext.Clip(path, filesChangePath), Reading: reading, Failed: failed,
	})
}

func issuesChangeOf(entry *device, issues []httpapi.DeviceIssue, dialog *deviceapi.Dialog) json.RawMessage {
	if issues == nil {
		issues = []httpapi.DeviceIssue{}
	}

	return encoded(issuesChange{Printer: entry.id, Driver: entry.driver, Issues: issues, Dialog: dialog})
}

func linkOf(entry *device, reading httpapi.DeviceLink) json.RawMessage {
	return encoded(linkReading{Printer: entry.id, Driver: entry.driver, Link: reading})
}

// recordChangeOf renders one written record and the fields that moved. The
// record is the one that reached the store, so what a subscriber reads is what
// a second reader of that table would read.
//
// connected is the exception, and it is passed rather than read off the record
// because no column holds it. It is the presence the entry
// answered with as the record was taken, under the same lock, so the frame and
// the row it reports are one reading and not two.
func recordChangeOf(record devicestore.DeviceRecord, connected bool, moved recordFields) json.RawMessage {
	return encoded(recordChange{
		DeviceID:  record.DeviceID,
		Name:      record.Name,
		Driver:    record.Driver,
		Vendor:    record.Vendor,
		Model:     record.Model,
		Serial:    record.Serial,
		UnitUID:   record.UnitUID,
		Firmware:  record.Firmware,
		Departure: record.Departure,
		Connected: connected,
		LastSeen:  record.LastSeen.UTC(),
		Tags:      nonNilTags(record.Tags),
		Changed:   moved.names(),
	})
}

func sampleOf(patch connectorapi.StatePatch) json.RawMessage {
	return encoded(sample{Path: patch.Path, Value: patch.Value})
}

func encoded(payload any) json.RawMessage {
	data, err := json.Marshal(payload)
	if err != nil {
		return json.RawMessage(`{}`)
	}

	return data
}
