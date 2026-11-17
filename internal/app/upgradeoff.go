package app

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"time"

	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/betaportal"
	"github.com/prusa3d/prusa-connect/internal/upgrade"
)

// reportUpgradesOff says that core.upgrade.enabled is off, names a pairing an
// earlier run left with an access token, and cancels an upgrade an earlier run
// scheduled. The web app shows neither while upgrades are off.
func reportUpgradesOff(ctx context.Context, logger *slog.Logger, core config.Core, configPath string) {
	logger.InfoContext(ctx, "upgrades are off", "key", "core.upgrade.enabled")

	reportPairingLeft(ctx, logger, core, configPath)
	cancelScheduledUpgrade(ctx, logger, core)
}

// reportPairingLeft names each pairing file that still holds a token. The
// token works at the portal until a disconnect revokes it, and Disconnect is
// on the Updates tab, which is not drawn. A file whose token is cleared holds
// a private key the portal does not know, so it is not named.
func reportPairingLeft(ctx context.Context, logger *slog.Logger, core config.Core, configPath string) {
	const held = "a Beta Portal pairing still holds an access token; switch upgrades on and disconnect " +
		"it in Updates, or remove this server in the Beta Portal and then delete the file"
	const unreadable = "a Beta Portal pairing cannot be read and may still hold an access token; switch " +
		"upgrades on and disconnect it in Updates, or remove this server in the Beta Portal and then delete the file"

	named := map[string]bool{}
	for _, path := range []string{betaportal.Path(core.Overlay), betaportal.EarlierPath(configPath)} {
		path = absolute(path)
		if named[path] {
			continue
		}
		named[path] = true

		token, err := betaportal.HoldsToken(path)
		switch {
		case err != nil:
			logger.WarnContext(ctx, unreadable, "file", path, "error", err.Error())
		case token:
			logger.WarnContext(ctx, held, "file", path)
		}
	}
}

// cancelScheduledUpgrade removes an upgrade scheduled before upgrades were
// switched off, like Cancel on the Updates tab. No scheduler runs while they
// are off. If the upgrade were kept, the scheduler would start it as soon as
// upgrades are on again, and nobody could see or cancel it before then.
func cancelScheduledUpgrade(ctx context.Context, logger *slog.Logger, core config.Core) {
	const cancelled = "an upgrade scheduled before upgrades were switched off is cancelled; " +
		"prepare the release again once upgrades are on"

	journal, removed, err := upgrade.CancelScheduled(filepath.Dir(upgradeJournalPath(core)))
	if removed {
		attrs := []any{"transaction", journal.ID, "to_version", journal.ToVersion}
		if journal.ScheduledFor != nil {
			attrs = append(attrs, "scheduled_for", journal.ScheduledFor.UTC().Format(time.RFC3339))
		}
		logger.WarnContext(ctx, cancelled, attrs...)
	}
	switch {
	case err != nil && removed:
		logger.WarnContext(ctx, "the staged release of the cancelled upgrade is left on disk", "error", err.Error())
	case err != nil && journal.ID != "":
		const kept = "an upgrade scheduled before upgrades were switched off cannot be cancelled, because its " +
			"journal cannot be removed; "
		release := "its staged release is removed, so the upgrade fails once upgrades are on"
		if errors.Is(err, upgrade.ErrStagedLeft) {
			release = "its staged release is left on disk, so the upgrade may run once upgrades are on"
		}
		logger.WarnContext(ctx, kept+release+"; make the upgrade folder writable and restart the server",
			"transaction", journal.ID, "error", err.Error())
	case err != nil:
		logger.WarnContext(ctx, "the upgrade journal cannot be read, so a scheduled upgrade may still wait",
			"error", err.Error())
	}
}
