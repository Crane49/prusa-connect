package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/kernel/atomicfile"
	"github.com/prusa3d/prusa-connect/internal/upgrade"
)

const upgradeTrialEnvironment = "PRUSA_CONNECT_LOCAL_UPGRADE_TRIAL"
const upgradeTrialExtensionsEnvironment = "PRUSA_CONNECT_LOCAL_UPGRADE_TRIAL_EXTENSIONS"

const (
	restoreRequestPerm = 0o600
	trialProbeInterval = 250 * time.Millisecond
	trialStopTimeout   = 5 * time.Second
)

// RecoverUpgrade completes rollback before an ordinary start reaches a
// database. It reports whether the restored binary must replace this process.
func RecoverUpgrade(ctx context.Context, configPath string) (bool, error) {
	core, err := loadCore(configPath)
	if err != nil {
		return false, err
	}

	store := upgrade.NewStore(upgradeJournalPath(core))
	journal, err := store.Load()
	if errors.Is(err, upgrade.ErrNoJournal) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !journal.Interrupted() {
		return false, nil
	}
	if journal.State == upgrade.StateTrial && os.Getenv(upgradeTrialEnvironment) == journal.ID {
		return false, nil
	}

	plan, err := backupPlan(
		configPath, core, "", upgrade.PostgresMode(core.Upgrade.PostgresBackup), true,
	)
	if err != nil {
		return false, err
	}

	runner, err := upgrade.NewRunner(upgrade.RunnerOptions{
		Store:   store,
		Backups: backupManager(core, ""),
		BackupPlan: func(upgrade.Journal) (upgrade.BackupPlan, error) {
			return upgrade.BackupPlan{}, nil
		},
		Trial:       func(context.Context, upgrade.Journal) error { return nil },
		RestoreDSNs: postgresRestoreTargets(plan),
		Logger:      slog.New(slog.NewTextHandler(os.Stderr, nil)),
	})
	if err != nil {
		return false, err
	}

	_, err = runner.Recover(ctx)
	if err != nil && !errors.Is(err, context.Canceled) {
		// Recover returns the interruption as its cause after a complete
		// rollback. The durable state decides whether recovery succeeded.
		recovered, loadErr := store.Load()
		if loadErr != nil || recovered.State != upgrade.StateRolledBack {
			return false, errors.Join(err, loadErr)
		}
	}

	return journal.PreviousBinary != "", nil
}

func upgradeJournalPath(core config.Core) string {
	return filepath.Join(filepath.Dir(core.Overlay), "upgrade", "journal.json")
}

func backupRestoreRequestPath(core config.Core) string {
	return filepath.Join(filepath.Dir(core.Overlay), "upgrade", "restore-request")
}

func recordBackupRestore(core config.Core, id string) error {
	if id == "" || filepath.Base(id) != id {
		return errors.New("backup restore id is invalid")
	}
	if err := os.MkdirAll(filepath.Dir(backupRestoreRequestPath(core)), 0o700); err != nil {
		return fmt.Errorf("create backup restore request directory: %w", err)
	}

	return atomicfile.Write(backupRestoreRequestPath(core), []byte(id+"\n"), restoreRequestPerm)
}

// ApplyPreparedRestore restores only after Run has closed every database and
// removes the durable request only after every verified file is in place.
func ApplyPreparedRestore(ctx context.Context, configPath string) error {
	core, err := loadCore(configPath)
	if err != nil {
		return err
	}

	request := backupRestoreRequestPath(core)
	content, err := os.ReadFile(request)
	if err != nil {
		return fmt.Errorf("read backup restore request: %w", err)
	}
	id := strings.TrimSpace(string(content))
	if id == "" || filepath.Base(id) != id {
		return errors.New("backup restore request is invalid")
	}

	journal, journalErr := upgrade.NewStore(upgradeJournalPath(core)).Load()
	if journalErr != nil && !errors.Is(journalErr, upgrade.ErrNoJournal) {
		return journalErr
	}

	plan, err := backupPlan(
		configPath, core, "", upgrade.PostgresMode(core.Upgrade.PostgresBackup), true,
	)
	if err != nil {
		return err
	}
	external, err := backupManager(core, "").RestoreReporting(ctx, id, postgresRestoreTargets(plan))
	if err != nil {
		return err
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	for _, entry := range external {
		logger.WarnContext(ctx,
			"the backup was restored without this PostgreSQL database; restore it from the external backup",
			"database", entry.Name, "mode", string(entry.Mode), "backup", id)
	}
	if journalErr == nil && journal.BackupID == id && journal.PreviousBinary != "" {
		if err = upgrade.RestorePreviousBinary(
			journal.CurrentBinary, journal.StagedBinary, journal.PreviousBinary,
		); err != nil {
			return err
		}
	}
	if err = os.Remove(upgradeJournalPath(core)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove restored upgrade journal: %w", err)
	}
	if err = os.Remove(request); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove backup restore request: %w", err)
	}

	return nil
}

