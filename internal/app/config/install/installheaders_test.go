package install_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config/install"
)

// moduleWithHeaders renders a service block granted the request headers
// named.
func moduleWithHeaders(names ...string) []byte {
	block := strings.Builder{}

	block.Write(moduleService("com.github.aux.acme.planner/spool-tracker"))
	block.WriteString(lines("        request_headers:"))

	for _, name := range names {
		block.WriteString(lines("          - " + name))
	}

	return []byte(block.String())
}

// A granted header is read in its canonical form (ADR-20261118-nnm).
func TestAGrantedRequestHeaderIsReadInItsCanonicalForm(t *testing.T) {
	t.Parallel()

	doc, err := install.Parse("config.yaml", moduleWithHeaders("print-after-upload"))
	require.NoError(t, err)
	require.Empty(t, doc.Failures)
	require.Len(t, doc.Instances, 1)
	require.Equal(t, []string{"Print-After-Upload"}, doc.Instances[0].Service.RequestHeaders)
}

// A hand edit cannot grant a header no module ever gets, a header every
// module gets already, or one header twice. The instance is refused at load,
// naming the key.
func TestAGrantedRequestHeaderAModuleNeverGetsIsRefusedAtLoad(t *testing.T) {
	t.Parallel()

	for name, names := range map[string][]string{
		"the cookie":        {"Cookie"},
		"the bearer token":  {"Authorization"},
		"the API key":       {"x-api-key"},
		"a forwarded value": {"X-Forwarded-For"},
		"a default header":  {"Accept"},
		"not a token":       {"Print After"},
		"not on the list":   {"Overwrite"},
		"twice":             {"Print-After-Upload", "print-after-upload"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			doc, err := install.Parse("config.yaml", moduleWithHeaders(names...))
			require.NoError(t, err)
			require.Empty(t, doc.Instances)
			require.Len(t, doc.Failures, 1)
			require.ErrorIs(t, doc.Failures[0].Err, install.ErrInvalid)
			require.Contains(t, doc.Failures[0].Err.Error(), "service.request_headers")
		})
	}
}
