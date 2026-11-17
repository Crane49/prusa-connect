// Package upgrade holds the durable transaction that replaces a core release.
package upgrade

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/prusa3d/prusa-connect/internal/kernel/atomicfile"
)

const (
	journalSchema = 1
	privateDir    = 0o700
	privateFile   = 0o600
)

// ErrNoJournal reports a deployment with no prepared upgrade.
var ErrNoJournal = errors.New("upgrade: no journal")

// ErrJournal reports bytes or a transition that do not follow the journal
// contract.
var ErrJournal = errors.New("upgrade: invalid journal")

// State is the durable point an upgrade has reached.
type State string

const (
	StateStaged           State = "staged"
	StateVerified         State = "verified"
	StatePreflighted      State = "preflighted"
	StateRehearsed        State = "rehearsed"
	StateScheduled        State = "scheduled"
	StateDraining         State = "draining"
	StateBackedUp         State = "backed-up"
	StateSwapped          State = "swapped"
	StateTrial            State = "trial"
	StateCommitted        State = "committed"
	StateRollingBack      State = "rolling-back"
	StateRolledBack       State = "rolled-back"
	StateFailedBeforeSwap State = "failed-before-swap"
)

// Journal is the durable record of one upgrade transaction. Paths name files
// already kept under the deployment's private upgrade directory.
//
//nolint:golines // OpenAPI field tags stay beside the fields they describe.
type Journal struct {
	Schema             int               `json:"schema"                    doc:"The durable journal contract version."`
	ID                 string            `json:"id"                        doc:"The unique upgrade transaction identifier."`
	State              State             `json:"state"                     doc:"The current durable upgrade state."`
	FromVersion        string            `json:"from_version"              doc:"The server version active before this transaction."`
	ToVersion          string            `json:"to_version"                doc:"The candidate version selected by this transaction."`
	StagedBinary       string            `json:"staged_binary"             doc:"The private path holding the candidate executable."`
	StagedRoot         string            `json:"staged_root,omitempty"     doc:"The private directory holding every artifact in an offline bundle."`
	CurrentBinary      string            `json:"current_binary,omitempty"  doc:"The stable path used to start the server."`
	PreviousBinary     string            `json:"previous_binary,omitempty" doc:"The rollback path holding the previous executable."`
	BackupID           string            `json:"backup_id,omitempty"       doc:"The snapshot associated with this transaction."`
	BackupReady        bool              `json:"backup_ready,omitempty"    doc:"Whether the rollback snapshot completed before live mutation."`
	RequestedBy        string            `json:"requested_by,omitempty"    doc:"The principal that staged this transaction."`
	Online             bool              `json:"online,omitempty" doc:"Whether the server downloaded the candidate from the release catalogue. An online candidate is always signed."`
	Schedule           string            `json:"schedule,omitempty"        doc:"The authorized apply schedule."`
	ScheduledFor       *time.Time        `json:"scheduled_for,omitempty"   doc:"The authorized apply instant for an at schedule."`
	ForcePrinting      bool              `json:"force_printing,omitempty"  doc:"Whether the operator allowed pausing active prints."`
	Verified           *Verified         `json:"verified,omitempty"        doc:"The signed identity of the candidate executable."`
	BundleKeyID        string            `json:"bundle_key_id,omitempty"   doc:"The trusted key that signed an offline bundle manifest."`
	Extensions         []StagedExtension `json:"extensions,omitempty"      doc:"The verified extension archives staged with the core executable."`
	TrialRequired      []string          `json:"trial_extensions,omitempty" doc:"Extensions required during candidate trial."`
	ExtensionReviews   []ExtensionReview `json:"extension_reviews,omitempty" doc:"What each bundled extension asks for and how it differs from the installed one, as preparation found it."`
	ExtensionsApproved bool              `json:"extensions_approved,omitempty" doc:"Whether the operator approved the new asks and older versions the extension reviews name."`
	Metadata           *BinaryMetadata   `json:"metadata,omitempty"        doc:"The runtime metadata reported by the candidate."`
	Preflight          *PreflightReport  `json:"preflight,omitempty"       doc:"The checks completed before migration rehearsal."`
	Rehearsal          *RehearsalReport  `json:"rehearsal,omitempty"       doc:"The migration rehearsal result."`
	Revision           uint64            `json:"revision"                  doc:"The monotonic journal revision."`
	UpdatedAt          time.Time         `json:"updated_at"                doc:"The instant the journal last changed."`
	Failure            string            `json:"failure,omitempty"         doc:"The terminal or rollback failure reason."`
	ExternalRestore    []string          `json:"external_restore,omitempty" doc:"The PostgreSQL databases a rollback left for the operator to restore from their own backup."`
}

