package join

import (
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
)

// deepPath is a Node Path of 243 characters for the Parent under test. A Node
// Path may have 255 characters. A join with a Node ID of 60 characters gives a
// path that is too long, and a join with a Node ID of one character does not.
var deepPath = fractal.NodePath(strings.Repeat(strings.Repeat("a", 60)+"/", 3) + strings.Repeat("a", 60))

// TestTheChecksRunInTheOrderTheSpecificationSets compares the names in the
// steps table, in order, with the list of checks in fractal.md, The Join
// Exchange.
func TestTheChecksRunInTheOrderTheSpecificationSets(t *testing.T) {
	t.Parallel()
	want := []string{
		"parent's own node path",
		"protocol version",
		"well-formedness and proof of identity",
		"node id",
		"node path",
		"authorization",
		"admission and uniqueness",
	}
	if len(steps) != len(want) {
		t.Fatalf("the steps table has %d checks, the specification lists %d", len(steps), len(want))
	}
	for i, name := range want {
		if steps[i].Name != name {
			t.Errorf("check %d is %q, the specification puts %q there", i+1, steps[i].Name, name)
		}
	}
}

// TestTheParentAnswersTheFirstCheckThatFails covers fractal.md, The Join
// Exchange: the Parent MUST run the checks in the listed order and answer with
// the code of the first check that fails. Each case sends a request that fails
// two checks next to each other in the list, and reads Outcome.Check to see
// which of them denied it.
func TestTheParentAnswersTheFirstCheckThatFails(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		// options change the ParentConfig before NewParent runs.
		options []func(*ParentConfig)
		// admit is a request the Parent grants before the case runs, while
		// the authorizer still accepts.
		admit fractal.JoinRequest
		// refuse makes the authorizer refuse the request under test.
		refuse    bool
		req       fractal.JoinRequest
		wantCheck string
		wantCode  fractal.Code
	}{
		{
			name:      "1 before 2: no node path and an unsupported version",
			options:   []func(*ParentConfig){func(cfg *ParentConfig) { cfg.Path = "" }},
			req:       fractal.JoinRequest{Version: 99, NodeID: "printer-1", Identity: "child-key"},
			wantCheck: "parent's own node path",
			wantCode:  fractal.CodeJoinUnavailable,
		},
		{
			name:      "2 before 3: an unsupported version and no identity",
			req:       fractal.JoinRequest{Version: 99, NodeID: "printer-1"},
			wantCheck: "protocol version",
			wantCode:  fractal.CodeJoinRejected,
		},
		{
			name:      "3 before 4: no identity and a node id that is not valid",
			req:       fractal.JoinRequest{Version: fractal.Version, NodeID: "bad id"},
			wantCheck: "well-formedness and proof of identity",
			wantCode:  fractal.CodeJoinRejected,
		},
		{
			name:      "4 before 5: a node id that is not valid and a path that is too long",
			options:   []func(*ParentConfig){func(cfg *ParentConfig) { cfg.Path = deepPath }},
			req:       request(fractal.NodeID("-"+strings.Repeat("b", 59)), "child-key"),
			wantCheck: "node id",
			wantCode:  fractal.CodeJoinRejected,
		},
		{
			name:      "5 before 6: a path that is too long and a refused credential",
			options:   []func(*ParentConfig){func(cfg *ParentConfig) { cfg.Path = deepPath }},
			refuse:    true,
			req:       request(fractal.NodeID(strings.Repeat("b", 60)), "child-key"),
			wantCheck: "node path",
			wantCode:  fractal.CodeJoinRejected,
		},
		{
			name:      "6 before 7: a refused credential and a node id another identity owns",
			admit:     request("printer-1", "first-key"),
			refuse:    true,
			req:       request("printer-1", "second-key"),
			wantCheck: "authorization",
			wantCode:  fractal.CodeJoinUnauthorized,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			gate := &switchableAuthorizer{}
			f := newFixture(t, func(cfg *ParentConfig) {
				cfg.Authorizer = gate
				for _, option := range c.options {
					option(cfg)
				}
			})
			if c.admit.Identity != "" {
				f.mustGrant(t, c.admit)
			}
			gate.refuse.Store(c.refuse)
			out := f.join(t, c.req)
			if out.Granted {
				t.Fatalf("the request was granted, wanted a denial at %q", c.wantCheck)
			}
			if out.Check != c.wantCheck {
				t.Errorf("the denial came from %q, wanted %q: %v", out.Check, c.wantCheck, out.Cause)
			}
			if out.Denial.Code != c.wantCode {
				t.Errorf("the code is %s, wanted %s", out.Denial.Code, c.wantCode)
			}
		})
	}
}
