package linkapi

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

// Link is the interface a link module implements. The handlers from Handlers
// call it when the host calls the module. Client has the same methods, so it
// implements Link too.
type Link interface {
	// Status returns the link's current Status.
	Status(ctx context.Context) (Status, error)
	// Approve admits one node below this one. It returns ErrNoNode for a node
	// id the link does not know.
	Approve(ctx context.Context, params NodeParams) error
	// Reject removes one node that waits for a decision. It returns ErrNoNode
	// for a node id that waits for no decision.
	Reject(ctx context.Context, params NodeParams) error
	// Revoke takes admission away from one node below this one. It returns
	// ErrNoNode for a node id the link does not know.
	Revoke(ctx context.Context, params NodeParams) error
}

// Handlers returns the handlers for MethodStatus, MethodApprove, MethodReject
// and MethodRevoke. A link module installs them on its session. Handlers
// returns an empty set for a nil link.
func Handlers(link Link) wirerpc.Methods {
	if link == nil {
		return wirerpc.Methods{}
	}

	return wirerpc.Methods{
		MethodStatus:  statusHandler(link),
		MethodApprove: decideHandler(link.Approve),
		MethodReject:  decideHandler(link.Reject),
		MethodRevoke:  decideHandler(link.Revoke),
	}
}

// statusHandler answers MethodStatus. It validates the Status first and
// answers CodeInvalidReading for an invalid one.
func statusHandler(link Link) wirerpc.Handler {
	return func(ctx context.Context, _ wirerpc.Call) (any, error) {
		status, err := link.Status(ctx)
		if err != nil {
			return nil, refusal(err, ModuleFailed)
		}

		if err = status.Validate(); err != nil {
			return nil, refusal(err, ModuleFailed)
		}

		return status, nil
	}
}

// decideHandler answers one call that names a node: MethodApprove,
// MethodReject or MethodRevoke. act applies the decision.
func decideHandler(act func(context.Context, NodeParams) error) wirerpc.Handler {
	return func(ctx context.Context, call wirerpc.Call) (any, error) {
		params, err := read[NodeParams](call)
		if err != nil {
			return nil, err
		}

		if err = params.Validate(); err != nil {
			return nil, refusal(err, ModuleFailed)
		}

		if err = act(ctx, params); err != nil {
			return nil, refusal(err, ModuleFailed)
		}

		return struct{}{}, nil
	}
}

// Changes is the interface the host implements to receive MethodChanged. The
// handler from ChangedHandlers calls it. Reporter has the same method, so it
// implements Changes too.
type Changes interface {
	// Changed receives a link's new Status.
	Changed(ctx context.Context, params ChangedParams) error
}

// ChangedHandlers returns the handler for MethodChanged. The host installs it
// on the session it opened to one link module. ChangedHandlers returns an
// empty set for a nil Changes.
func ChangedHandlers(changes Changes) wirerpc.Methods {
	if changes == nil {
		return wirerpc.Methods{}
	}

	return wirerpc.Methods{MethodChanged: changedHandler(changes)}
}

// changedHandler answers MethodChanged. It validates the Status again on the
// host, because a module written without this package may not have checked.
func changedHandler(changes Changes) wirerpc.Handler {
	return func(ctx context.Context, call wirerpc.Call) (any, error) {
		params, err := read[ChangedParams](call)
		if err != nil {
			return nil, err
		}

		if err = params.Validate(); err != nil {
			return nil, refusal(err, HostFailed)
		}

		if err = changes.Changed(ctx, params); err != nil {
			return nil, refusal(err, HostFailed)
		}

		return struct{}{}, nil
	}
}

// read decodes the params object of one call. A missing params object, or one
// that does not decode, returns CodeInvalidParams.
func read[T any](call wirerpc.Call) (T, error) {
	var params T

	if len(call.Params) == 0 {
		return params, &wirerpc.Error{
			Code:    wirerpc.CodeInvalidParams,
			Message: fmt.Sprintf("%s takes a params object", call.Method),
		}
	}

	if err := json.Unmarshal(call.Params, &params); err != nil {
		return params, &wirerpc.Error{
			Code:    wirerpc.CodeInvalidParams,
			Message: fmt.Sprintf("%s: the params object could not be read", call.Method),
		}
	}

	return params, nil
}