// ExtensionReview is what preparation found about one bundled extension.
type ExtensionReview struct {
	Module           string          `json:"module"                      doc:"The extension module path."`
	Version          string          `json:"version"                     doc:"The version in the bundle."`
	InstalledVersion string          `json:"installed_version,omitempty" doc:"The version installed now, empty for a new extension."`
	Asks             []string        `json:"asks,omitempty"              doc:"Everything the bundled release asks for, one request per line."`
	NewAsks          []string        `json:"new_asks,omitempty"          doc:"The requests the installed release did not make. Applying needs the operator's approval for them."`
	Downgrade        bool            `json:"downgrade,omitempty"         doc:"Whether the bundled version is older than the installed one. Applying needs the operator's approval for it."`
	Shown            json.RawMessage `json:"shown,omitempty"             doc:"The asks shown here in the form a module declaration records them. Apply grants no more than this."`
}

// NeedsApproval reports whether applying this extension needs the operator to
// approve what the review names.
func (r ExtensionReview) NeedsApproval() bool {
	return len(r.NewAsks) > 0 || r.Downgrade
}

func (r ExtensionReview) approvalReason() string {
	var reasons []string
	if r.Downgrade {
		reasons = append(reasons, "is older than installed "+r.InstalledVersion)
	}
	if len(r.NewAsks) > 0 {
		reasons = append(reasons, "asks for "+strings.Join(r.NewAsks, ", "))
	}

	return strings.Join(reasons, " and ")
}

// NewJournal starts a transaction after its release bytes have landed.
func NewJournal(id, fromVersion, toVersion, stagedBinary string, now time.Time) (Journal, error) {
	journal := Journal{
		Schema:       journalSchema,
		ID:           id,
		State:        StateStaged,
		FromVersion:  fromVersion,
		ToVersion:    toVersion,
		StagedBinary: stagedBinary,
		Revision:     1,
		UpdatedAt:    now.UTC(),
	}

	if err := journal.Validate(); err != nil {
		return Journal{}, err
	}

	return journal, nil
}

// Validate checks the part of the journal both an old and a new binary must
// agree on before either one acts on it.
func (j Journal) Validate() error {
	switch {
	case j.Schema != journalSchema:
		return fmt.Errorf("%w: schema is %d, expected %d", ErrJournal, j.Schema, journalSchema)
	case j.ID == "":
		return fmt.Errorf("%w: id is empty", ErrJournal)
	case !slices.Contains(states, j.State):
		return fmt.Errorf("%w: state is %q", ErrJournal, j.State)
	case j.FromVersion == "":
		return fmt.Errorf("%w: from_version is empty", ErrJournal)
	case j.ToVersion == "":
		return fmt.Errorf("%w: to_version is empty", ErrJournal)
	case j.StagedBinary == "":
		return fmt.Errorf("%w: staged_binary is empty", ErrJournal)
	case j.Revision == 0:
		return fmt.Errorf("%w: revision is zero", ErrJournal)
	case j.UpdatedAt.IsZero():
		return fmt.Errorf("%w: updated_at is empty", ErrJournal)
	}

	return nil
}

// Interrupted reports a state whose work may have touched the current binary
// or live data and must be recovered before an ordinary start.
func (j Journal) Interrupted() bool {
	return slices.Contains([]State{
		StateDraining,
		StateBackedUp,
		StateSwapped,
		StateTrial,
		StateRollingBack,
	}, j.State)
}

// Store reads and publishes one journal.
type Store struct {
	path string
}

// NewStore opens the journal at path. It creates no file until Save or
// Transition publishes one.
func NewStore(path string) Store {
	return Store{path: path}
}

// Path returns the file this store owns.
func (s Store) Path() string {
	return s.path
}

// Load reads and validates the durable journal.
func (s Store) Load() (Journal, error) {
	content, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return Journal{}, ErrNoJournal
	}

	if err != nil {
		return Journal{}, fmt.Errorf("upgrade: read journal: %w", err)
	}

	var journal Journal
	if err = decodeJournal(content, &journal); err != nil {
		return Journal{}, fmt.Errorf("%w: decode: %w", ErrJournal, err)
	}

	if err = journal.Validate(); err != nil {
		return Journal{}, err
	}

	return journal, nil
}

// Save publishes a complete journal atomically.
func (s Store) Save(journal Journal) error {
	if err := journal.Validate(); err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(s.path), privateDir); err != nil {
		return fmt.Errorf("upgrade: create journal directory: %w", err)
	}

	content, err := json.MarshalIndent(journal, "", "  ")
	if err != nil {
		return fmt.Errorf("upgrade: encode journal: %w", err)
	}

	content = append(content, '\n')

	if err = atomicfile.Write(s.path, content, privateFile); err != nil {
		return fmt.Errorf("upgrade: publish journal: %w", err)
	}

	return nil
}

