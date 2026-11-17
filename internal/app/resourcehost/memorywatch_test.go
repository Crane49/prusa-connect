package resourcehost

import (
	"errors"
	"slices"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config/install"
	"github.com/prusa3d/prusa-connect/internal/app/resource"
	"github.com/prusa3d/prusa-connect/internal/extension/registry"
	"github.com/prusa3d/prusa-connect/internal/extension/supervisor"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
)

func TestChildEnvKeepsOnlyTheAllowlist(t *testing.T) {
	t.Parallel()

	host := []string{
		"PATH=/usr/bin", "HOME=/home/pcl", "AWS_SECRET_ACCESS_KEY=hunter2",
		"https_proxy=http://proxy:3128", "LC_ALL=C.UTF-8", "GOMEMLIMIT=1GiB",
	}

	got := childEnv(host, 128<<20, false)

	require.ElementsMatch(t, []string{
		"PATH=/usr/bin", "HOME=/home/pcl", "https_proxy=http://proxy:3128", "LC_ALL=C.UTF-8",
		"GOMEMLIMIT=114085068", "PCL_MEMORY_BUDGET_BYTES=134217728",
	}, got)

	profile := []string{
		`Path=C:\Windows`, `TEMP=C:\Temp`, `TMP=C:\Temp`, `USERPROFILE=C:\Users\pcl`,
		`APPDATA=C:\Users\pcl\AppData\Roaming`, `LOCALAPPDATA=C:\Users\pcl\AppData\Local`,
		"AWS_SECRET_ACCESS_KEY=hunter2",
	}

	require.ElementsMatch(
		t,
		append(profile[:6:6], "GOMEMLIMIT=114085068", "PCL_MEMORY_BUDGET_BYTES=134217728"),
		childEnv(profile, 128<<20, true),
		"on Windows the profile variables pass",
	)
	require.ElementsMatch(
		t,
		[]string{`Path=C:\Windows`, "GOMEMLIMIT=114085068", "PCL_MEMORY_BUDGET_BYTES=134217728"},
		childEnv(profile, 128<<20, false),
		"elsewhere they stay with the server",
	)
}

func TestNearBudgetIsFromEightyPercentAndAbsentWhenUnknown(t *testing.T) {
	t.Parallel()

	watch := newMemoryWatch(100)
	watch.live(1)

	read := func(used int64, err error) httpapi.ResourceStatus {
		watch.sample(time.Now(), used, err)

		var status httpapi.ResourceStatus
		watch.fill(&status)

		return status
	}

	below := read(79, nil)
	require.Equal(t, int64(79), *below.Memory.UsedBytes)
	require.False(t, below.Memory.NearBudget)

	require.True(t, read(80, nil).Memory.NearBudget)

	unknown := read(0, errors.New("no RssAnon"))
	require.Nil(t, unknown.Memory.UsedBytes, "an unread figure is absent, never 0")
	require.False(t, unknown.Memory.NearBudget)
	require.Equal(t, int64(100), unknown.Memory.BudgetBytes)
}

func TestAStartTakesTheBudgetOfTheDeclarationItStarts(t *testing.T) {
	t.Parallel()

	types, err := registry.NewFrom()
	require.NoError(t, err)

	h, err := New(Config{Registry: types})
	require.NoError(t, err)

	instance, err := ref.New(ref.KindSink, "example")
	require.NoError(t, err)

	declared := func(memoryMiB int) resource.Declaration {
		return resource.Declaration{Ref: instance, Spec: resource.Spec{
			Runtime: install.RuntimeService, Service: &install.Service{MemoryMiB: memoryMiB},
		}}
	}

	first := h.watchMemory(declared(64))
	again := h.watchMemory(declared(96))

	require.Same(t, first, again, "the history is kept across starts")

	var status httpapi.ResourceStatus
	again.fill(&status)
	require.Equal(t, int64(96<<20), status.Memory.BudgetBytes)
}

