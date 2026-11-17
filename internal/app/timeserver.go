package app

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/timeserver"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
)

// timeSwitchStore is the part of the resource store that keeps the time
// server switch in the machine-owned file.
type timeSwitchStore interface {
	TimeServerEnabled() (enabled, recorded bool, err error)
	RecordTimeServerEnabled(enabled bool) error
}

// timeService is the time server and its switch. It holds the run's context,
// because a server started from a request has to outlive that request.
type timeService struct {
	ctx    context.Context //nolint:containedctx // the run's lifetime, which the request contexts do not carry
	server *timeserver.Server
	store  timeSwitchStore
	port   int

	// mu makes the write to the file and the change to the socket one step.
	mu      sync.Mutex
	enabled bool
}

var _ httpapi.TimeServer = (*timeService)(nil)

// newTimeService starts the server when the switch is on. A choice stored in
// the machine-owned file wins over core.time_server.enabled. Neither a file
// that cannot be read nor a port that will not open stops the start: the time
// server is optional, and the API reports its state.
func newTimeService(
	ctx context.Context, core config.Core, server *timeserver.Server, port int, store timeSwitchStore,
	logger *slog.Logger,
) *timeService {
	enabled, recorded, err := store.TimeServerEnabled()
	if err != nil {
		logger.WarnContext(ctx, "the time server switch could not be read, so the configuration decides",
			"error", err)
	}

	if !recorded || err != nil {
		enabled = core.TimeServer.Enabled
	}

	service := &timeService{ctx: ctx, server: server, store: store, port: port, enabled: enabled}
	if enabled {
		_ = server.Start(ctx) // a failure is the state the API reports
	}

	return service
}

// stop closes the socket on the way out.
func (s *timeService) stop() { s.server.Stop() }

// TimeServer is the switch and the state of the server.
func (s *timeService) TimeServer(context.Context) httpapi.TimeServerState {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.state()
}

// SetTimeServer stores the switch first and then applies it, so a write the
// file refused leaves the socket as it was. Switching on again retries a port
// that failed.
func (s *timeService) SetTimeServer(_ context.Context, enabled bool) (httpapi.TimeServerState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.store.RecordTimeServerEnabled(enabled); err != nil {
		return httpapi.TimeServerState{}, fmt.Errorf("store the time server switch: %w", err)
	}

	s.enabled = enabled
	if enabled {
		_ = s.server.Start(s.ctx) // a failure is the state answered below
	} else {
		s.server.Stop()
	}

	return s.state(), nil
}

func (s *timeService) state() httpapi.TimeServerState {
	state, reason, cause := s.server.State()

	return httpapi.TimeServerState{Enabled: s.enabled, State: state, Reason: reason, Cause: cause, Port: s.port}
}
