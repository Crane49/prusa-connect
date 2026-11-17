package discovery

import (
	"context"
	"runtime"
	"testing"
	"testing/synctest"
	"time"

	"github.com/miekg/dns"
	"github.com/stretchr/testify/require"
)

// TestHidingWithdrawsTheRecordsAtOnce is the goodbye on hide. Every record
// goes out once more with a time to live of zero, and showing the server
// probes and announces again. No restart.
func TestHidingWithdrawsTheRecordsAtOnce(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		over := newFakeNetwork()
		announcer := fakeAnnouncer(over)

		announcer.Start(t.Context(), 8080)
		defer announcer.Stop(t.Context())

		settle()
		require.Equal(t, StateAnnouncing, announcer.State())

		hidden := over.mark()
		announcer.SetHidden(t.Context(), true)

		withdrawn := over.since(hidden)
		require.Len(t, withdrawn, 1, "hiding sent no goodbye")
		require.True(t, isGoodbye(withdrawn[0]))
		require.Len(t, withdrawn[0].msg.Answer, 10, "the goodbye left a record out")
		require.Equal(t, StateHidden, announcer.State())

		shown := over.mark()
		announcer.SetHidden(t.Context(), false)
		require.Equal(t, StateAnnouncing, announcer.State())

		settle()

		again := over.since(shown)
		require.Equal(t, 3, count(again, isProbe))
		require.Equal(t, 2, count(again, isAnnouncement), "showing the server again did not announce it")
		require.Equal(t, 1, over.opens)
	})
}

func TestAServerHiddenAtStartAnnouncesNothing(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		over := newFakeNetwork()
		announcer := fakeAnnouncer(over)

		announcer.SetHidden(t.Context(), true)
		announcer.Start(t.Context(), 8080)
		defer announcer.Stop(t.Context())

		settle()
		require.Equal(t, StateHidden, announcer.State())
		require.Empty(t, over.all())

		announcer.SetHidden(t.Context(), false)
		settle()

		require.Equal(t, 2, count(over.all(), isAnnouncement))
		require.Equal(t, StateAnnouncing, announcer.State())
	})
}

// TestAHiddenServerAnswersNothingAndReclaimsNothing is the hidden setting
// holding against the link. A question gets no answer, and a machine that
// claims the name does not make the server probe or announce again.
func TestAHiddenServerAnswersNothingAndReclaimsNothing(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		over := newFakeNetwork()
		announcer := fakeAnnouncer(over)

		announcer.Start(t.Context(), 8080)
		defer announcer.Stop(t.Context())

		settle()
		announcer.SetHidden(t.Context(), true)

		hidden := over.mark()

		over.deliver(claim(), peer, groupIPv4)
		over.deliver(question(linkService+".local.", dns.TypePTR), peer, groupIPv4)
		time.Sleep(10 * time.Second)
		synctest.Wait()

		require.Empty(t, over.since(hidden), "a hidden server sent a packet")
		require.Equal(t, StateHidden, announcer.State())
	})
}

// TestAHideWhileTheNameIsHeldBackReturnsAtOnce is the hide a person makes
// while the responder waits out a peer that keeps claiming the name. The hide
// does not wait for the probe, and no probe goes out after it.
func TestAHideWhileTheNameIsHeldBackReturnsAtOnce(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		over := newFakeNetwork()
		announcer := fakeAnnouncer(over)

		announcer.Start(t.Context(), 8080)
		defer announcer.Stop(t.Context())

		synctest.Wait()

		for i := range conflictBurst {
			over.deliver(claimOf(renamed("Desk", i, instanceSuffix)), peer, groupIPv4)
		}

		mark, hidden := over.mark(), time.Now()
		announcer.SetHidden(t.Context(), true)

		require.Equal(t, hidden, time.Now(), "the hide waited")
		require.Equal(t, StateHidden, announcer.State())

		time.Sleep(time.Minute)
		synctest.Wait()

		for _, one := range over.since(mark) {
			require.True(t, isGoodbye(one), "a hidden server sent %s", one.msg)
		}
	})
}

