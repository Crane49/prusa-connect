package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/internal/upgrade"
)

func TestCoreAndTwoBundledProcessExtensionsCommitTogether(t *testing.T) {
	if testing.Short() {
		t.Skip("builds four extension binaries")
	}
	t.Parallel()

	pids := t.TempDir()
	oldSpools := upgradeReleaseNamed(t, "spools", "1.0.0", filepath.Join(pids, "spools-old"))
	newSpools := upgradeReleaseNamed(t, "spools", "1.1.0", filepath.Join(pids, "spools-new"))
	oldQueue := upgradeReleaseNamed(t, "queue", "1.0.0", filepath.Join(pids, "queue-old"))
	newQueue := upgradeReleaseNamed(t, "queue", "1.1.0", filepath.Join(pids, "queue-new"))

	deployment := aLiveDeployment(t)
	install := func(module, version, from string) {
		t.Helper()
		installed, err := deployment.installer.Install(t.Context(), deployment.principal, httpapi.ModuleRelease{
			Module: module, Version: version, From: from, Approve: true,
		})
		require.NoError(t, err)
		require.True(t, installed.Running, installed.Reason)
	}
	install("example.test/spools", "1.0.0", oldSpools)
	install("example.test/queue", "1.0.0", oldQueue)

	extra := filepath.Join(deployment.installer.core.Modules, "spools", "extra")
	require.NoError(t, os.WriteFile(extra, []byte("old extra"), 0o600))

	root := t.TempDir()
	current := filepath.Join(root, "bin", "core")
	staged := filepath.Join(root, "staged", "core")
	writeGuardFile(t, current, "old core")
	writeGuardFile(t, staged, "new core")
	digest := sha256.Sum256([]byte("new core"))
	now := time.Date(2026, 10, 27, 0, 0, 0, 0, time.UTC)
	journal, err := upgrade.NewJournal("bundle-transaction", "1.0.0", "1.1.0", staged, now)
	require.NoError(t, err)
	journal.State = upgrade.StateRehearsed
	journal.Revision = 4
	journal.CurrentBinary = current
	journal.Verified = &upgrade.Verified{
		Name: "core", SHA256: hex.EncodeToString(digest[:]), Size: int64(len("new core")),
	}
	journal.Extensions = []upgrade.StagedExtension{
		testStagedExtension(t, "example.test/queue", "queue", anArchiveOf(t, newQueue)),
		testStagedExtension(t, "example.test/spools", "spools", anArchiveOf(t, newSpools)),
	}
	journal.ExtensionReviews, err = reviewBundledExtensions(
		deployment.installer.core, deployment.installer.configPath, journal,
	)
	require.NoError(t, err)
	journal.ExtensionsApproved = true
	store := upgrade.NewStore(filepath.Join(root, "journal.json"))
	require.NoError(t, store.Save(journal))

	spools, err := ref.New(ref.KindSink, "spools")
	require.NoError(t, err)
	queue, err := ref.New(ref.KindSink, "queue")
	require.NoError(t, err)
	runner, err := upgrade.NewRunner(upgrade.RunnerOptions{
		Store: store, Backups: upgrade.BackupManager{Root: filepath.Join(root, "backups")},
		BackupPlan: func(held upgrade.Journal) (upgrade.BackupPlan, error) {
			return upgrade.BackupPlan{
				ID: held.ID, Files: []upgrade.FileSource{
					{Name: "overlay", Path: deployment.installer.core.Overlay},
					{Name: "modules", Path: deployment.installer.core.Modules},
				},
			}, nil
		},
		Drain: func(context.Context) error {
			deployment.host.Stop(spools)
			deployment.host.Stop(queue)

			return nil
		},
		Activate: func(ctx context.Context, held upgrade.Journal) error {
			return activateBundledExtensions(
				ctx, deployment.installer.core, deployment.installer.configPath, held,
			)
		},
		Trial: func(ctx context.Context, _ upgrade.Journal) error {
			if reloadErr := deployment.store.Reload(ctx); reloadErr != nil {
				return reloadErr
			}
			deadline := time.NewTimer(10 * time.Second)
			defer deadline.Stop()
			ticker := time.NewTicker(20 * time.Millisecond)
			defer ticker.Stop()
			for {
				serving := true
				for _, instance := range []ref.Ref{spools, queue} {
					status, found := deployment.host.Status(instance)
					serving = serving && found && status.Serving
				}
				if serving {
					return nil
				}
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-deadline.C:
					return errors.New("extension trial timed out")
				case <-ticker.C:
				}
			}
		},
		Now: func() time.Time {
			now = now.Add(time.Second)

			return now
		},
	})
	require.NoError(t, err)

	committed, err := runner.Apply(t.Context())
	require.NoError(t, err)
	require.Equal(t, upgrade.StateCommitted, committed.State)
	require.Equal(t, "new core", readGuardFile(t, current))
	require.NoFileExists(t, extra)
	listedSpools, found := deployment.listed(t, spools)
	require.True(t, found)
	listedQueue, found := deployment.listed(t, queue)
	require.True(t, found)
	require.Equal(t, "1.1.0", listedSpools.Module.Version)
	require.Equal(t, "1.1.0", listedQueue.Module.Version)
	require.NotEqual(t, pidIn(t, filepath.Join(pids, "spools-old")), pidIn(t, filepath.Join(pids, "spools-new")))
	require.NotEqual(t, pidIn(t, filepath.Join(pids, "queue-old")), pidIn(t, filepath.Join(pids, "queue-new")))
}

