package app

import (
	"context"
	"encoding/json"

	"github.com/prusa3d/prusa-connect/sdk/linkapi"
	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

// readableLink is the interface the links API uses for every link. A link
// connects this server to the servers above and below it. Every link runs in a
// process of its own and answers the sdk/linkapi contract, so the API builds
// the HTTP body from one type, linkapi.Status.
type readableLink interface {
	// Status returns the current state of the link.
	Status(ctx context.Context) (linkapi.Status, error)
	// Approve admits one node below this one.
	Approve(ctx context.Context, node linkapi.NodeID) error
	// Reject removes one node that waits for a decision.
	Reject(ctx context.Context, node linkapi.NodeID) error
	// Revoke removes the admission of one node below this one.
	Revoke(ctx context.Context, node linkapi.NodeID) error
}

// readableLinkOf wraps a running link instance as a readableLink.
//
// A target with a Call method is a link module in its own process, and the
// host calls the sdk/linkapi methods on it. For any other target it returns
// false, and the API reports the link as running with no further details.
func readableLinkOf(target any) (readableLink, bool) {
	caller, ok := target.(linkCaller)
	if !ok {
		return nil, false
	}

	return serviceLink{client: linkapi.NewClient(wireCaller{caller: caller})}, true
}

// linkCaller is the method the host uses to call a link module: one request
// over the session the host opened to the module process. Only the resource
// host for module processes builds a target with this method, so a target
// that has it is a link in its own process.
type linkCaller interface {
	Call(ctx context.Context, method string, params any) (json.RawMessage, error)
}

// wireCaller adapts a linkCaller to the caller interface of sdk/linkapi. The
// two differ only in the type of the method name: sdk/linkapi passes a
// wirerpc.Method and linkCaller takes a string.
type wireCaller struct {
	caller linkCaller
}

func (w wireCaller) Call(
	ctx context.Context, method wirerpc.Method, params any,
) (json.RawMessage, error) {
	return w.caller.Call(ctx, string(method), params)
}

// serviceLink is a link running in a process of its own.
type serviceLink struct {
	client *linkapi.Client
}

var _ readableLink = serviceLink{}

func (l serviceLink) Status(ctx context.Context) (linkapi.Status, error) {
	return l.client.Status(ctx)
}

func (l serviceLink) Approve(ctx context.Context, node linkapi.NodeID) error {
	return l.client.Approve(ctx, linkapi.NodeParams{ID: node})
}

func (l serviceLink) Reject(ctx context.Context, node linkapi.NodeID) error {
	return l.client.Reject(ctx, linkapi.NodeParams{ID: node})
}

func (l serviceLink) Revoke(ctx context.Context, node linkapi.NodeID) error {
	return l.client.Revoke(ctx, linkapi.NodeParams{ID: node})
}
