package app

import (
	"errors"
	"slices"
	"strings"

	"github.com/prusa3d/prusa-connect/internal/app/resourcehost"
	"github.com/prusa3d/prusa-connect/internal/authz"
)

// moduleStanding is who a module's own work runs as and what it may do.
//
// A module's own work is every call it makes naming no person: a sync loop, a
// poll, a republish. It runs as the instance's own principal, inside the
// ceiling the operator wrote for the instance at install, so a call reaches
// what the module declared it needs and what the instance was granted, and
// never more than either. The ceiling is a list of actions and never a grant:
// listing observe here confers nothing, and the authorization graph still
// decides whether the instance holds observe on each device.
//
// A call naming a person is that person's and is not held to the ceiling. The
// supervision plane has already refused a person it did not hand the module.
//
// A call naming a server that joined this one is held to the ceiling, because
// nobody vouched for it. Only a link may make one, and the link is answering
// for another server rather than serving a request of this one. The graph
// still decides what that server reaches on each printer, and the ceiling is
// what an operator writes to say how far a link may go at all: a link
// installed with observe alone relays readings and drives nothing.
type moduleStanding struct {
	principal string
	ceiling   []authz.Action
	// checks is the actions the instance may only ask about, for a person it
	// serves. They are never in the ceiling, so its own work cannot take them.
	checks []authz.Action
}

// errOutsideCeiling reports a module's own call taking an action the operator
// did not list for the instance. Each door wraps it in its contract's own
// refusal.
var errOutsideCeiling = errors.New("the instance declares no such permission for its own work")

// standingOf is the instance's standing as the supervision plane declares it.
func standingOf(module resourcehost.Module) moduleStanding {
	return moduleStanding{
		principal: sinkPrincipal(module.Instance), ceiling: module.Permissions, checks: module.Checks,
	}
}

// nodePrefix is how a server that joined this one is spelled as a principal.
const nodePrefix = "node:"

// namesANode reports whether this principal is a server that joined this one.
// The spelling is checked and never the grammar: a node id that is not one is
// refused by the graph, which is the one place a principal is parsed.
func namesANode(principal string) bool { return strings.HasPrefix(principal, nodePrefix) }

// as answers who a call runs as. A call naming a person runs as them. One
// naming nobody runs as the instance, and one naming a server that joined this
// one runs as that server. Both of those last two take the action from the
// ceiling the operator wrote, and are refused outside it.
func (s moduleStanding) as(principal string, action authz.Action) (string, error) {
	if principal != "" && !namesANode(principal) {
		return principal, nil
	}

	if !slices.Contains(s.ceiling, action) {
		return "", errOutsideCeiling
	}

	if principal != "" {
		return principal, nil
	}

	return s.principal, nil
}