func TestBundledExtensionActivationFailureRestoresWholeDeployment(t *testing.T) {
	if testing.Short() {
		t.Skip("builds four extension binaries")
	}
	t.Parallel()

	pids := t.TempDir()
	oldSpools := upgradeReleaseNamed(t, "spools", "1.0.0", filepath.Join(pids, "spools-old"))
	newSpools := upgradeReleaseNamed(t, "spools", "1.1.0", filepath.Join(pids, "spools-new"))
	oldNotes := aReleaseOfNotes(t, notesModule, "1.0.0")
	newNotes := aReleaseOfNotes(t, notesBroken, "1.1.0")
	appendManifest(t, newSpools, "permissions:\n  - observe\n")

	deployment := aLiveDeployment(t)
	for _, item := range []struct{ module, version, from string }{
		{"example.test/spools", "1.0.0", oldSpools},
		{notesPath, "1.0.0", oldNotes},
	} {
		_, err := deployment.installer.Install(t.Context(), deployment.principal, httpapi.ModuleRelease{
			Module: item.module, Version: item.version, From: item.from, Approve: true,
		})
		require.NoError(t, err)
	}

	oldExtra := filepath.Join(deployment.installer.core.Modules, "spools", "old-extra")
	newExtra := filepath.Join(deployment.installer.core.Modules, "spools", "new-extra")
	require.NoError(t, os.WriteFile(oldExtra, []byte("old extra"), 0o600))
	overlayBefore, err := os.ReadFile(deployment.installer.core.Overlay)
	require.NoError(t, err)
	configBefore, err := os.ReadFile(deployment.installer.configPath)
	require.NoError(t, err)
	grantsBefore := countSQLiteRows(t, deployment.installer.core.Authz.Database, "authz_declared_grant")

	root := t.TempDir()
	current := filepath.Join(root, "bin", "core")
	staged := filepath.Join(root, "staged", "core")
	writeGuardFile(t, current, "old core")
	writeGuardFile(t, staged, "new core")
	digest := sha256.Sum256([]byte("new core"))
	now := time.Date(2026, 10, 27, 0, 0, 0, 0, time.UTC)
	journal, err := upgrade.NewJournal("failed-bundle", "1.0.0", "1.1.0", staged, now)
	require.NoError(t, err)
	journal.State = upgrade.StateRehearsed
	journal.Revision = 4
	journal.CurrentBinary = current
	journal.Verified = &upgrade.Verified{
		Name: "core", SHA256: hex.EncodeToString(digest[:]), Size: int64(len("new core")),
	}
	journal.Extensions = []upgrade.StagedExtension{
		testStagedExtension(t, "example.test/spools", "spools", anArchiveOf(t, newSpools)),
		testStagedExtension(t, notesPath, "example-database", anArchiveOf(t, newNotes)),
	}
	journal.ExtensionReviews, err = reviewBundledExtensions(
		deployment.installer.core, deployment.installer.configPath, journal,
	)
	require.NoError(t, err)
	journal.ExtensionsApproved = true
	store := upgrade.NewStore(filepath.Join(root, "journal.json"))
	require.NoError(t, store.Save(journal))

	spools, err := ref.New(ref.KindSink, "spools")
	require.NoError(t, err)
	notes, err := ref.New(ref.KindSink, "example-database")
	require.NoError(t, err)
	trialCalled := false
	runner, err := upgrade.NewRunner(upgrade.RunnerOptions{
		Store: store, Backups: backupManager(deployment.installer.core, filepath.Join(root, "backups")),
		BackupPlan: func(held upgrade.Journal) (upgrade.BackupPlan, error) {
			plan, planErr := backupPlan(
				deployment.installer.configPath, deployment.installer.core, held.ToVersion,
				upgrade.PostgresNone, true,
			)
			plan.ID = held.ID

			return plan, planErr
		},
		Drain: func(context.Context) error {
			deployment.host.Stop(spools)
			deployment.host.Stop(notes)

			return nil
		},
		Activate: func(ctx context.Context, held upgrade.Journal) error {
			if activateErr := activateBundledExtensions(
				ctx, deployment.installer.core, deployment.installer.configPath, held,
			); activateErr != nil {
				return activateErr
			}
			require.NoError(t, os.WriteFile(newExtra, []byte("new extra"), 0o600))

			return nil
		},
		Trial: func(ctx context.Context, _ upgrade.Journal) error {
			trialCalled = true
			if reloadErr := deployment.store.Reload(ctx); reloadErr != nil {
				return reloadErr
			}
			deadline := time.NewTimer(10 * time.Second)
			defer deadline.Stop()
			ticker := time.NewTicker(20 * time.Millisecond)
			defer ticker.Stop()
			for {
				status, found := deployment.host.Status(notes)
				if found && (status.State == moduleStateFailed || status.State == moduleStateRetrying) {
					deployment.host.Stop(spools)
					deployment.host.Stop(notes)

					return errors.New("bundled extension failed candidate trial")
				}
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-deadline.C:
					return errors.New("failed extension did not report its state")
				case <-ticker.C:
				}
			}
		},
		Now: func() time.Time {
			now = now.Add(time.Second)

			return now
		},
	})
	require.NoError(t, err)

	rolledBack, err := runner.Apply(t.Context())
	require.Error(t, err)
	require.Equal(t, upgrade.StateRolledBack, rolledBack.State)
	require.True(t, trialCalled)
	require.Equal(t, "old core", readGuardFile(t, current))
	require.Equal(t, overlayBefore, requireFile(t, deployment.installer.core.Overlay))
	require.Equal(t, configBefore, requireFile(t, deployment.installer.configPath))
	require.Equal(t, grantsBefore,
		countSQLiteRows(t, deployment.installer.core.Authz.Database, "authz_declared_grant"))
	require.Equal(t, "old extra", string(requireFile(t, oldExtra)))
	require.NoFileExists(t, newExtra)
	require.FileExists(t, filepath.Join(
		deployment.installer.core.Modules, "spools", binaryDirName, platformBinary("spools"),
	))
	require.FileExists(t, filepath.Join(
		deployment.installer.core.Modules, "example-database", binaryDirName,
		platformBinary("example-database"),
	))
	require.NoError(t, deployment.store.Reload(t.Context()))
	require.Eventually(t, func() bool {
		state, reported := deployment.host.Database(notes)
		status, _ := deployment.host.Status(notes)

		return reported && state.Version == 3 && status.Serving
	}, settle, 20*time.Millisecond, "restored SQLite module data did not serve under the old release")
}

