package install_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config/install"
)

// moduleListening renders a service block with the given listen lines and a
// data directory.
func moduleListening(listen ...string) []byte {
	block := strings.Builder{}

	block.Write(moduleService("com.github.aux.acme.planner/buddy-debug-logs"))
	block.WriteString(lines("        data: true"))

	if len(listen) > 0 {
		block.WriteString(lines("        listen:"))

		for _, line := range listen {
			block.WriteString(lines("          " + line))
		}
	}

	return []byte(block.String())
}

// The ports an operator granted are read by name, with the network and the
// port, and the data switch beside them (ADR-20261019-3ns rule 8).
func TestTheGrantedPortsAreReadAtLoad(t *testing.T) {
	t.Parallel()

	doc, err := install.Parse("config.yaml", moduleListening("syslog: udp/13514", "api: tcp/8080"))
	require.NoError(t, err)
	require.Empty(t, doc.Failures)
	require.Len(t, doc.Instances, 1)

	service := doc.Instances[0].Service
	require.True(t, service.Data)
	require.Equal(t, map[string]install.ListenPort{
		"syslog": {Network: "udp", Port: 13514},
		"api":    {Network: "tcp", Port: 8080},
	}, service.Listen)
	require.Equal(t, "udp/13514", service.Listen["syslog"].String())
}

// A port written in any other shape, a name the manifest rule refuses, and
// one port granted under two names are refused at load, naming the key.
func TestAPortGrantInAnotherShapeIsRefusedAtLoad(t *testing.T) {
	t.Parallel()

	for name, listen := range map[string][]string{
		"no network":         {"syslog: 13514"},
		"another network":    {"syslog: sctp/13514"},
		"port zero":          {"syslog: udp/0"},
		"port too high":      {"syslog: udp/65536"},
		"a name in capitals": {"Syslog: udp/13514"},
		"one port twice":     {"syslog: udp/13514", "other: udp/13514"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			doc, err := install.Parse("config.yaml", moduleListening(listen...))
			require.NoError(t, err)
			require.Empty(t, doc.Instances)
			require.Len(t, doc.Failures, 1)
			require.ErrorIs(t, doc.Failures[0].Err, install.ErrInvalid)
			require.Contains(t, doc.Failures[0].Err.Error(), "service.listen")
		})
	}
}
