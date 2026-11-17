package config_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config"
)

// TestADeploymentWithNoDiscoveryBlockAnnouncesTheShippedPair is ADR-20260916-b3x rule
// 7. A server nobody configured announces itself, so a client on the same
// network finds a fresh install without anybody editing a file.
func TestADeploymentWithNoDiscoveryBlockAnnouncesTheShippedPair(t *testing.T) {
	t.Parallel()

	core, err := config.Load(write(t, "core:\n  principal: someone\n"))
	require.NoError(t, err)

	require.True(t, core.Discovery.On())
	require.Equal(t,
		[]string{config.ServicePrusaLink, config.ServicePrusaConnect},
		core.Discovery.Services)
}

// TestTheStrictLoaderReadsBothDiscoveryKeys is the other half of rule 7. The
// loader refuses every key it does not read, so a block it does read has to be
// proven read rather than assumed.
func TestTheStrictLoaderReadsBothDiscoveryKeys(t *testing.T) {
	t.Parallel()

	core, err := config.Load(write(t, `core:
  discovery:
    enabled: false
    services:
      - _workshop._udp
`))
	require.NoError(t, err)

	require.False(t, core.Discovery.On(), "enabled: false is the one switch that turns the announcer off")
	require.Equal(t, []string{"_workshop._udp"}, core.Discovery.Services)
}

// TestAKeyInsideTheDiscoveryBlockTheCoreDoesNotReadIsRefused keeps the block
// under the same rule as every other: an operator who misspelled a key is told,
// rather than left with a server that announces the default.
func TestAKeyInsideTheDiscoveryBlockTheCoreDoesNotReadIsRefused(t *testing.T) {
	t.Parallel()

	_, err := config.Load(write(t, `core:
  discovery:
    interval: 30s
`))
	require.ErrorIs(t, err, config.ErrInvalid)
	require.Contains(t, err.Error(), "line 3")
	require.Contains(t, err.Error(), "core.discovery.interval")
}

// TestAServiceNameOutsideTheShapeIsRefusedAtStartup is ADR-20260916-b3x rule 19. A name
// no browser asks for is announced to nobody, and the process would run for
// months before anybody noticed.
func TestAServiceNameOutsideTheShapeIsRefusedAtStartup(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"no leading underscore":       "prusalink._tcp",
		"no transport":                "_prusalink",
		"a transport nothing speaks":  "_prusalink._sctp",
		"a name over fifteen letters": "_aaaaaaaaaaaaaaaa._tcp",
		"an empty name":               "",
	}

	for name, service := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			core, err := config.Load(write(t, "core:\n  principal: someone\n"))
			require.NoError(t, err)

			core.Discovery.Services = []string{service}

			err = core.Validate()
			require.ErrorIs(t, err, config.ErrInvalid)
			require.Contains(t, err.Error(), "core.discovery.services")
			require.Contains(t, err.Error(), config.ServicePrusaLink,
				"the refusal does not show a name that works")
		})
	}
}

// TestTheShippedServiceNamesPassTheirOwnShapeCheck is the positive control. A
// pattern that refused everything would make the case above pass while the
// server announced nothing at all.
func TestTheShippedServiceNamesPassTheirOwnShapeCheck(t *testing.T) {
	t.Parallel()

	core, err := config.Load(write(t, "core:\n  principal: someone\n"))
	require.NoError(t, err)

	require.Equal(t,
		[]string{config.ServicePrusaLink, config.ServicePrusaConnect},
		core.Discovery.Services)
	require.NoError(t, core.Validate())
}

// TestAPrinterStartsVisibleUnlessItsFileHidesIt is the starting value of the
// hidden setting. A new printer is found by the others with nothing to set, and
// a printer model can ship hidden through its file.
func TestAPrinterStartsVisibleUnlessItsFileHidesIt(t *testing.T) {
	t.Parallel()

	visible, err := config.Load(write(t, "core:\n  principal: someone\n"))
	require.NoError(t, err)
	require.False(t, visible.Discovery.Hidden)

	hidden, err := config.Load(write(t, "core:\n  discovery:\n    hidden: true\n"))
	require.NoError(t, err)
	require.True(t, hidden.Discovery.Hidden)
	require.True(t, hidden.Discovery.On(), "hiding a printer is not switching discovery off")
}

// TestTheHostNameIsTheServersUnlessTheFileLeavesItToTheSystem is the
// core.discovery.host_records key (ADR-20261103-qag). A deployment with no
// responder of its own needs nothing set, and one whose operating system
// answers for the host name says so in its file.
func TestTheHostNameIsTheServersUnlessTheFileLeavesItToTheSystem(t *testing.T) {
	t.Parallel()

	own, err := config.Load(write(t, "core:\n  principal: someone\n"))
	require.NoError(t, err)
	require.Equal(t, config.HostRecordsOwn, own.Discovery.HostRecords)
	require.False(t, own.Discovery.SystemHost())

	system, err := config.Load(write(t, "core:\n  discovery:\n    host_records: system\n"))
	require.NoError(t, err)
	require.True(t, system.Discovery.SystemHost())
	require.True(t, system.Discovery.On(), "leaving the host name to the system is not switching discovery off")
}

// TestAnUnknownWayToAnswerForTheHostIsRefused keeps a typo from starting a
// server that answers for nothing.
func TestAnUnknownWayToAnswerForTheHostIsRefused(t *testing.T) {
	t.Parallel()

	core, err := config.Load(write(t, "core:\n  principal: someone\n  discovery:\n    host_records: avahi\n"))
	require.NoError(t, err)

	err = core.Validate()
	require.ErrorIs(t, err, config.ErrInvalid)
	require.ErrorContains(t, err, `core.discovery.host_records is "avahi", expected "own" or "system"`)
}

// TestAServerComesBeforePrusaLinkNearby is the priority of ADR-20261110-vjh: a
// machine that announces both is listed as the server.
func TestAServerComesBeforePrusaLinkNearby(t *testing.T) {
	t.Parallel()

	require.Equal(t, []string{config.ServicePrusaConnect, config.ServicePrusaLink}, config.NearbyServices())
}
