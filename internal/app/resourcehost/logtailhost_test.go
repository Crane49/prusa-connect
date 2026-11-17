package resourcehost_test

import (
	"context"
	"log/slog"
	"testing"

	"github.com/prusa3d/prusa-connect/internal/app/resourcehost"
	"github.com/prusa3d/prusa-connect/internal/extension/supervisor"
)

// A server logging at error level drops a module's standard error from its own
// log, and the page that reads it back must still have it.
func TestTheLogHoldsStderrWhateverTheServerLogs(t *testing.T) {
	t.Parallel()

	w := newWiredWith(t, nil, func(cfg *resourcehost.Config) {
		onStatus := cfg.OnStatus
		cfg.Logger = slog.New(silent{wake: func() { onStatus(supervisor.Status{}) }})
	})
	w.declareHog(installHog(t), "idle", 64)

	instance := theServiceSink(t)
	w.waitReady(instance)

	w.await(func() bool {
		log, _ := w.host.Log(instance)

		for _, line := range log.Lines {
			if hogBanner.MatchString(line.Text) {
				return true
			}
		}

		return false
	}, "the child's standard error never reached the log", func() string { return w.describe(instance) })
}

// silent is a server log that enables nothing. The banner reaches the ring on
// the stderr reader's goroutine, often after the instance's last status
// change, and a server that logs nothing writes no line to the recorder, so
// neither would wake the wait. The tail asks its inner handler whether a record
// is enabled after keeping it, so that question is the wake-up.
type silent struct{ wake func() }

func (s silent) Enabled(context.Context, slog.Level) bool {
	s.wake()

	return false
}

func (silent) Handle(context.Context, slog.Record) error { return nil }
func (s silent) WithAttrs([]slog.Attr) slog.Handler      { return s }
func (s silent) WithGroup(string) slog.Handler           { return s }
