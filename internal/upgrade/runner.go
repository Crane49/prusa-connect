package upgrade

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

type RunnerOptions struct {
	Store       Store
	Backups     BackupManager
	BackupPlan  func(Journal) (BackupPlan, error)
	Drain       func(context.Context) error
	Activate    func(context.Context, Journal) error
	Trial       func(context.Context, Journal) error
	Now         func() time.Time
	SyncParent  func(string) error
	RestoreDSNs map[string]PostgresRestoreTarget
	Logger      *slog.Logger
}

type Runner struct {
	options RunnerOptions
}

func NewRunner(options RunnerOptions) (*Runner, error) {
	if options.Store.Path() == "" || options.BackupPlan == nil || options.Trial == nil {
		return nil, errors.New("upgrade: runner needs a journal, backup plan and trial")
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.SyncParent == nil {
		options.SyncParent = syncDirectory
	}
	if options.Logger == nil {
		options.Logger = slog.New(slog.DiscardHandler)
	}

	return &Runner{options: options}, nil
}

//nolint:gocognit // The steps are the durable transaction order and cannot be reordered.
func (r *Runner) Apply(ctx context.Context) (Journal, error) {
	journal, err := r.options.Store.Load()
	if err != nil {
		return Journal{}, err
	}
	if journal.State != StateRehearsed && journal.State != StateScheduled {
		return Journal{}, fmt.Errorf("%w: apply needs rehearsed or scheduled, found %s", ErrJournal, journal.State)
	}
	if err = verifyStagedIdentity(journal); err != nil {
		failed, transitionErr := r.options.Store.Transition(
			journal, StateFailedBeforeSwap, r.options.Now(),
			func(next *Journal) { next.Failure = err.Error() },
		)
		return failed, errors.Join(err, transitionErr)
	}

	journal, err = r.options.Store.Transition(journal, StateDraining, r.options.Now(), nil)
	if err != nil {
		return Journal{}, err
	}

	if r.options.Drain != nil {
		if err = r.options.Drain(ctx); err != nil {
			return r.rollback(ctx, journal, fmt.Errorf("drain: %w", err))
		}
	}

	plan, err := r.options.BackupPlan(journal)
	if err != nil {
		return r.rollback(ctx, journal, err)
	}
	if plan.ID == "" {
		plan.ID = journal.ID
	}

	journal, err = r.options.Store.Transition(journal, StateBackedUp, r.options.Now(), func(next *Journal) {
		next.BackupID = plan.ID
	})
	if err != nil {
		return Journal{}, err
	}

	if _, err = r.options.Backups.Create(ctx, plan); err != nil {
		return r.rollback(ctx, journal, fmt.Errorf("create upgrade backup: %w", err))
	}
	journal.BackupReady = true
	journal.Revision++
	journal.UpdatedAt = r.options.Now().UTC()
	if err = r.options.Store.Save(journal); err != nil {
		return Journal{}, err
	}
	if err = verifyStagedIdentity(journal); err != nil {
		return r.rollback(ctx, journal, err)
	}
	if r.options.Activate != nil {
		if err = r.options.Activate(ctx, journal); err != nil {
			return r.rollback(ctx, journal, fmt.Errorf("activate bundled extensions: %w", err))
		}
	}

	candidate, err := placeBeside(journal, r.options.SyncParent)
	if err != nil {
		return r.rollback(ctx, journal, err)
	}

	previous := journal.CurrentBinary + ".previous"
	journal, err = r.options.Store.Transition(journal, StateSwapped, r.options.Now(), func(next *Journal) {
		next.PreviousBinary = previous
	})
	if err != nil {
		return Journal{}, err
	}

	if err = swapBinaries(journal.CurrentBinary, candidate, previous, r.options.SyncParent); err != nil {
		return r.rollback(ctx, journal, err)
	}

	journal, err = r.options.Store.Transition(journal, StateTrial, r.options.Now(), nil)
	if err != nil {
		return Journal{}, err
	}

	if err = r.options.Trial(ctx, journal); err != nil {
		return r.rollback(ctx, journal, fmt.Errorf("trial: %w", err))
	}

	journal, err = r.options.Store.Transition(journal, StateCommitted, r.options.Now(), nil)
	if err != nil {
		return Journal{}, err
	}

	return journal, nil
}

func verifyStagedIdentity(journal Journal) error {
	if journal.Verified == nil {
		return errors.New("upgrade: staged binary has no verified identity")
	}
	file, err := os.Open(journal.StagedBinary)
	if err != nil {
		return fmt.Errorf("upgrade: reopen staged binary: %w", err)
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("upgrade: inspect staged binary: %w", err)
	}

	hash := sha256.New()
	if _, err = io.Copy(hash, file); err != nil {
		return fmt.Errorf("upgrade: rehash staged binary: %w", err)
	}
	got := hex.EncodeToString(hash.Sum(nil))
	if info.Size() != journal.Verified.Size || got != journal.Verified.SHA256 {
		return errors.New("upgrade: staged binary changed after signature verification")
	}
	for _, extension := range journal.Extensions {
		if err = verifyFileIdentity(
			extension.StagedPath,
			extension.Size,
			extension.SHA256,
			"extension "+extension.Module,
		); err != nil {
			return err
		}
	}

	return nil
}

func verifyFileIdentity(path string, wantSize int64, wantSHA256, name string) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("upgrade: reopen staged %s: %w", name, err)
	}
	defer func() { _ = file.Close() }()

	hash := sha256.New()
	size, err := io.Copy(hash, file)
	if err != nil {
		return fmt.Errorf("upgrade: rehash staged %s: %w", name, err)
	}
	if size != wantSize || hex.EncodeToString(hash.Sum(nil)) != wantSHA256 {
		return fmt.Errorf("upgrade: staged %s changed after signature verification", name)
	}

	return nil
}

