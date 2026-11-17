package app

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"

	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/betaportal"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/internal/upgrade"
)

// openPortal opens the Beta Portal pairing and the client that pairs over
// it. With core.upgrade.enabled off it opens neither and writes no file, and
// the start says so. The pairing operations then answer not found, and the
// marketplace signs no request. The client is returned as the interface, so
// an absent one is a nil interface and never a nil pointer inside one.
func openPortal(
	ctx context.Context, logger *slog.Logger, core config.Core, configPath, instance string,
) (*betaportal.Store, httpapi.BetaPortalPairing, error) {
	if !core.Upgrade.On() {
		reportUpgradesOff(ctx, logger, core, configPath)

		return nil, nil, nil
	}

	entitlement, err := openPairing(ctx, logger, core, configPath)
	if err != nil {
		return nil, nil, err
	}

	portal, err := betaportal.NewClient(core.Upgrade.Server, &http.Client{Timeout: portalTimeout}, entitlement,
		betaportal.Metadata{
			InstanceID: instance,
			Name:       core.Frontend.Title,
			Platform:   upgrade.Platform(),
		})
	if err != nil {
		return nil, nil, err
	}

	return entitlement, portal, nil
}

// openPairing opens the Beta Portal pairing betaportal.Resolve names, the
// same file the backup protects. An earlier release kept the pairing beside
// the configuration file. A pairing there that cannot be read, decoded or
// protected is left where it is and named in the log. The server then opens
// its pairing beside the overlay, so the start goes on.
func openPairing(
	ctx context.Context, logger *slog.Logger, core config.Core, configPath string,
) (*betaportal.Store, error) {
	beside := betaportal.Path(core.Overlay)

	path, passedOver := betaportal.Resolve(configPath, core.Overlay)
	if passedOver != nil {
		earlier := betaportal.EarlierPath(configPath)
		token, _ := betaportal.HoldsToken(earlier)
		logger.WarnContext(ctx, passedOverMessage(passedOver, token),
			"file", absolute(earlier), "error", passedOver.Error())
	}

	entitlement, err := betaportal.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open the Beta Portal pairing store: %w", err)
	}
	if recovered := entitlement.Recovered(); recovered != "" {
		logger.WarnContext(ctx, "the Beta Portal pairing file was damaged and was moved aside; pair again in Updates",
			"moved_to", recovered)
	}
	if unused := otherPairing(path, beside); unused != "" {
		reportUnusedPairing(ctx, logger, unused, path)
	}

	return entitlement, nil
}

// reportUnusedPairing names the pairing beside the overlay that the server
// does not use. One that still holds an access token, or may hold one, is
// removed in the Beta Portal first, because Disconnect acts on the pairing in
// use.
func reportUnusedPairing(ctx context.Context, logger *slog.Logger, unused, used string) {
	const unusedPairing = "a Beta Portal pairing beside the overlay is not used while the one beside the " +
		"configuration file can be used"
	const idle = unusedPairing + "; delete it once Updates shows the pairing you expect"
	const held = unusedPairing + ", and it still holds an access token; remove it in the Beta Portal, then delete it"
	const unreadable = unusedPairing + ", and it cannot be read and may still hold an access token; remove it " +
		"in the Beta Portal, then delete it"

	token, err := betaportal.HoldsToken(unused)
	switch {
	case err != nil:
		logger.WarnContext(ctx, unreadable, "file", absolute(unused), "used", absolute(used), "error", err.Error())
	case token:
		logger.WarnContext(ctx, held, "file", absolute(unused), "used", absolute(used))
	default:
		logger.WarnContext(ctx, idle, "file", absolute(unused), "used", absolute(used))
	}
}

// passedOverMessage says why the earlier pairing is not used and what to do.
// A file that cannot be read can be repaired, and one the server cannot set to
// its file mode can be moved to a writable folder; a damaged one cannot be
// repaired. A file that still holds an access token says so.
func passedOverMessage(err error, token bool) string {
	const opens = "the Beta Portal pairing beside the configuration file "
	message := opens + "is damaged and not used, so the server opens one beside the overlay; if it held a " +
		"pairing, remove this server in the Beta Portal, then delete the file"
	switch {
	case errors.Is(err, betaportal.ErrNotProtected):
		message = opens + "cannot be set to mode 0600, so the server opens one beside the overlay; if it held " +
			"a pairing, remove this server in the Beta Portal, then delete the file"
	case errors.Is(err, fs.ErrPermission):
		message = opens + "cannot be read, so the server opens one beside the overlay; make the file readable " +
			"by the server and restart it to use the file again"
	}
	if token {
		message += "; the file still holds an access token"
	}

	return message
}

// otherPairing returns beside when it holds a pairing file other than the one
// at path, and nothing otherwise.
func otherPairing(path, beside string) string {
	other, err := os.Stat(beside)
	if err != nil {
		return ""
	}
	used, err := os.Stat(path)
	if err != nil || os.SameFile(used, other) {
		return ""
	}

	return beside
}

// absolute names path from the root, so a log line names the file wherever
// the reader stands. A path it cannot resolve is returned as it is.
func absolute(path string) string {
	resolved, err := filepath.Abs(path)
	if err != nil {
		return path
	}

	return resolved
}
