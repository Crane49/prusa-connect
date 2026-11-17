package join

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
)

// grantOf builds a valid grant. parent is the identity of the node that sent
// it, and path is the Node Path it grants.
func grantOf(parent fractal.Identity, path fractal.NodePath) fractal.JoinGrant {
	return fractal.JoinGrant{
		Version:        fractal.Version,
		NodePath:       path,
		ParentIdentity: parent,
		Credentials:    "fresh",
		ExpiresAt:      fractal.TimestampOf(time.Unix(1700003600, 0)),
	}
}

// heldBy builds the Membership a node saved after an earlier grant. The grant
// came from the identity "parent-key" through the endpoint "tls://brno:4222".
func heldBy() Membership {
	return Membership{
		NodePath:       "hq/brno/printer-1",
		ParentIdentity: "parent-key",
		Credentials:    "old",
		ExpiresAt:      fractal.TimestampOf(time.Unix(1700000000, 0)),
		Endpoint:       "tls://brno:4222",
		Binding:        json.RawMessage(`{"maxHeartbeatInterval":5000}`),
	}
}

// TestDecideCoversEveryBranchOfAfterTheJoinExchange covers fractal.md, After
// the Join Exchange, one case per sentence. Each case that ends in a refusal
// has a matching case where Decide accepts the grant.
func TestDecideCoversEveryBranchOfAfterTheJoinExchange(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		situation  Situation
		wantAction Action
		wantReason error
		wantPath   fractal.NodePath
	}{
		{
			name: "a node that has never joined adopts the granted path",
			situation: Situation{
				Grant:    grantOf("parent-key", "hq/brno/printer-1"),
				Endpoint: "tls://brno:4222",
			},
			wantAction: ActionAdopt,
			wantPath:   "hq/brno/printer-1",
		},
		{
			name: "the same identity grants the same path, so the node keeps it",
			situation: Situation{
				Held:     heldBy(),
				Joined:   true,
				Grant:    grantOf("parent-key", "hq/brno/printer-1"),
				Endpoint: "tls://brno:4222",
			},
			wantAction: ActionKeep,
			wantPath:   "hq/brno/printer-1",
		},
		{
			name: "the same identity grants a different path, so the node refuses",
			situation: Situation{
				Held:     heldBy(),
				Joined:   true,
				Grant:    grantOf("parent-key", "hq/brno/printer-9"),
				Endpoint: "tls://brno:4222",
			},
			wantAction: ActionRefuse,
			wantReason: ErrPathDisagreement,
		},
		{
			name: "a different identity answers on the same endpoint, so the node refuses",
			situation: Situation{
				Held:     heldBy(),
				Joined:   true,
				Grant:    grantOf("stranger-key", "hq/brno/printer-1"),
				Endpoint: "tls://brno:4222",
			},
			wantAction: ActionRefuse,
			wantReason: ErrDifferentParent,
		},
		{
			name: "a different identity answers on a different endpoint, so the node adopts",
			situation: Situation{
				Held:     heldBy(),
				Joined:   true,
				Grant:    grantOf("stranger-key", "hq/praha/printer-1"),
				Endpoint: "tls://praha:4222",
			},
			wantAction: ActionAdopt,
			wantPath:   "hq/praha/printer-1",
		},
		{
			name: "a different identity grants the same path",
			situation: Situation{
				Held:     heldBy(),
				Joined:   true,
				Grant:    grantOf("stranger-key", "hq/brno/printer-1"),
				Endpoint: "tls://praha:4222",
			},
			wantAction: ActionAdopt,
			wantPath:   "hq/brno/printer-1",
		},
		{
			name: "a node that has nodes below it refuses a grant from a different identity",
			situation: Situation{
				Held:        heldBy(),
				Joined:      true,
				Grant:       grantOf("stranger-key", "hq/praha/printer-1"),
				Endpoint:    "tls://praha:4222",
				HasChildren: true,
			},
			wantAction: ActionRefuse,
			wantReason: ErrHasChildren,
		},
		{
			name: "a node that has nodes below it keeps the path the same identity grants",
			situation: Situation{
				Held:        heldBy(),
				Joined:      true,
				Grant:       grantOf("parent-key", "hq/brno/printer-1"),
				Endpoint:    "tls://brno:4222",
				HasChildren: true,
			},
			wantAction: ActionKeep,
			wantPath:   "hq/brno/printer-1",
		},
		{
			name: "an unsupported grant version is refused and never retried",
			situation: Situation{
				Grant: fractal.JoinGrant{
					Version:        fractal.Version + 1,
					NodePath:       "hq/brno/printer-1",
					ParentIdentity: "parent-key",
					Credentials:    "fresh",
					ExpiresAt:      fractal.TimestampOf(time.Unix(1700003600, 0)),
				},
				Endpoint: "tls://brno:4222",
			},
			wantAction: ActionRefuse,
			wantReason: ErrUnsupportedGrantVersion,
		},
		{
			name: "a grant with no expiry is refused",
			situation: Situation{
				Grant: fractal.JoinGrant{
					Version:        fractal.Version,
					NodePath:       "hq/brno/printer-1",
					ParentIdentity: "parent-key",
					Credentials:    "fresh",
				},
				Endpoint: "tls://brno:4222",
			},
			wantAction: ActionRefuse,
			wantReason: ErrUnusableGrant,
		},
		{
			name: "a grant with no credentials is refused",
			situation: Situation{
				Grant: fractal.JoinGrant{
					Version:        fractal.Version,
					NodePath:       "hq/brno/printer-1",
					ParentIdentity: "parent-key",
					ExpiresAt:      fractal.TimestampOf(time.Unix(1700003600, 0)),
				},
				Endpoint: "tls://brno:4222",
			},
			wantAction: ActionRefuse,
			wantReason: ErrUnusableGrant,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			d := Decide(c.situation)
			if d.Action != c.wantAction {
				t.Fatalf("the action is %s, wanted %s: %v", d.Action, c.wantAction, d.Reason)
			}
			if c.wantReason != nil && !errors.Is(d.Reason, c.wantReason) {
				t.Errorf("the reason is %v, wanted %v", d.Reason, c.wantReason)
			}
			if c.wantReason == nil && d.Reason != nil {
				t.Errorf("the reason is %v, wanted none", d.Reason)
			}
			if c.wantPath != "" {
				checkMembership(t, d.Membership, c.situation, c.wantPath)
			}
		})
	}
}

