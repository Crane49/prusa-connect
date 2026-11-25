package app

import (
	"fmt"
	"io"
	"log/slog"

	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/platform/observability"
)

// newLogger builds the one logger every part of the core is handed. Code in
// the core takes this logger and does not set a package-level one.
//
// It writes to out, which is standard error unless the process entry names
// another writer. Standard error is used so that a process whose stdout is a
// pipe still logs somewhere an operator can see. A Windows service has no
// standard error anybody reads, and logs to a file instead.
//
// Every line passes through the counting handler. That handler writes
// pcl_log_lines_total and samples the lines below info. counting's zero value
// counts nothing and samples on the defaults, for a test that is not about
// metrics.
//
// The ring keeps what out receives, for a diagnostics bundle. A nil ring
// records nothing.
func newLogger(
	cfg config.Log,
	out io.Writer,
	counting observability.LogConfig,
	ring *observability.LogRing,
) (*slog.Logger, error) {
	level, err := parseLevel(cfg.Level)
	if err != nil {
		return nil, err
	}

	options := &slog.HandlerOptions{Level: level}

	var sink slog.Handler

	switch cfg.Format {
	case "json":
		sink = slog.NewJSONHandler(out, options)
	case "text":
		sink = slog.NewTextHandler(out, options)
	default:
		return nil, fmt.Errorf("%w: core.log.format is %q", config.ErrInvalid, cfg.Format)
	}

	sink = ringSink(sink, ring)

	return slog.New(observability.NewLogHandler(sink, counting)), nil
}

func parseLevel(name string) (slog.Level, error) {
	var level slog.Level
	if err := level.UnmarshalText([]byte(name)); err != nil {
		return 0, fmt.Errorf("%w: core.log.level is %q", config.ErrInvalid, name)
	}

	return level, nil
}
