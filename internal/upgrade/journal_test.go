package upgrade_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/prusa3d/prusa-connect/internal/upgrade"
)

func TestJournalPersistsEveryTransition(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 25, 12, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "upgrade", "journal.json")
	store := upgrade.NewStore(path)

	journal, err := upgrade.NewJournal("u-1", "1.0.0", "1.1.0", "staged/1.1.0/core", now)
	if err != nil {
		t.Fatalf("new journal: %v", err)
	}

	if err = store.Save(journal); err != nil {
		t.Fatalf("save staged journal: %v", err)
	}

	_, err = store.Transition(journal, upgrade.StateVerified, now.Add(time.Second), nil)
	if err != nil {
		t.Fatalf("record verified: %v", err)
	}

	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("load verified journal: %v", err)
	}

	if loaded.State != upgrade.StateVerified {
		t.Errorf("loaded state is %q, expected %q", loaded.State, upgrade.StateVerified)
	}

	if loaded.Revision != 2 {
		t.Errorf("loaded revision is %d, expected 2", loaded.Revision)
	}

	if !loaded.UpdatedAt.Equal(now.Add(time.Second)) {
		t.Errorf("loaded update time is %s, expected %s", loaded.UpdatedAt, now.Add(time.Second))
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat journal: %v", err)
	}

	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("journal mode is %o, expected 600", got)
	}
}

func TestJournalRefusesSkippedAndUnknownStates(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 25, 12, 0, 0, 0, time.UTC)
	store := upgrade.NewStore(filepath.Join(t.TempDir(), "journal.json"))

	journal, err := upgrade.NewJournal("u-1", "1.0.0", "1.1.0", "staged/core", now)
	if err != nil {
		t.Fatalf("new journal: %v", err)
	}

	if _, err = store.Transition(journal, upgrade.StateSwapped, now, nil); !errors.Is(err, upgrade.ErrJournal) {
		t.Fatalf("skipped transition failed with %v, expected %v", err, upgrade.ErrJournal)
	}

	journal.State = upgrade.State("invented")
	if err = store.Save(journal); !errors.Is(err, upgrade.ErrJournal) {
		t.Fatalf("unknown state failed with %v, expected %v", err, upgrade.ErrJournal)
	}
}

func TestInterruptedJournalCannotBeForgotten(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 25, 12, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "journal.json")
	store := upgrade.NewStore(path)

	journal, err := upgrade.NewJournal("u-1", "1.0.0", "1.1.0", "staged/core", now)
	if err != nil {
		t.Fatalf("new journal: %v", err)
	}

	for _, next := range []upgrade.State{
		upgrade.StateVerified,
		upgrade.StatePreflighted,
		upgrade.StateRehearsed,
		upgrade.StateDraining,
	} {
		journal, err = store.Transition(journal, next, now, nil)
		if err != nil {
			t.Fatalf("transition to %s: %v", next, err)
		}
	}

	if !journal.Interrupted() {
		t.Fatal("a draining journal was not classified as interrupted")
	}

	if err = store.Remove(journal); !errors.Is(err, upgrade.ErrJournal) {
		t.Fatalf("removing interrupted journal failed with %v, expected %v", err, upgrade.ErrJournal)
	}

	if _, err = store.Load(); err != nil {
		t.Fatalf("the refused remove did not keep the journal: %v", err)
	}
}

func TestMissingAndTornJournalsAreDifferentFailures(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "journal.json")
	store := upgrade.NewStore(path)

	if _, err := store.Load(); !errors.Is(err, upgrade.ErrNoJournal) {
		t.Fatalf("missing journal failed with %v, expected %v", err, upgrade.ErrNoJournal)
	}

	if err := os.WriteFile(path, []byte(`{"schema":1`), 0o600); err != nil {
		t.Fatalf("write torn journal: %v", err)
	}

	if _, err := store.Load(); !errors.Is(err, upgrade.ErrJournal) {
		t.Fatalf("torn journal failed with %v, expected %v", err, upgrade.ErrJournal)
	}
}

func TestJournalRefusesUnknownFieldsAndTrailingDocuments(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "journal.json")
	store := upgrade.NewStore(path)
	for _, content := range []string{
		`{"schema":1,"future":true}`,
		`{"schema":1} {"schema":1}`,
	} {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("write malformed journal: %v", err)
		}
		if _, err := store.Load(); !errors.Is(err, upgrade.ErrJournal) {
			t.Fatalf("journal %q failed with %v, expected %v", content, err, upgrade.ErrJournal)
		}
	}
}

func TestTerminalUpgradeIsAppendedToHistoryOnce(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 25, 12, 0, 0, 0, time.UTC)
	store := upgrade.NewStore(filepath.Join(t.TempDir(), "upgrade", "journal.json"))
	journal, err := upgrade.NewJournal("u-1", "1.0.0", "1.1.0", "staged/core", now)
	if err != nil {
		t.Fatalf("new journal: %v", err)
	}

	for _, next := range []upgrade.State{
		upgrade.StateVerified, upgrade.StatePreflighted, upgrade.StateRehearsed,
		upgrade.StateDraining, upgrade.StateBackedUp, upgrade.StateSwapped,
		upgrade.StateTrial, upgrade.StateCommitted,
	} {
		journal, err = store.Transition(journal, next, now.Add(time.Second), nil)
		if err != nil {
			t.Fatalf("transition to %s: %v", next, err)
		}
	}

	history, err := store.History()
	if err != nil {
		t.Fatalf("read history: %v", err)
	}
	if len(history) != 1 || history[0].State != upgrade.StateCommitted || history[0].ID != "u-1" {
		t.Fatalf("history is %#v, expected one committed u-1", history)
	}
}

// A crash in the middle of an append leaves a last line with no newline. It
// recorded nothing, so History drops it and the next transition still
// appends; a damaged line in the middle is still refused.
func TestHistorySurvivesATornLastLine(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 25, 12, 0, 0, 0, time.UTC)
	dir := filepath.Join(t.TempDir(), "upgrade")
	store := upgrade.NewStore(filepath.Join(dir, "journal.json"))
	journal, err := upgrade.NewJournal("u-1", "1.0.0", "1.1.0", "staged/core", now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.Transition(journal, upgrade.StateFailedBeforeSwap, now, nil); err != nil {
		t.Fatal(err)
	}
	history := filepath.Join(dir, "history.jsonl")
	file, err := os.OpenFile(history, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = file.WriteString(`{"schema":1,"id":"u-2","sta`); err != nil {
		t.Fatal(err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}

	records, err := store.History()
	if err != nil || len(records) != 1 {
		t.Fatalf("history is %d records, error %v", len(records), err)
	}

	next, err := upgrade.NewJournal("u-3", "1.0.0", "1.1.0", "staged/core", now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.Transition(next, upgrade.StateFailedBeforeSwap, now, nil); err != nil {
		t.Fatalf("a transition after a torn line failed: %v", err)
	}
	records, err = store.History()
	if err != nil || len(records) != 2 || records[1].ID != "u-3" {
		t.Fatalf("history is %#v, error %v", records, err)
	}

	if err = os.WriteFile(history, []byte("{broken\n"+`{"schema":1}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err = store.History(); err == nil {
		t.Fatal("a damaged complete line was accepted")
	}
}