func (r *Runner) Recover(ctx context.Context) (Journal, error) {
	journal, err := r.options.Store.Load()
	if err != nil {
		return Journal{}, err
	}
	if !journal.Interrupted() {
		return journal, nil
	}

	return r.rollback(ctx, journal, errors.New("upgrade was interrupted"))
}

func (r *Runner) rollback(ctx context.Context, journal Journal, cause error) (Journal, error) {
	if journal.State != StateRollingBack {
		var err error
		journal, err = r.options.Store.Transition(journal, StateRollingBack, r.options.Now(), func(next *Journal) {
			next.Failure = cause.Error()
		})
		if err != nil {
			return Journal{}, errors.Join(cause, err)
		}
	}

	var rollbackErr error

	if journal.BackupID != "" && (journal.BackupReady || journal.PreviousBinary != "") {
		external, restoreErr := r.options.Backups.RestoreReporting(ctx, journal.BackupID, r.options.RestoreDSNs)
		rollbackErr = errors.Join(rollbackErr, restoreErr)
		journal.ExternalRestore = journal.ExternalRestore[:0]
		for _, entry := range external {
			journal.ExternalRestore = append(journal.ExternalRestore, entry.Name)
			r.options.Logger.WarnContext(ctx,
				"the upgrade rolled back without this PostgreSQL database; restore it from the external backup",
				"database", entry.Name, "mode", string(entry.Mode), "backup", journal.BackupID)
		}
	}

	if journal.PreviousBinary != "" {
		rollbackErr = errors.Join(rollbackErr, restorePrevious(
			journal.CurrentBinary, journal.StagedBinary, journal.PreviousBinary, r.options.SyncParent,
		))
	}

	if journal.CurrentBinary != "" {
		next := journal.CurrentBinary + nextSuffix
		if err := os.Remove(next); err != nil && !errors.Is(err, os.ErrNotExist) {
			rollbackErr = errors.Join(rollbackErr, fmt.Errorf("upgrade: remove candidate copy: %w", err))
		}
	}

	if rollbackErr != nil {
		return journal, errors.Join(cause, fmt.Errorf("rollback: %w", rollbackErr))
	}

	journal, err := r.options.Store.Transition(journal, StateRolledBack, r.options.Now(), nil)
	if err != nil {
		return Journal{}, errors.Join(cause, err)
	}

	return journal, cause
}

func swapBinaries(current, staged, previous string, syncParent func(string) error) error {
	if current == "" || staged == "" || previous == "" {
		return errors.New("upgrade: binary swap has an empty path")
	}
	if filepath.Dir(current) != filepath.Dir(previous) {
		return errors.New("upgrade: current and previous binary are on different directories")
	}

	return platformSwap(current, staged, previous, syncParent)
}

func restorePrevious(current, staged, previous string, syncParent func(string) error) error {
	if _, err := os.Stat(previous); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			if _, currentErr := os.Stat(current); currentErr == nil {
				return nil
			}
		}

		return fmt.Errorf("upgrade: inspect previous binary: %w", err)
	}

	keepFailedCandidate(current, staged)

	// The previous binary replaces current in one rename beside it, so the
	// service path is never empty and no rename crosses a filesystem.
	if err := replaceExecutable(previous, current); err != nil {
		return fmt.Errorf("upgrade: restore previous binary: %w", err)
	}

	return syncParent(filepath.Dir(current))
}

// keepFailedCandidate copies the failed binary to the staged path when that
// copy is gone, so an operator can still inspect it. It is best effort: the
// rollback never depends on it.
func keepFailedCandidate(current, staged string) {
	if _, err := os.Stat(staged); !errors.Is(err, os.ErrNotExist) {
		return
	}
	if _, err := os.Stat(current); err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(staged), privateDir); err != nil {
		return
	}
	_ = copyExecutable(current, staged)
}

// RestorePreviousBinary puts the trusted pre-upgrade executable back at its
// stable path. It is used by an explicit backup rollback after the server has
// closed its databases.
func RestorePreviousBinary(current, staged, previous string) error {
	return restorePrevious(current, staged, previous, syncDirectory)
}