// checkMembership covers fractal.md, After the Join Exchange: after a grant
// the joining node MUST save the granted Node Path, the identity of the node
// that granted it, the credentials and the endpoint it joined through.
func checkMembership(t *testing.T, m Membership, s Situation, wantPath fractal.NodePath) {
	t.Helper()
	if m.NodePath != wantPath {
		t.Errorf("the path is %q, wanted %q", m.NodePath, wantPath)
	}
	if m.ParentIdentity != s.Grant.ParentIdentity {
		t.Errorf("ParentIdentity is %q, wanted %q", m.ParentIdentity, s.Grant.ParentIdentity)
	}
	if m.Credentials != s.Grant.Credentials {
		t.Errorf("the credentials are %q, wanted %q", m.Credentials, s.Grant.Credentials)
	}
	if m.ExpiresAt != s.Grant.ExpiresAt {
		t.Errorf("the expiry is %v, wanted %v", m.ExpiresAt, s.Grant.ExpiresAt)
	}
	if m.Endpoint != s.Endpoint {
		t.Errorf("the endpoint is %q, wanted %q", m.Endpoint, s.Endpoint)
	}
}

// TestDecideKeepsWhatTheBindingPersisted covers Membership.Binding, the bytes
// the transport binding saves. On ActionKeep, Decide keeps the saved bytes
// when Situation.Binding is nil, and replaces them when it is set.
func TestDecideKeepsWhatTheBindingPersisted(t *testing.T) {
	t.Parallel()
	held := heldBy()
	base := Situation{
		Held:     held,
		Joined:   true,
		Grant:    grantOf("parent-key", "hq/brno/printer-1"),
		Endpoint: "tls://brno:4222",
	}

	t.Run("a renewal with no binding bytes keeps the saved bytes", func(t *testing.T) {
		t.Parallel()
		d := Decide(base)
		if string(d.Membership.Binding) != string(held.Binding) {
			t.Errorf("the binding field is %s, wanted %s", d.Membership.Binding, held.Binding)
		}
	})
	t.Run("a renewal with new binding bytes replaces the saved bytes", func(t *testing.T) {
		t.Parallel()
		s := base
		s.Binding = json.RawMessage(`{"maxHeartbeatInterval":9000}`)
		d := Decide(s)
		if string(d.Membership.Binding) != string(s.Binding) {
			t.Errorf("the binding field is %s, wanted %s", d.Membership.Binding, s.Binding)
		}
	})
}