// Transition records the next state before its caller performs that state's
// work. change fills fields that become true at the same transition.
func (s Store) Transition(
	journal Journal,
	next State,
	now time.Time,
	change func(*Journal),
) (Journal, error) {
	if !allowedTransition(journal.State, next) {
		return Journal{}, fmt.Errorf("%w: transition from %s to %s", ErrJournal, journal.State, next)
	}

	if change != nil {
		change(&journal)
	}

	journal.State = next
	journal.Revision++
	journal.UpdatedAt = now.UTC()

	if err := s.Save(journal); err != nil {
		return Journal{}, err
	}
	if slices.Contains([]State{StateCommitted, StateRolledBack, StateFailedBeforeSwap}, journal.State) {
		if err := s.appendHistory(journal); err != nil {
			return Journal{}, err
		}
	}

	return journal, nil
}

// History returns completed attempts in append order. A malformed line is
// refused so an audit trail can never be silently truncated.
func (s Store) History() ([]Journal, error) {
	content, err := os.ReadFile(s.historyPath())
	if errors.Is(err, os.ErrNotExist) {
		return []Journal{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("upgrade: read history: %w", err)
	}

	// A line that ends without a newline is a write cut short by a crash. It
	// recorded nothing, so it is dropped; any other bad line is refused.
	torn := len(content) > 0 && content[len(content)-1] != '\n'
	lines := bytes.Split(bytes.TrimSpace(content), []byte{'\n'})
	history := make([]Journal, 0, len(lines))
	for index, line := range lines {
		if len(line) == 0 {
			continue
		}

		var journal Journal
		if err = decodeJournal(line, &journal); err != nil {
			if torn && index == len(lines)-1 {
				break
			}

			return nil, fmt.Errorf("upgrade: decode history line %d: %w", index+1, err)
		}
		if err = journal.Validate(); err != nil {
			return nil, fmt.Errorf("upgrade: validate history line %d: %w", index+1, err)
		}
		history = append(history, journal)
	}

	return history, nil
}

func decodeJournal(content []byte, journal *Journal) error {
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(journal); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("a second JSON value follows the journal")
		}

		return err
	}

	return nil
}

func (s Store) appendHistory(journal Journal) error {
	history, err := s.History()
	if err != nil {
		return err
	}
	if slices.ContainsFunc(history, func(record Journal) bool {
		return record.ID == journal.ID && record.Revision == journal.Revision
	}) {
		return nil
	}

	history = append(history, journal)
	var content bytes.Buffer
	for _, record := range history {
		line, encodeErr := json.Marshal(record)
		if encodeErr != nil {
			return fmt.Errorf("upgrade: encode history: %w", encodeErr)
		}
		content.Write(line)
		content.WriteByte('\n')
	}
	if err = os.MkdirAll(filepath.Dir(s.path), privateDir); err != nil {
		return fmt.Errorf("upgrade: create history directory: %w", err)
	}
	// The whole file is replaced atomically, so a crash leaves the old
	// history or the new one and never half a line.
	if err = atomicfile.Write(s.historyPath(), content.Bytes(), privateFile); err != nil {
		return fmt.Errorf("upgrade: append history: %w", err)
	}

	return nil
}

func (s Store) historyPath() string {
	return filepath.Join(filepath.Dir(s.path), "history.jsonl")
}

// Remove forgets a transaction that has not touched the current binary or live
// data.
func (s Store) Remove(journal Journal) error {
	if journal.Interrupted() {
		return fmt.Errorf("%w: cannot remove journal in state %s", ErrJournal, journal.State)
	}

	if err := os.Remove(s.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("upgrade: remove journal: %w", err)
	}

	return nil
}

var states = []State{
	StateStaged,
	StateVerified,
	StatePreflighted,
	StateRehearsed,
	StateScheduled,
	StateDraining,
	StateBackedUp,
	StateSwapped,
	StateTrial,
	StateCommitted,
	StateRollingBack,
	StateRolledBack,
	StateFailedBeforeSwap,
}

var transitions = map[State][]State{
	StateStaged:           {StateVerified, StateFailedBeforeSwap},
	StateVerified:         {StatePreflighted, StateFailedBeforeSwap},
	StatePreflighted:      {StateRehearsed, StateFailedBeforeSwap},
	StateRehearsed:        {StateScheduled, StateDraining, StateFailedBeforeSwap},
	StateScheduled:        {StateDraining, StateFailedBeforeSwap},
	StateDraining:         {StateBackedUp, StateRollingBack},
	StateBackedUp:         {StateSwapped, StateRollingBack},
	StateSwapped:          {StateTrial, StateRollingBack},
	StateTrial:            {StateCommitted, StateRollingBack},
	StateRollingBack:      {StateRolledBack},
	StateCommitted:        nil,
	StateRolledBack:       nil,
	StateFailedBeforeSwap: nil,
}

func allowedTransition(from, to State) bool {
	return slices.Contains(transitions[from], to)
}
