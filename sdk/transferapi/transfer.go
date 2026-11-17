// Package transferapi is the contract for reading back the record of a file
// this host moved from one of its storages onto a device.
//
// This package has no way to write a record. A history adapter builds the
// records from the transfer events of sdk/event. Records is optional: an
// adapter that keeps no such records does not implement it.
package transferapi

import (
	"context"
	"errors"
	"time"
)

// ErrNoRecords reports a history adapter that keeps no transfer records. It
// is not the same as an empty page.
var ErrNoRecords = errors.New("transferapi: this history store keeps no transfer records")

// The states of a transfer. The first two belong to a running transfer. The
// rest are final.
const (
	// StateQueued is a transfer this host accepted and has not yet offered.
	StateQueued = "queued"
	// StateSending is a transfer the device accepted and is fetching.
	StateSending = "sending"
	// StateDone is a transfer the device reported finished.
	StateDone = "done"
	// StateFailed is a transfer that could not be carried out.
	StateFailed = "failed"
	// StateCancelled is a transfer somebody stopped.
	StateCancelled = "cancelled"
	// StateInterrupted is a transfer that was running when the server stopped.
	StateInterrupted = "interrupted"
)

// Record is one transfer as the store kept it. The times are Unix
// milliseconds. EndedMS is zero for a transfer that has not ended.
type Record struct {
	// TransferID is the host-minted id, a ULID.
	TransferID string `json:"transfer_id"`
	// Device is the rendered reference of the device the file went to.
	Device string `json:"device"`
	// Storage and Source are where the bytes came from.
	Storage string `json:"storage"`
	Source  string `json:"source"`
	// Destination is where the file lands on the device.
	Destination string `json:"destination"`
	// BytesTotal is the file's length and BytesSent is how much of it left.
	BytesTotal int64 `json:"bytes_total"`
	BytesSent  int64 `json:"bytes_sent"`
	// State is one of the State constants.
	State string `json:"state"`
	// Reason is why a transfer ended the way it did, empty for one that
	// finished, and FaultCode is the host's own code for the failure.
	Reason    string `json:"reason,omitempty"`
	FaultCode string `json:"fault_code,omitempty"`
	// Warning is what the driver could not confirm about a transfer that
	// finished, such as whether the file is on the device's storage. It is
	// not a failure. A record read back from a store has none.
	Warning string `json:"warning,omitempty"`
	// StartedMS and EndedMS are Unix milliseconds.
	StartedMS int64 `json:"started_ms"`
	EndedMS   int64 `json:"ended_ms,omitempty"`
}

// Running reports whether the record is one that has not settled.
func (r Record) Running() bool {
	return r.State == StateQueued || r.State == StateSending
}

// Started renders the start as a time, for a caller that shows it.
func (r Record) Started() time.Time { return time.UnixMilli(r.StartedMS) }

// Query is one page of transfer records.
type Query struct {
	// Device is the rendered reference to read. An empty value reads every
	// device.
	Device string
	// Limit is how many rows to return, newest first. Zero takes the store's
	// own default.
	Limit int
	// RunningOnly restricts the answer to transfers that have not ended.
	RunningOnly bool
}

// Records is the ability to read the transfer records.
type Records interface {
	// Transfers returns the records matching the query, newest first.
	Transfers(ctx context.Context, query Query) ([]Record, error)
}

// Of returns store as Records, and false when store does not implement it.
func Of(store any) (Records, bool) {
	records, ok := store.(Records)

	return records, ok
}