func TestAnUnreadableSampleIsUnknownAndKillsNothing(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		watch := newMemoryWatch(128 << 20)
		stopped := make(chan struct{})
		killed := false

		go watch.watch(nil, stopped,
			func() (int64, error) { return 0, errors.New("no RssAnon") },
			func(supervisor.Stop) { killed = true }, func(supervisor.Stop) {})

		time.Sleep(5*sampleEvery + sampleEvery/2)
		close(stopped)
		synctest.Wait()

		require.False(t, killed)

		samples := watch.Samples()
		require.Len(t, samples, 5)

		for _, sample := range samples {
			require.Equal(t, int64(-1), sample.Bytes)
		}
	})
}

// overWatch runs a watch over readings and returns how often it killed and
// warned, and the status after the last reading.
func overWatch(t *testing.T, mode string, readings ...int64) (kills, warns int, status httpapi.ResourceStatus) {
	t.Helper()

	synctest.Test(t, func(*testing.T) {
		watch := newMemoryWatch(100)
		watch.setMode(mode)

		stopped := make(chan struct{})
		next := 0

		go watch.watch(nil, stopped,
			func() (int64, error) {
				used := readings[min(next, len(readings)-1)]
				next++

				return used, nil
			},
			func(supervisor.Stop) { kills++ }, func(supervisor.Stop) { warns++ })

		time.Sleep(time.Duration(len(readings))*sampleEvery + sampleEvery/2)
		watch.fill(&status)
		close(stopped)
		synctest.Wait()
	})

	return kills, warns, status
}

func TestWarnModeNeverKills(t *testing.T) {
	t.Parallel()

	kills, warns, status := overWatch(t, install.OnOverMemoryWarn, 150, 160)
	require.Zero(t, kills)
	require.Equal(t, 1, warns, "one warning when it goes over, not one per reading")
	require.True(t, status.Memory.OverBudget)

	kills, warns, status = overWatch(t, install.OnOverMemoryWarn, 150, 90)
	require.Zero(t, kills)
	require.Equal(t, 1, warns)
	require.False(t, status.Memory.OverBudget, "a later reading under the budget clears it")
}

func TestARestartedWarnModuleWarnsAgain(t *testing.T) {
	t.Parallel()

	watch := newMemoryWatch(100)
	watch.setMode(install.OnOverMemoryWarn)
	watch.live(1)

	_, _, warned := watch.sample(time.Now(), 150, nil)
	require.True(t, warned)

	watch.live(-1)
	watch.live(1)

	_, kill, warned := watch.sample(time.Now(), 150, nil)
	require.False(t, kill)
	require.True(t, warned, "the new child going straight over warns again")
}

func TestStopModeStillKills(t *testing.T) {
	t.Parallel()

	kills, warns, _ := overWatch(t, "", 90, 150, 160)
	require.Equal(t, 1, kills, "the watch kills once and ends")
	require.Zero(t, warns)
}

func TestTheRingKeepsOneHour(t *testing.T) {
	t.Parallel()

	watch := newMemoryWatch(1 << 40)
	start := time.Now()

	for i := range samplesKept + 100 {
		watch.sample(start.Add(time.Duration(i)*sampleEvery), int64(i), nil)
	}

	samples := watch.Samples()
	require.Len(t, samples, samplesKept)
	require.Equal(t, int64(100), samples[0].Bytes, "the oldest kept is the 101st pushed")
	require.Equal(t, int64(samplesKept+99), samples[len(samples)-1].Bytes)
	require.True(t, slices.IsSortedFunc(samples, func(a, b MemorySample) int { return a.At.Compare(b.At) }))
}

func TestTheHardLimitFollowsTheBudgetOfTheRunningChild(t *testing.T) {
	t.Parallel()

	watch := newMemoryWatch(64)

	var first, second []int64

	old := watch.attach(func(bytes int64) { first = append(first, bytes) })
	watch.setBudget(96)

	watch.attach(func(bytes int64) { second = append(second, bytes) })
	watch.detach(old)
	watch.setBudget(128)

	require.Equal(t, []int64{64, 96}, first, "the first child is limited at start and on an edit")
	require.Equal(t, []int64{96, 128}, second, "a child that ended late removed its successor's limit")
}