// RecoverBackupRestore completes a restore request left by an interruption
// before ordinary startup opens any live database.
func RecoverBackupRestore(ctx context.Context, configPath string) (bool, error) {
	core, err := loadCore(configPath)
	if err != nil {
		return false, err
	}
	if _, err = os.Stat(backupRestoreRequestPath(core)); errors.Is(err, os.ErrNotExist) {
		return false, nil
	} else if err != nil {
		return false, fmt.Errorf("inspect backup restore request: %w", err)
	}

	return true, ApplyPreparedRestore(ctx, configPath)
}

// ApplyPreparedUpgrade runs only after Run has completely stopped. It snapshots
// the closed stores, swaps the executable, proves the candidate can become
// ready, and restores both data and executable if that proof fails.
//
// It names the executable to start next. When the upgrade fails and the
// journal rolled back, or stopped before the swap, that is the restored
// executable, and the caller starts it alongside the error.
func ApplyPreparedUpgrade(ctx context.Context, options Options) (string, error) {
	core, err := loadCore(options.ConfigPath)
	if err != nil {
		return "", err
	}

	plan, err := backupPlan(
		options.ConfigPath, core, "", upgrade.PostgresMode(core.Upgrade.PostgresBackup), true,
	)
	if err != nil {
		return "", err
	}

	runner, err := upgrade.NewRunner(upgrade.RunnerOptions{
		Store:   upgrade.NewStore(upgradeJournalPath(core)),
		Backups: backupManager(core, ""),
		BackupPlan: func(journal upgrade.Journal) (upgrade.BackupPlan, error) {
			return backupPlan(
				options.ConfigPath, core, journal.ToVersion,
				upgrade.PostgresMode(core.Upgrade.PostgresBackup), true,
			)
		},
		Activate: func(activateCtx context.Context, journal upgrade.Journal) error {
			return activateBundledExtensions(activateCtx, core, options.ConfigPath, journal)
		},
		Trial: func(trialCtx context.Context, journal upgrade.Journal) error {
			return trialUpgrade(trialCtx, options, core.Upgrade.TrialTimeout, journal)
		},
		RestoreDSNs: postgresRestoreTargets(plan),
		Logger:      slog.New(slog.NewTextHandler(options.stderr(), nil)),
	})
	if err != nil {
		return "", err
	}

	journal, err := runner.Apply(ctx)
	if err != nil {
		if journal.State == upgrade.StateRolledBack || journal.State == upgrade.StateFailedBeforeSwap {
			return journal.CurrentBinary, err
		}

		return "", err
	}

	return journal.CurrentBinary, nil
}

func trialUpgrade(ctx context.Context, options Options, timeout time.Duration, journal upgrade.Journal) error {
	if timeout <= 0 {
		timeout = config.DefaultUpgradeTrialTimeout
	}
	trialCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	command := exec.CommandContext( // #nosec G204 -- the signed journal fixes the executable path
		context.WithoutCancel(ctx), journal.CurrentBinary, trialArguments(options)...,
	)
	command.Env = append(
		os.Environ(),
		upgradeTrialEnvironment+"="+journal.ID,
		upgradeTrialExtensionsEnvironment+"="+strings.Join(journal.TrialRequired, ","),
	)
	command.Stdout = options.stderr()
	command.Stderr = options.stderr()
	if err := command.Start(); err != nil {
		return fmt.Errorf("start candidate: %w", err)
	}

	waited := make(chan error, 1)
	go func() { waited <- command.Wait() }()

	ticker := time.NewTicker(trialProbeInterval)
	defer ticker.Stop()

	var ready bool
	for !ready {
		select {
		case err := <-waited:
			return fmt.Errorf("candidate stopped before readiness: %w", err)
		case <-trialCtx.Done():
			_ = command.Process.Kill()
			<-waited

			return fmt.Errorf("candidate readiness: %w", trialCtx.Err())
		case <-ticker.C:
			probeCtx, probeCancel := context.WithTimeout(trialCtx, time.Second)
			err := Probe(probeCtx, options)
			probeCancel()
			ready = err == nil
		}
	}

	forcedStop := false
	if err := command.Process.Signal(os.Interrupt); err != nil {
		forcedStop = true
		_ = command.Process.Kill()
	}
	select {
	case err := <-waited:
		if err != nil && !forcedStop {
			return fmt.Errorf("stop candidate after readiness: %w", err)
		}
	case <-time.After(trialStopTimeout):
		_ = command.Process.Kill()
		<-waited
	}

	return nil
}

// trialArguments starts the candidate with the flags this server was started
// with. Without them it would bind the addresses in the file, and the
// readiness probe, which reads the flags, would wait for it on another one.
func trialArguments(options Options) []string {
	args := []string{"core", "-config", options.ConfigPath}
	for _, flag := range []struct{ name, value string }{
		{"-overlay", options.Overlay},
		{"-addr", options.APIAddr},
		{"-printer-addr", options.PrinterAddr},
		{"-log-level", options.LogLevel},
		{"-log-format", options.LogFormat},
	} {
		if flag.value != "" {
			args = append(args, flag.name, flag.value)
		}
	}

	return args
}
