package install

import (
	"fmt"
	"slices"
	"strings"

	"github.com/prusa3d/prusa-connect/internal/authz"
)

// permissionsOf reads the actions listed under key: those an instance may take
// as itself, or those it may only ask about. It refuses a name the
// authorization vocabulary does not hold and a name written twice. The
// operator reads both at load, before the module makes its first call.
func permissionsOf(key string, declared []string) ([]authz.Action, error) {
	out := make([]authz.Action, 0, len(declared))

	for _, name := range declared {
		action := authz.Action(strings.TrimSpace(name))
		if !action.Valid() {
			return nil, fmt.Errorf("service.%s names %q, which is not an action the "+
				"authorization graph holds; the actions are %v", key, name, authz.ActionNames(authz.Actions()))
		}

		if slices.Contains(out, action) {
			return nil, fmt.Errorf("service.%s names %s twice", key, action)
		}

		out = append(out, action)
	}

	return out, nil
}

// actionsOf reads the actions an instance may take as itself and those it may
// only ask about. It refuses an action under both, since a permission may be
// asked about already.
func actionsOf(declared *ServiceEntry) (permissions, checks []authz.Action, err error) {
	if permissions, err = permissionsOf("permissions", declared.Permissions); err != nil {
		return nil, nil, err
	}

	if checks, err = permissionsOf("checks", declared.Checks); err != nil {
		return nil, nil, err
	}

	for _, action := range checks {
		if slices.Contains(permissions, action) {
			return nil, nil, fmt.Errorf("service.checks names %s, which service.permissions names too: "+
				"a permission may be asked about already", action)
		}
	}

	return permissions, checks, nil
}