// TestActionString checks the text of each Action, which a caller writes to
// its log.
func TestActionString(t *testing.T) {
	t.Parallel()
	cases := map[Action]string{
		ActionAdopt: "adopt", ActionKeep: "keep", ActionRefuse: "refuse", Action(0): "unknown",
	}
	for action, want := range cases {
		if got := action.String(); got != want {
			t.Errorf("action %d is %q, wanted %q", action, got, want)
		}
	}
}

// TestDecideKeepsOnlyTheExtensionsTheNodeRuns covers fractal.md, The Join
// Exchange and After the Join Exchange: a child ignores the join data of an
// extension it does not run, and keeps the join data of those it runs. A
// renewal replaces what the node saved, so data the parent stopped offering
// goes.
func TestDecideKeepsOnlyTheExtensionsTheNodeRuns(t *testing.T) {
	t.Parallel()
	files := json.RawMessage(`{"version":1}`)
	offered := map[fractal.Identifier]json.RawMessage{"files": files, "maps": json.RawMessage(`{}`)}
	heldWithFiles := heldBy()
	heldWithFiles.Extensions = map[fractal.Identifier]json.RawMessage{"files": json.RawMessage(`{"version":0}`)}
	cases := []struct {
		name    string
		held    Membership
		joined  bool
		offered map[fractal.Identifier]json.RawMessage
		runs    []fractal.Identifier
		want    map[fractal.Identifier]json.RawMessage
	}{
		{
			name:    "the node keeps the entry of an extension it runs",
			offered: offered,
			runs:    []fractal.Identifier{"files"},
			want:    map[fractal.Identifier]json.RawMessage{"files": files},
		},
		{
			name:    "a node that runs no extension keeps nothing",
			offered: offered,
		},
		{
			name:    "the parent offers none of the extensions the node runs",
			offered: map[fractal.Identifier]json.RawMessage{"maps": json.RawMessage(`{}`)},
			runs:    []fractal.Identifier{"files"},
		},
		{
			name:   "a renewal without the entry drops the saved one",
			held:   heldWithFiles,
			joined: true,
			runs:   []fractal.Identifier{"files"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			grant := grantOf("parent-key", "hq/brno/printer-1")
			grant.Extensions = c.offered
			d := Decide(Situation{
				Held:       c.held,
				Joined:     c.joined,
				Grant:      grant,
				Endpoint:   "tls://brno:4222",
				Extensions: c.runs,
			})
			if d.Action == ActionRefuse {
				t.Fatalf("Decide refused the grant: %v", d.Reason)
			}
			if !reflect.DeepEqual(d.Membership.Extensions, c.want) {
				t.Errorf("the membership keeps %v, want %v", d.Membership.Extensions, c.want)
			}
		})
	}
}
