package app

import (
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/upgrade"
)

// TestStartupGuardRestoresAnInterruptedSwap holds with upgrades off too: a
// server switched off after an upgrade began still rolls it back at start.
func TestStartupGuardRestoresAnInterruptedSwap(t *testing.T) {
	t.Parallel()

	for name, configuration := range map[string]string{
		"upgrades on":  "",
		"upgrades off": "core:\n  upgrade:\n    enabled: false\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			restoresAnInterruptedSwap(t, configuration)
		})
	}
}

func restoresAnInterruptedSwap(t *testing.T, configuration string) {
	t.Helper()

	root := t.TempDir()
	configPath := filepath.Join(root, "config", "config.yaml")
	writeGuardFile(t, configPath, configuration)

	core, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}

	current := filepath.Join(root, "bin", "core")
	previous := current + ".previous"
	staged := filepath.Join(root, "config", "config.d", "upgrade", "staged", "failed")
	data := filepath.Join(root, "data.json")
	writeGuardFile(t, current, "new")
	writeGuardFile(t, previous, "old")
	writeGuardFile(t, data, "before")

	backups := backupManager(core, "")
	if _, err = backups.Create(t.Context(), upgrade.BackupPlan{
		ID: "transaction", Files: []upgrade.FileSource{{Name: "data", Path: data}},
	}); err != nil {
		t.Fatal(err)
	}
	writeGuardFile(t, data, "migrated")

	store := upgrade.NewStore(upgradeJournalPath(core))
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	journal, err := upgrade.NewJournal("transaction", "1.0.0", "1.1.0", staged, now)
	if err != nil {
		t.Fatal(err)
	}
	journal.CurrentBinary = current
	if err = store.Save(journal); err != nil {
		t.Fatal(err)
	}
	journal, err = store.Transition(journal, upgrade.StateVerified, now, nil)
	if err != nil {
		t.Fatal(err)
	}
	journal, err = store.Transition(journal, upgrade.StatePreflighted, now, nil)
	if err != nil {
		t.Fatal(err)
	}
	journal, err = store.Transition(journal, upgrade.StateRehearsed, now, nil)
	if err != nil {
		t.Fatal(err)
	}
	journal, err = store.Transition(journal, upgrade.StateDraining, now, nil)
	if err != nil {
		t.Fatal(err)
	}
	journal, err = store.Transition(journal, upgrade.StateBackedUp, now, func(next *upgrade.Journal) {
		next.BackupID = "transaction"
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.Transition(journal, upgrade.StateSwapped, now, func(next *upgrade.Journal) {
		next.PreviousBinary = previous
	}); err != nil {
		t.Fatal(err)
	}

	restart, err := RecoverUpgrade(t.Context(), configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !restart {
		t.Fatal("restored binary did not request a restart")
	}
	if got := readGuardFile(t, current); got != "old" {
		t.Fatalf("current binary is %q", got)
	}
	if got := readGuardFile(t, data); got != "before" {
		t.Fatalf("restored data is %q", got)
	}
}

func writeGuardFile(t *testing.T, path, content string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o700); err != nil {
		t.Fatal(err)
	}
}

func readGuardFile(t *testing.T, path string) string {
	t.Helper()

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	return string(content)
}

func TestTrialStartsTheCandidateWithTheServersFlags(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("the candidate is a shell script")
	}

	root := t.TempDir()
	recorded := filepath.Join(root, "arguments")
	candidate := filepath.Join(root, "core")
	writeGuardFile(t, candidate, "#!/bin/sh\necho \"$@\" > "+recorded+"\nexit 1\n")

	options := Options{
		ConfigPath:  filepath.Join(root, "config.yaml"),
		Overlay:     filepath.Join(root, "resources.json"),
		APIAddr:     "127.0.0.1:8100",
		PrinterAddr: "127.0.0.1:8101",
		LogLevel:    "info",
		Stderr:      io.Discard,
		// The candidate is a shell script that opens no socket, and the gate
		// over every Options literal still asks for loopback.
		LoopbackOnly: true,
	}
	err := trialUpgrade(t.Context(), options, time.Minute, upgrade.Journal{CurrentBinary: candidate})
	if err == nil {
		t.Fatal("a candidate that stopped passed its trial")
	}

	want := "core -config " + options.ConfigPath + " -overlay " + options.Overlay +
		" -addr 127.0.0.1:8100 -printer-addr 127.0.0.1:8101 -log-level info"
	if got := strings.TrimSpace(readGuardFile(t, recorded)); got != want {
		t.Fatalf("candidate started with %q, want %q", got, want)
	}
}

func TestTrialArgumentsLeaveOutFlagsTheServerWasNotGiven(t *testing.T) {
	t.Parallel()

	got := strings.Join(trialArguments(Options{ConfigPath: "config/config.yaml", LoopbackOnly: true}), " ")
	if got != "core -config config/config.yaml" {
		t.Fatalf("arguments are %q", got)
	}
}
