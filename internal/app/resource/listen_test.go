package resource

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config/install"
	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
)

// A module learns its ports and data directory only when it starts, so moving
// a grant has to read as a change that rebuilds the running instance.
func TestAMovedGrantRestartsTheInstance(t *testing.T) {
	t.Parallel()

	service := func(listen map[string]install.ListenPort, data bool, edit ...func(*install.Service)) Spec {
		s := &install.Service{Path: "p", Checksum: "c", ContractMajor: 1, Listen: listen, Data: data}
		for _, e := range edit {
			e(s)
		}

		return Spec{Type: "notes", Enabled: true, Runtime: install.RuntimeService, Service: s}
	}
	at := func(port int) map[string]install.ListenPort {
		return map[string]install.ListenPort{"syslog": {Network: "udp", Port: port}}
	}
	declare := func(spec Spec) Document {
		return Document{Instances: []Declaration{{Ref: ref.Ref{}, Spec: spec}}}
	}

	base := service(at(5514), false)
	for name, moved := range map[string]Spec{
		"port": service(at(6514), false),
		"data": service(at(5514), true),
		"permissions": service(at(5514), false, func(s *install.Service) {
			s.Permissions = []authz.Action{authz.ActionOperateResource}
		}),
		"calls":   service(at(5514), false, func(s *install.Service) { s.Calls = []string{"fleet"} }),
		"records": service(at(5514), false, func(s *install.Service) { s.RecordsMiB = 64 }),
		"absent":  service(at(5514), false, func(s *install.Service) { s.CheckAbsentPeople = true }),
		"people":  service(at(5514), false, func(s *install.Service) { s.ListPeople = true }),
	} {
		require.False(t, base.Equal(moved), name)
		require.NotEqual(t, base.Digest(), moved.Digest(), name)
		require.Equal(t, OutcomeRestart, Diff(declare(base), declare(moved)).Steps[0].Outcome, name)
	}

	require.True(t, base.Equal(service(at(5514), false)))

	// A declaration with no grants renders as it did before the keys existed.
	require.Equal(t, "\x00service\x00p\x00c\x001\x00", service(nil, false).installMode())
}
