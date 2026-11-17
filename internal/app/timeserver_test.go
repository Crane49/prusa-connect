package app

import (
	"errors"
	"log/slog"
	"net"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/discovery/networktest"
	"github.com/prusa3d/prusa-connect/internal/timeserver"
)

// storedSwitch is the machine-owned file's time server member.
type storedSwitch struct {
	enabled, recorded bool
	unreadable        error
	refuse            error
}

func (s *storedSwitch) TimeServerEnabled() (enabled, recorded bool, err error) {
	return s.enabled, s.recorded, s.unreadable
}

func (s *storedSwitch) RecordTimeServerEnabled(enabled bool) error {
	if s.refuse != nil {
		return s.refuse
	}

	s.enabled, s.recorded = enabled, true

	return nil
}

func newTestTimeService(t *testing.T, fromFile bool, store *storedSwitch) *timeService {
	t.Helper()

	var core config.Core
	core.TimeServer.Enabled = fromFile

	logger := slog.New(slog.DiscardHandler)
	service := newTimeService(t.Context(), core, timeserver.New("127.0.0.1:0", logger), timeserver.Port, store, logger)
	t.Cleanup(service.stop)

	return service
}

func TestTheConfigurationKeyStartsTheTimeServerWhileNothingIsStored(t *testing.T) {
	t.Parallel()

	on := newTestTimeService(t, true, &storedSwitch{})
	require.Equal(t, "serving", on.TimeServer(t.Context()).State)

	off := newTestTimeService(t, false, &storedSwitch{})
	require.Equal(t, "off", off.TimeServer(t.Context()).State)
}

func TestAStoredChoiceWinsOverTheConfigurationKey(t *testing.T) {
	t.Parallel()

	service := newTestTimeService(t, true, &storedSwitch{enabled: false, recorded: true})

	state := service.TimeServer(t.Context())
	require.False(t, state.Enabled)
	require.Equal(t, "off", state.State)
}

func TestAFailedPortReachesTheAnswerWithItsCause(t *testing.T) {
	t.Parallel()

	taken, err := net.ListenPacket("udp4", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = taken.Close() })

	logger := slog.New(slog.DiscardHandler)
	service := newTimeService(t.Context(), config.Core{},
		timeserver.New(taken.LocalAddr().String(), logger), timeserver.Port, &storedSwitch{}, logger)
	t.Cleanup(service.stop)

	state, err := service.SetTimeServer(t.Context(), true)
	require.NoError(t, err)
	require.Equal(t, "failed", state.State)
	require.Equal(t, timeserver.CauseInUse, state.Cause)
	require.NotEmpty(t, state.Reason)
}

func TestTheSwitchIsStoredAndAppliedAtOnce(t *testing.T) {
	t.Parallel()

	store := &storedSwitch{}
	service := newTestTimeService(t, false, store)

	state, err := service.SetTimeServer(t.Context(), true)
	require.NoError(t, err)
	require.Equal(t, "serving", state.State)
	require.Equal(t, timeserver.Port, state.Port)
	require.True(t, store.enabled && store.recorded)

	state, err = service.SetTimeServer(t.Context(), false)
	require.NoError(t, err)
	require.Equal(t, "off", state.State)
	require.False(t, store.enabled)
}

func TestARefusedWriteLeavesTheTimeServerAsItWas(t *testing.T) {
	t.Parallel()

	store := &storedSwitch{refuse: errors.New("read-only file system")}
	service := newTestTimeService(t, true, store)

	_, err := service.SetTimeServer(t.Context(), false)
	require.Error(t, err)

	state := service.TimeServer(t.Context())
	require.True(t, state.Enabled)
	require.Equal(t, "serving", state.State)
}

func TestAnUnreadableFileLeavesTheConfigurationKeyInCharge(t *testing.T) {
	t.Parallel()

	service := newTestTimeService(t, true, &storedSwitch{
		enabled: false, recorded: true, unreadable: errors.New("the overlay is not JSON"),
	})
	require.Equal(t, "serving", service.TimeServer(t.Context()).State)
}

// TestAnIPv4HolderOfThePortIsAFailure is the port held on 0.0.0.0 by another
// program. A dual-stack socket on [::] opens beside it on macOS and hears
// nothing over IPv4, so the server has to report failed.
func TestAnIPv4HolderOfThePortIsAFailure(t *testing.T) {
	networktest.Require(t)
	t.Parallel()

	taken, err := net.ListenPacket("udp4", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	defer taken.Close()

	_, port, err := net.SplitHostPort(taken.LocalAddr().String())
	if err != nil {
		t.Fatal(err)
	}

	server := timeserver.New(":"+port, slog.New(slog.DiscardHandler))
	if err = server.Start(t.Context()); err == nil {
		server.Stop()
		t.Fatal("start beside an IPv4 holder of the port succeeded")
	}

	if state, _, cause := server.State(); state != timeserver.StateFailed || cause != timeserver.CauseInUse {
		t.Fatalf("state %s, cause %q, want failed with in_use", state, cause)
	}
}