// TestAHideWhoseCallerLeftIsStillMade keeps the network in step with the
// file: the setting is stored before the announcer is told, so a caller that
// went away before the change was made cannot leave the server announced.
func TestAHideWhoseCallerLeftIsStillMade(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		over := newFakeNetwork()
		announcer := fakeAnnouncer(over)

		announcer.Start(t.Context(), 8080)
		defer announcer.Stop(t.Context())

		settle()

		gone, cancel := context.WithCancel(t.Context())
		cancel()

		announcer.SetHidden(gone, true)
		synctest.Wait()

		require.Equal(t, StateHidden, announcer.State())
		require.Equal(t, 1, count(over.all(), isGoodbye))
	})
}

// TestAChangeAfterStopIsKeptForTheNextStart is a hide that arrives once the
// announcer has stopped. It blocks nothing, and the next start begins hidden.
func TestAChangeAfterStopIsKeptForTheNextStart(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		over := newFakeNetwork()
		announcer := fakeAnnouncer(over)

		announcer.Start(t.Context(), 8080)
		settle()
		announcer.Stop(t.Context())

		announcer.SetHidden(t.Context(), true)
		require.True(t, announcer.Hidden())

		mark := over.mark()

		announcer.Start(t.Context(), 8080)
		defer announcer.Stop(t.Context())

		settle()
		require.Empty(t, over.since(mark))
		require.Equal(t, StateHidden, announcer.State())
	})
}

// TestHideAndShowReuseOneResponder is the leak a responder per change had:
// twenty changes open nothing new and leave no goroutine behind.
//
// It does not run in parallel, because it counts the goroutines of the whole
// test binary.
func TestHideAndShowReuseOneResponder(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		over := newFakeNetwork()
		announcer := fakeAnnouncer(over)

		announcer.Start(t.Context(), 8080)
		defer announcer.Stop(t.Context())

		settle()

		before := runtime.NumGoroutine()

		for range 20 {
			announcer.SetHidden(t.Context(), true)
			announcer.SetHidden(t.Context(), false)
		}

		settle()

		require.Equal(t, 1, over.opens)
		require.LessOrEqual(t, runtime.NumGoroutine(), before)
	})
}

func TestHidingAnAnnouncerSwitchedOffChangesNothingOnTheNetwork(t *testing.T) {
	t.Parallel()

	over := newFakeNetwork()
	announcer := New(Config{Enabled: false}, quiet(), NewLink(quiet(), nil))
	announcer.open = over.open

	announcer.Start(t.Context(), 8080)
	announcer.SetHidden(t.Context(), true)
	announcer.SetHidden(t.Context(), false)
	announcer.Stop(t.Context())

	require.Zero(t, over.opens)
	require.Empty(t, over.all())
	require.Equal(t, StateOff, announcer.State())
}

// TestTheDescriptionNamesWhatTheRecordsCarry is what the settings page shows:
// the names, the port and the service names stay readable while the server
// is hidden, because they are what showing it again announces.
func TestTheDescriptionNamesWhatTheRecordsCarry(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		over := newFakeNetwork()
		announcer := fakeAnnouncer(over)

		before := announcer.Describe()
		require.Equal(t, "Desk", before.Instance)
		require.Equal(t, "desk.local", before.Host)
		require.Zero(t, before.Port, "a port was named before one was bound")

		announcer.SetHidden(t.Context(), true)
		announcer.Start(t.Context(), 8080)
		defer announcer.Stop(t.Context())

		settle()

		described := announcer.Describe()
		require.Equal(t, "Desk", described.Instance)
		require.Equal(t, "desk.local", described.Host)
		require.Equal(t, 8080, described.Port)
		require.Equal(t, []string{linkService, connectService}, described.Services)
	})
}

func TestAnAnnouncerThatAnnouncesNothingDescribesNothing(t *testing.T) {
	t.Parallel()

	var none *Announcer

	require.Equal(t, Description{}, none.Describe())

	off := New(
		Config{Enabled: false, Services: []string{linkService}, Instance: "Desk"},
		quiet(),
		NewLink(quiet(), nil),
	)
	require.Equal(t, Description{}, off.Describe(), "a switched-off announcer named a host nobody hears")
}