func appendManifest(t *testing.T, release, content string) {
	t.Helper()

	path := filepath.Join(release, manifestName)
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	require.NoError(t, err)
	_, writeErr := file.WriteString(content)
	require.NoError(t, errors.Join(writeErr, file.Close()))
}

func testStagedExtension(t *testing.T, module, name, path string) upgrade.StagedExtension {
	t.Helper()

	info, err := os.Stat(path)
	require.NoError(t, err)
	digest, err := digestOfFile(path)
	require.NoError(t, err)

	return upgrade.StagedExtension{
		BundleExtension: upgrade.BundleExtension{
			Module: module, Name: name, Kind: "sink", Version: "1.1.0",
			Path: filepath.ToSlash(filepath.Join("extensions", filepath.Base(path))),
			Size: info.Size(), SHA256: digest,
		},
		StagedPath: path,
	}
}

func requireFile(t *testing.T, path string) []byte {
	t.Helper()

	content, err := os.ReadFile(path)
	require.NoError(t, err)

	return content
}

func countSQLiteRows(t *testing.T, database, table string) int {
	t.Helper()

	db, err := sql.Open("sqlite", database+"?_pragma=busy_timeout(5000)")
	require.NoError(t, err)
	defer func() { require.NoError(t, db.Close()) }()

	var count int
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM "+table).Scan(&count))

	return count
}
