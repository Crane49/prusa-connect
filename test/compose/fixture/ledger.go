// Package composefixture is an event sink that lives outside the core module.
//
// It exists to be composed. ADR-20260901-b2z rule 1 and ADR-20260901-7t4 owe one gate jointly:
// a module this repository does not compile, linked into a binary by the build
// configuration alone, registering through a published contract and served
// under the namespace its own module path derives. Every claim in that sentence
// is false unless something actually builds it, so this is the something.
//
// It writes each event to a file, because a sink that did nothing observable
// would prove linking and not dispatch.
package composefixture

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"

	"github.com/prusa3d/prusa-connect/sdk/eventsapi"
	"github.com/prusa3d/prusa-connect/sdk/extension"
)

// Type is what an operator writes as the bare name of this sink. The qualified
// name the host serves it under is this with the namespace derived from this
// module's path in front, and nothing here chooses that half.
const Type = "ledger"

// ErrMissingPath rejects a spec with nowhere to write rather than starting a
// sink that silently records nothing.
var ErrMissingPath = errors.New("composefixture: config.path is required")

//nolint:gochecknoinits // registration has to happen before main; the host reaches it through a blank import.
func init() { eventsapi.Register(Type, doc(), New) }

// doc is what the published reference says about this sink.
func doc() extension.Doc {
	return extension.Doc{
		Summary: "Appends each domain event to a file, one JSON object per line.",
		Description: "A sink from outside the core module, built to prove that a binary composed " +
			"from a build configuration registers, dispatches and is served under the namespace " +
			"its own module path derives.",
		Version:   "1.0.0",
		Contract:  eventsapi.Contract,
		Stability: extension.StabilityExperimental,
		Options: []extension.Option{
			{
				Key:      "path",
				Type:     extension.OptionString,
				Required: true,
				Summary:  "The file each event is appended to.",
			},
		},
	}
}

// Ledger is the sdk/eventsapi.Processor this package registers.
type Ledger struct {
	mutex sync.Mutex
	file  *os.File
}

// New builds a Ledger from its spec.
func New(spec eventsapi.ProviderSpec) (eventsapi.Processor, error) {
	path, _ := spec.Config["path"].(string)
	if path == "" {
		return nil, ErrMissingPath
	}

	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("composefixture: open %s: %w", path, err)
	}

	return &Ledger{file: file}, nil
}

// PrinterStateChanged records one printer state fact.
func (l *Ledger) PrinterStateChanged(state eventsapi.PrinterState) { l.record("printer.state", state) }

// PrinterLabelsChanged records one printer label fact.
func (l *Ledger) PrinterLabelsChanged(labels eventsapi.PrinterLabels) {
	l.record("printer.labels", labels)
}

// PrinterDeleted records one removed printer.
func (l *Ledger) PrinterDeleted(id string) { l.record("printer.deleted", id) }

// JobUpdated records one job fact.
func (l *Ledger) JobUpdated(job eventsapi.JobInfo) { l.record("job.updated", job) }

// LabelDeleted records one removed label.
func (l *Ledger) LabelDeleted(id string) { l.record("label.deleted", id) }

// FactObserved records one durable bus event, whole. The fixture exists to be
// read back by an assertion, so the envelope goes in unpicked.
func (l *Ledger) FactObserved(fact eventsapi.Fact) { l.record("fact.observed", fact) }

// Close closes the file. The contract says a Close is called once.
func (l *Ledger) Close() error {
	l.mutex.Lock()
	defer l.mutex.Unlock()

	if err := l.file.Close(); err != nil {
		return fmt.Errorf("composefixture: close: %w", err)
	}

	return nil
}

// record appends one line. The contract forbids blocking and panicking, and a
// write to a local file under a mutex is neither for the volume a fixture sees.
// A failure is dropped rather than logged, because this adapter has no channel
// to the host's logger and a sink that cannot write is the host's to supervise.
func (l *Ledger) record(kind string, data any) {
	line, err := json.Marshal(map[string]any{"kind": kind, "data": data})
	if err != nil {
		return
	}

	l.mutex.Lock()
	defer l.mutex.Unlock()

	_, _ = l.file.Write(append(line, '\n'))
}
