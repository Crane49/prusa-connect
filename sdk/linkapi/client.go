package linkapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

// Caller is the one method of a wirerpc session that Client and Reporter use.
// *wirerpc.Conn implements it. A test can pass its own Caller, so it needs no
// second process.
type Caller interface {
	// Call sends one request and returns the body of the answer.
	Call(ctx context.Context, method wirerpc.Method, params any) (json.RawMessage, error)
}

// Client makes the calls of the host on one link module. The host creates it
// with the session it opened to that module.
type Client struct {
	conn Caller
}

// NewClient returns a Client that calls the module at the other end of conn.
// It does not check that the module answers these methods.
func NewClient(conn Caller) *Client {
	return &Client{conn: conn}
}

var _ Link = (*Client)(nil)

// Status asks the module for the link's Status. It validates the answer with
// Status.Validate and returns ErrInvalidReading when the check fails.
func (c *Client) Status(ctx context.Context) (Status, error) {
	var status Status

	if err := call(ctx, c.conn, MethodStatus, StatusParams{}, &status); err != nil {
		return Status{}, err
	}

	if err := status.Validate(); err != nil {
		return Status{}, fmt.Errorf("linkapi: %s: %w", MethodStatus, err)
	}

	return status, nil
}

// Approve admits one node below this one. It returns ErrNoNode for a node id
// the link does not know. Approving an admitted node changes nothing and
// returns no error, so a caller can retry.
func (c *Client) Approve(ctx context.Context, params NodeParams) error {
	return c.decide(ctx, MethodApprove, params)
}

// Reject removes one node that waits for a decision. The node leaves Status
// and its id is free again. It returns ErrNoNode for a node that waits for no
// decision, including one that has joined. Remove a joined node with Revoke.
func (c *Client) Reject(ctx context.Context, params NodeParams) error {
	return c.decide(ctx, MethodReject, params)
}

// Revoke takes admission away from one node below this one. The node stays in
// Status with AdmissionRevoked. Revoke returns ErrNoNode for a node id the
// link does not know. Revoking a revoked node changes nothing.
func (c *Client) Revoke(ctx context.Context, params NodeParams) error {
	return c.decide(ctx, MethodRevoke, params)
}

// decide makes the call for Approve, Reject and Revoke. It validates the node
// id first, so an empty id makes no call.
func (c *Client) decide(ctx context.Context, method wirerpc.Method, params NodeParams) error {
	if err := params.Validate(); err != nil {
		return fmt.Errorf("linkapi: %s: %w", method, err)
	}

	return call(ctx, c.conn, method, params, nil)
}

// Reporter makes the call of a link module on the host. The module uses it to
// send its new Status when the Status changes. The module creates it with the
// session the host opened.
type Reporter struct {
	conn Caller
}

// NewReporter returns a Reporter that calls the host at the other end of conn.
// A host that does not answer MethodChanged answers CodeMethodNotFound. A
// module may ignore that error, because the host still calls MethodStatus.
func NewReporter(conn Caller) *Reporter {
	return &Reporter{conn: conn}
}

var _ Changes = (*Reporter)(nil)

// Changed sends the link's whole new Status to the host. It validates the
// Status first and returns ErrInvalidReading without making a call when the
// check fails.
func (r *Reporter) Changed(ctx context.Context, params ChangedParams) error {
	if err := params.Validate(); err != nil {
		return fmt.Errorf("linkapi: %s: %w", MethodChanged, err)
	}

	return call(ctx, r.conn, MethodChanged, params, nil)
}

// call sends one request and decodes the answer body into into, which is nil
// for a method with no answer body. An error code of this contract comes back
// as its error variable, such as ErrNoNode.
func call(ctx context.Context, conn Caller, method wirerpc.Method, params, into any) error {
	if conn == nil {
		return fmt.Errorf("linkapi: %s: the client has no session", method)
	}

	body, err := conn.Call(ctx, method, params)
	if err != nil {
		if coded, ok := errors.AsType[*wirerpc.Error](err); ok {
			if refused := refusalOf(method, coded); refused != nil {
				return refused
			}
		}

		return fmt.Errorf("linkapi: %s: %w", method, err)
	}

	if into == nil || len(body) == 0 {
		return nil
	}

	if err = json.Unmarshal(body, into); err != nil {
		return fmt.Errorf("linkapi: %s: read the answer: %w", method, err)
	}

	return nil
}
