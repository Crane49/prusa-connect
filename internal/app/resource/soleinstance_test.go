package resource_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// linkType is what this case declares the link as. The store resolves no type
// name, so the name only has to pass the check that refuses a module here: a
// link an operator really runs is a module, and a module is installed rather
// than declared through this API.
const linkType = "core/link"

// TestTheAPIRefusesASecondInstanceOfAKindBoundedToOne is the rule at the
// resource API, the other door the loader's refusal has to hold: a server has
// one link. Rewriting the one that exists stays allowed.
func TestTheAPIRefusesASecondInstanceOfAKindBoundedToOne(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	first := refOf(t, ref.KindLink, "first")
	second := refOf(t, ref.KindLink, "second")

	created, err := h.create(
		first,
		spec(linkType, `{"node_id":"workshop","data_dir":"/var/lib/prusa-connect/fractal"}`),
	)
	require.NoError(t, err)

	_, err = h.create(second, spec(linkType, `{"node_id":"garage","data_dir":"/var/lib/prusa-connect/fractal"}`))
	require.Error(t, err)

	var refused *fault.Fault

	require.ErrorAs(t, err, &refused, "the refusal is a fault the API renders")
	require.Contains(t, err.Error(), "link:first is already declared")

	_, err = h.update(first, spec(linkType, `{"node_id":"workshop","data_dir":"/srv/prusa-connect/fractal"}`),
		created.Instance.Version)
	require.NoError(t, err, "the one instance may still be rewritten")
}
