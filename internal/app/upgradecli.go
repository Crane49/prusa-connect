package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/app/hostschema"
	"github.com/prusa3d/prusa-connect/internal/kernel/sqlstore"
	"github.com/prusa3d/prusa-connect/internal/upgrade"
)

const UpgradeCommand = "upgrade"

func Upgrade(ctx context.Context, out io.Writer, args []string) error {
	if len(args) == 0 || args[0] != "check" {
		return fmt.Errorf("%w: %s check [flags]", ErrUsage, UpgradeCommand)
	}

	flags := flag.NewFlagSet(UpgradeCommand+" check", flag.ContinueOnError)
	flags.SetOutput(out)

	configPath := flags.String("config", defaultConfigPath, "the operator's configuration file")
	jsonOutput := flags.Bool("json", false, "write the rehearsal report as JSON")

	if err := flags.Parse(args[1:]); err != nil {
		return fmt.Errorf("%w: %w", ErrUsage, err)
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("%w: %s check takes flags only", ErrUsage, UpgradeCommand)
	}

	started := time.Now()
	core, err := loadCore(*configPath)
	if err != nil {
		return err
	}

	report, err := rehearseCore(ctx, *configPath, core)
	if err != nil {
		return err
	}
	for _, key := range core.Ignored {
		report.Warnings = append(report.Warnings, key.String())
	}
	report.CheckedAt = time.Now().UTC()
	report.DurationMS = max(1, time.Since(started).Milliseconds())

	if *jsonOutput {
		return writeJSON(out, report)
	}

	fmt.Fprintf(out, "migration rehearsal passed in %s\n", time.Duration(report.DurationMS)*time.Millisecond)
	for _, warning := range report.Warnings {
		fmt.Fprintf(out, "warning: %s\n", warning)
	}

	return nil
}

func rehearseCore(ctx context.Context, configPath string, core config.Core) (upgrade.RehearsalReport, error) {
	if core.Persistence.Type == config.EnginePostgres {
		db, dialect, where, err := openDatabase(ctx, core)
		if err != nil {
			return upgrade.RehearsalReport{}, err
		}
		defer func() { _ = db.Close() }()

		migrator, err := hostschema.Migrator(db, engineOf(dialect), "")
		if err != nil {
			return upgrade.RehearsalReport{}, fmt.Errorf("rehearse database at %s: %w", where, err)
		}
		if err = migrator.Rehearse(ctx); err != nil {
			return upgrade.RehearsalReport{}, fmt.Errorf("rehearse database at %s: %w", where, err)
		}

		return upgrade.RehearsalReport{
			Warnings: []string{"PostgreSQL adapter and module databases require their own backup policy"},
		}, nil
	}

	plan, err := backupPlan(configPath, core, "", upgrade.PostgresNone, true)
	if err != nil {
		return upgrade.RehearsalReport{}, err
	}
	plan.Files = nil
	plan.Postgres = nil
	plan.ID = "rehearsal"

	rehearsalParent := filepath.Dir(core.Overlay)
	if err = os.MkdirAll(rehearsalParent, 0o700); err != nil {
		return upgrade.RehearsalReport{}, fmt.Errorf("create rehearsal parent: %w", err)
	}

	root, err := os.MkdirTemp(rehearsalParent, ".upgrade-rehearsal-")
	if err != nil {
		return upgrade.RehearsalReport{}, fmt.Errorf("create rehearsal directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(root) }()

	manager := upgrade.BackupManager{Root: root}
	manifest, err := manager.Create(ctx, plan)
	if err != nil {
		return upgrade.RehearsalReport{}, err
	}

	hostPath := ""
	for _, file := range manifest.Files {
		if file.Name == "host" {
			hostPath = filepath.Join(root, manifest.ID, filepath.FromSlash(file.Path))

			break
		}
	}
	if hostPath == "" {
		return upgrade.RehearsalReport{}, errors.New("upgrade: rehearsal did not snapshot the host database")
	}

	db, err := sql.Open("sqlite", hostPath+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		return upgrade.RehearsalReport{}, fmt.Errorf("open rehearsal host database: %w", err)
	}
	defer func() { _ = db.Close() }()

	migrator, err := hostschema.Migrator(db, sqlstore.EngineSQLite, "")
	if err != nil {
		return upgrade.RehearsalReport{}, err
	}
	if err = migrator.Migrate(ctx); err != nil {
		return upgrade.RehearsalReport{}, fmt.Errorf("rehearse host migrations: %w", err)
	}
	if err = migrator.RequireSchemaOfThisBuild(ctx); err != nil {
		return upgrade.RehearsalReport{}, fmt.Errorf("verify rehearsed host schema: %w", err)
	}

	return upgrade.RehearsalReport{
		Warnings: []string{"resource database snapshots passed integrity checks; their adapters migrate them at start"},
	}, nil
}

func decodeRehearsal(output []byte) (upgrade.RehearsalReport, error) {
	var report upgrade.RehearsalReport
	if err := json.Unmarshal(output, &report); err != nil {
		return upgrade.RehearsalReport{}, err
	}

	return report, nil
}
