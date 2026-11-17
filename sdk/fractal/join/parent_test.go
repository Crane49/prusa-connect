package join

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
)

// TestTheParentWithoutItsOwnNodePathDeniesEveryRequest covers fractal.md, The
// Join Exchange, check 1: a node that has no Node Path of its own MUST deny
// every join request. The second subtest shows that the same Parent grants the
// request after SetPath.
func TestTheParentWithoutItsOwnNodePathDeniesEveryRequest(t *testing.T) {
	f := newFixture(t, func(cfg *ParentConfig) { cfg.Path = "" })

	t.Run("a Parent with no node path denies", func(t *testing.T) {
		out := f.mustDeny(t, request("printer-1", "child-key"), fractal.CodeJoinUnavailable)
		if !errors.Is(out.Cause, ErrNoNodePath) {
			t.Errorf("the reason is %v, wanted %v", out.Cause, ErrNoNodePath)
		}
		if f.issuer.calls.Load() != 0 {
			t.Error("the issuer was called for a request the first check denied")
		}
	})

	t.Run("the same Parent grants after SetPath", func(t *testing.T) {
		if err := f.parent.SetPath(parentPath); err != nil {
			t.Fatalf("set the path: %v", err)
		}
		grant := f.mustGrant(t, request("printer-1", "child-key"))
		if grant.NodePath != "hq/brno/printer-1" {
			t.Errorf("the granted path is %q", grant.NodePath)
		}
	})
}

// TestTheParentDeniesAnUnsupportedProtocolVersion covers fractal.md, The Join
// Exchange, check 2: a join request of an unsupported version MUST be denied
// and not dropped. A node drops every other message of an unsupported version.
func TestTheParentDeniesAnUnsupportedProtocolVersion(t *testing.T) {
	f := newFixture(t)

	t.Run("an unsupported version is denied and not dropped", func(t *testing.T) {
		r := request("printer-1", "child-key")
		r.Version = fractal.Version + 1
		out := f.mustDeny(t, r, fractal.CodeJoinRejected)
		if !errors.Is(out.Cause, fractal.ErrUnsupportedVersion) {
			t.Errorf("the reason is %v, wanted %v", out.Cause, fractal.ErrUnsupportedVersion)
		}
	})

	t.Run("the supported version is granted", func(t *testing.T) {
		f.mustGrant(t, request("printer-1", "child-key"))
	})
}

// TestTheParentDeniesARequestWhoseProofDoesNotVerify covers fractal.md, The
// Join Exchange, check 3: the request MUST parse, it MUST have every field the
// verification requires, and its signature MUST be valid.
func TestTheParentDeniesARequestWhoseProofDoesNotVerify(t *testing.T) {
	t.Parallel()

	t.Run("a signature that is not valid is denied", func(t *testing.T) {
		t.Parallel()
		broken := errors.New("the signature is not valid")
		f := newFixture(t, func(cfg *ParentConfig) { cfg.Verifier = jsonVerifier{err: broken} })
		out := f.mustDeny(t, request("printer-1", "child-key"), fractal.CodeJoinRejected)
		if !errors.Is(out.Cause, broken) {
			t.Errorf("the reason is %v, wanted %v", out.Cause, broken)
		}
		if f.issuer.calls.Load() != 0 {
			t.Error("the issuer was called for a request whose signature is not valid")
		}
	})

	t.Run("a request with no identity is denied", func(t *testing.T) {
		t.Parallel()
		f := newFixture(t)
		out := f.mustDeny(t, request("printer-1", ""), fractal.CodeJoinRejected)
		if !errors.Is(out.Cause, fractal.ErrMissingIdentity) {
			t.Errorf("the reason is %v, wanted %v", out.Cause, fractal.ErrMissingIdentity)
		}
	})

	t.Run("a request that does not parse is denied", func(t *testing.T) {
		t.Parallel()
		f := newFixture(t)
		out := f.parent.Evaluate(t.Context(), []byte("not json"))
		if out.Granted || out.Denial.Code != fractal.CodeJoinRejected {
			t.Fatalf("the answer is %+v, wanted a denial of %s", out, fractal.CodeJoinRejected)
		}
	})

	t.Run("a request the verifier accepts is granted", func(t *testing.T) {
		t.Parallel()
		f := newFixture(t)
		f.mustGrant(t, request("printer-1", "child-key"))
	})
}

// TestTheParentDeniesANodeIDOutsideItsGrammar covers fractal.md, The Join
// Exchange, check 4: the requested Node ID MUST conform to the Node ID
// definition.
func TestTheParentDeniesANodeIDOutsideItsGrammar(t *testing.T) {
	f := newFixture(t)
	refused := []fractal.NodeID{
		"", "-leading-hyphen", "has a space", "slash/inside",
		fractal.NodeID(strings.Repeat("x", 65)),
	}
	for _, id := range refused {
		t.Run("denied: "+string(id), func(t *testing.T) {
			f.mustDeny(t, request(id, "child-key"), fractal.CodeJoinRejected)
		})
	}
	t.Run("granted: printer-1", func(t *testing.T) {
		f.mustGrant(t, request("printer-1", "child-key"))
	})
}

// TestTheParentDeniesANodePathOverTheBound covers fractal.md, The Join
// Exchange, check 5: the Node Path of the joining node is the path of the
// Parent, a slash and the requested Node ID. It MUST NOT be longer than a Node
// Path may be.
func TestTheParentDeniesANodePathOverTheBound(t *testing.T) {
	f := newFixture(t, func(cfg *ParentConfig) { cfg.Path = deepPath })

	t.Run("a path over 255 characters is denied", func(t *testing.T) {
		long := fractal.NodeID(strings.Repeat("b", 60))
		out := f.mustDeny(t, request(long, "child-key"), fractal.CodeJoinRejected)
		if !errors.Is(out.Cause, fractal.ErrTooLong) {
			t.Errorf("the reason is %v, wanted %v", out.Cause, fractal.ErrTooLong)
		}
	})

	t.Run("a path of an allowed length is granted", func(t *testing.T) {
		grant := f.mustGrant(t, request("b", "child-key"))
		if grant.NodePath != deepPath+"/b" {
			t.Errorf("the granted path is %q", grant.NodePath)
		}
	})
}

// TestTheParentVerifiesTheJoinCredential covers fractal.md, The Join Exchange,
// check 6: the Parent MUST verify the join credential, and MUST skip the check
// when the identity in the request already owns the requested Node ID. It also
// covers fractal-nats.md, Key source unavailable: when the Authorizer could
// not do the check, the answer is CodeJoinUnavailable and never
// CodeJoinUnauthorized.
func TestTheParentVerifiesTheJoinCredential(t *testing.T) {
	t.Parallel()

	t.Run("a refused credential is denied as unauthorized", func(t *testing.T) {
		t.Parallel()
		f := newFixture(t, func(cfg *ParentConfig) { cfg.Authorizer = refusingAuthorizer() })
		out := f.mustDeny(t, request("printer-1", "child-key"), fractal.CodeJoinUnauthorized)
		if !errors.Is(out.Cause, ErrUnauthorized) {
			t.Errorf("the reason is %v, wanted %v", out.Cause, ErrUnauthorized)
		}
	})

	t.Run("an authorizer that could not do the check is answered as unavailable", func(t *testing.T) {
		t.Parallel()
		f := newFixture(t, func(cfg *ParentConfig) { cfg.Authorizer = brokenAuthorizer() })
		f.mustDeny(t, request("printer-1", "child-key"), fractal.CodeJoinUnavailable)
	})

	t.Run("trust on first use authorizes the request", func(t *testing.T) {
		t.Parallel()
		f := newFixture(t)
		f.mustGrant(t, request("printer-1", "child-key"))
	})

	t.Run("the identity that owns the node id is not checked again", func(t *testing.T) {
		t.Parallel()
		var refuse bool
		f := newFixture(t, func(cfg *ParentConfig) {
			cfg.Authorizer = AuthorizerFunc(func(context.Context, fractal.JoinRequest, fractal.NodePath) error {
				if refuse {
					return ErrUnauthorized
				}
				return nil
			})
		})
		f.mustGrant(t, request("printer-1", "child-key"))
		refuse = true
		f.mustGrant(t, request("printer-1", "child-key"))
		f.mustDeny(t, request("printer-2", "other-key"), fractal.CodeJoinUnauthorized)
	})
}

// TestTheParentCreatesNothingForADeniedChild covers fractal.md, The Join
// Exchange: the Parent MUST NOT create any resources for the joining node
// until every check has passed. The test looks at two resources: the calls to
// the Issuer and the records in the store.
func TestTheParentCreatesNothingForADeniedChild(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		option  func(*ParentConfig)
		request fractal.JoinRequest
	}{
		{"no node path", func(cfg *ParentConfig) { cfg.Path = "" }, request("printer-1", "child-key")},
		{"bad version", nil, fractal.JoinRequest{Version: 99, NodeID: "printer-1", Identity: "child-key"}},
		{"no identity", nil, request("printer-1", "")},
		{"bad node id", nil, request("bad id", "child-key")},
		{
			"unauthorized",
			func(cfg *ParentConfig) { cfg.Authorizer = refusingAuthorizer() },
			request("printer-1", "child-key"),
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			var options []func(*ParentConfig)
			if c.option != nil {
				options = append(options, c.option)
			}
			f := newFixture(t, options...)
			f.join(t, c.request)
			if f.issuer.calls.Load() != 0 {
				t.Error("the issuer was called")
			}
			records, err := f.store.LoadAdmissions(t.Context())
			if err != nil {
				t.Fatalf("read the store: %v", err)
			}
			if len(records) != 0 {
				t.Errorf("the store has %d records, wanted none", len(records))
			}
		})
	}
}

// TestTheGrantCarriesEverythingTheChildNeeds covers fractal.md, The Join
// Exchange: on success the Parent MUST record the admission, issue connection
// credentials, and answer with a grant. The grant has the Node Path of the
// joining node, the identity of the Parent, the credentials and their expiry
// time. It also covers Authentication Identity and Connection Credentials: the
// credentials MUST be issued for the identity of the joining node, and they
// MUST expire.
func TestTheGrantCarriesEverythingTheChildNeeds(t *testing.T) {
	f := newFixture(t)
	grant := f.mustGrant(t, request("printer-1", "child-key"))

	t.Run("the grant is well formed", func(t *testing.T) {
		if err := grant.Validate(); err != nil {
			t.Fatalf("the grant is not well formed: %v", err)
		}
	})
	t.Run("the grant has the node path of the joining node", func(t *testing.T) {
		if grant.NodePath != "hq/brno/printer-1" {
			t.Errorf("the path is %q", grant.NodePath)
		}
	})
	t.Run("the grant has the identity of the Parent", func(t *testing.T) {
		if grant.ParentIdentity != "parent-key" {
			t.Errorf("ParentIdentity is %q", grant.ParentIdentity)
		}
	})
	t.Run("the credentials are issued for the identity in the request", func(t *testing.T) {
		if got := f.issuer.bound("printer-1"); got != "child-key" {
			t.Errorf("the credentials were issued for %q", got)
		}
	})
	t.Run("the credentials have an expiry time", func(t *testing.T) {
		if grant.ExpiresAt == 0 {
			t.Error("the grant has no expiry time")
		}
	})
	t.Run("the admission is recorded", func(t *testing.T) {
		a, ok := f.parent.Admission("printer-1")
		if !ok || a.Identity != "child-key" || a.Revoked {
			t.Errorf("the admission is %+v, ok %v", a, ok)
		}
	})
}

// TestTheParentDeniesARevokedChild covers fractal.md, Admission and
// Revocation: a Parent MUST deny a revoked node, under any authentication
// identity, and MUST NOT issue connection credentials to it. The connection
// credentials a revoked node already has MUST stop working.
func TestTheParentDeniesARevokedChild(t *testing.T) {
	f := newFixture(t)
	f.mustGrant(t, request("printer-1", "child-key"))
	issued := f.issuer.calls.Load()
	if err := f.parent.Revoke(t.Context(), "printer-1"); err != nil {
		t.Fatalf("revoke: %v", err)
	}

	t.Run("the revoked node is denied under its own identity", func(t *testing.T) {
		out := f.mustDeny(t, request("printer-1", "child-key"), fractal.CodeJoinRevoked)
		if !errors.Is(out.Cause, ErrRevoked) {
			t.Errorf("the reason is %v, wanted %v", out.Cause, ErrRevoked)
		}
	})
	t.Run("the revoked node id is denied under another identity", func(t *testing.T) {
		f.mustDeny(t, request("printer-1", "another-key"), fractal.CodeJoinRevoked)
	})
	t.Run("no credentials are issued to a revoked node", func(t *testing.T) {
		if f.issuer.calls.Load() != issued {
			t.Error("the issuer was called for a revoked node")
		}
	})
	t.Run("Revoke called the revocation hook once", func(t *testing.T) {
		if f.hook.seen() != 1 {
			t.Errorf("the hook recorded %d revocations, wanted 1", f.hook.seen())
		}
	})
	t.Run("another node id under the same Parent still joins", func(t *testing.T) {
		f.mustGrant(t, request("printer-2", "second-key"))
	})
	t.Run("revoking an unknown node id is an error", func(t *testing.T) {
		if err := f.parent.Revoke(t.Context(), "nobody"); !errors.Is(err, ErrUnknownChild) {
			t.Errorf("the error is %v, wanted %v", err, ErrUnknownChild)
		}
	})
}

// TestARevocationDuringARejoinStands covers fractal.md, Admission and
// Revocation: a Parent MUST deny a revoked node and MUST NOT issue connection
// credentials to it. printer-1 was admitted before the Parent started. An
// operator revokes it while its rejoin waits in Issue. The rejoin MUST end in a denial, and the admission MUST stay revoked.
func TestARevocationDuringARejoinStands(t *testing.T) {
	held := newGate(nil)
	admitted := &MemoryAdmissionStore{}
	if err := admitted.SaveAdmission(t.Context(), Admission{NodeID: "printer-1", Identity: "child-key"}); err != nil {
		t.Fatalf("save the admission: %v", err)
	}
	f := newFixture(t, func(cfg *ParentConfig) {
		cfg.Store = admitted
		cfg.Issuer = newGatedIssuer(held)
	})

	rejoined := make(chan Outcome, 1)
	go func() { rejoined <- f.join(t, request("printer-1", "child-key")) }()
	<-held.entered
	if err := f.parent.Revoke(t.Context(), "printer-1"); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	close(held.release)
	out := <-rejoined
	a, _ := f.parent.Admission("printer-1")

	got := map[string]any{
		"rejoin":  out.Denial.Code,
		"revoked": a.Revoked,
		"next":    f.join(t, request("printer-1", "child-key")).Denial.Code,
	}
	want := map[string]any{
		"rejoin": fractal.CodeJoinRevoked, "revoked": true, "next": fractal.CodeJoinRevoked,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("after the revocation the Parent has %v, wanted %v", got, want)
	}
}

// TestARevocationDuringTheFirstConnectionStands covers fractal.md, Admission
// and Revocation: a Parent MUST persist whether an admission was revoked, and
// MUST deny a revoked node. An operator revokes printer-1 while MarkConnected
// saves its first connection. Both writes MUST stay in the record.
func TestARevocationDuringTheFirstConnectionStands(t *testing.T) {
	store := &gatedStore{held: newGate(nil)}
	f := newFixture(t, func(cfg *ParentConfig) { cfg.Store = store })
	f.mustGrant(t, request("printer-1", "child-key"))

	connected := make(chan error, 1)
	go func() { connected <- f.parent.MarkConnected(t.Context(), "printer-1") }()
	<-store.held.entered
	revoked := make(chan error, 1)
	go func() { revoked <- f.parent.Revoke(t.Context(), "printer-1") }()
	// A Revoke that waits for MarkConnected sends nothing on revoked, so only
	// a deadline ends this wait.
	select {
	case err := <-revoked:
		revoked <- err
	case <-time.After(100 * time.Millisecond):
	}
	close(store.held.release)
	if err := errors.Join(<-connected, <-revoked); err != nil {
		t.Fatalf("mark connected and revoke: %v", err)
	}

	inMemory, _ := f.parent.Admission("printer-1")
	saved, err := store.LoadAdmissions(t.Context())
	if err != nil {
		t.Fatalf("load the admissions: %v", err)
	}
	got := map[string]any{
		"memory": inMemory,
		"store":  saved,
		"next":   f.join(t, request("printer-1", "child-key")).Denial.Code,
	}
	want := map[string]any{
		"memory": Admission{NodeID: "printer-1", Identity: "child-key", Revoked: true, EverConnected: true},
		"store": []Admission{
			{NodeID: "printer-1", Identity: "child-key", Revoked: true, EverConnected: true},
		},
		"next": fractal.CodeJoinRevoked,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("after the revocation the Parent has %v, wanted %v", got, want)
	}
}

// TestTheParentAnswersAConnectedChild covers fractal.md, Authentication
// Identity and Connection Credentials: a Parent MUST answer a request for new
// credentials while the node that sent it is connected, and MUST NOT treat the
// request as a connection loss. The test checks Admission.EverConnected, the
// flag a transport binding reads before it starts to watch whether a node is
// still connected.
func TestTheParentAnswersAConnectedChild(t *testing.T) {
	f := newFixture(t)
	first := f.mustGrant(t, request("printer-1", "child-key"))
	if err := f.parent.MarkConnected(t.Context(), "printer-1"); err != nil {
		t.Fatalf("mark connected: %v", err)
	}

	second := f.mustGrant(t, request("printer-1", "child-key"))
	t.Run("the second grant has new credentials", func(t *testing.T) {
		if second.Credentials == first.Credentials {
			t.Error("the second grant has the credentials of the first grant")
		}
	})
	t.Run("the admission still has EverConnected set", func(t *testing.T) {
		a, _ := f.parent.Admission("printer-1")
		if !a.EverConnected {
			t.Error("the second join cleared EverConnected, as a connection loss would")
		}
	})
}

// TestTheParentRestoresItsAdmissionsBeforeItAnswers covers fractal.md,
// Admission and Revocation: a Parent MUST load the admission records before it
// answers join requests. The second Parent in the test uses the store of the
// first one and an Authorizer that refuses every credential.
func TestTheParentRestoresItsAdmissionsBeforeItAnswers(t *testing.T) {
	store := &MemoryAdmissionStore{}
	first := newFixture(t, func(cfg *ParentConfig) { cfg.Store = store })
	first.mustGrant(t, request("printer-1", "child-key"))
	first.mustGrant(t, request("printer-2", "second-key"))
	if err := first.parent.Revoke(t.Context(), "printer-1"); err != nil {
		t.Fatalf("revoke: %v", err)
	}

	second := newFixture(t, func(cfg *ParentConfig) {
		cfg.Store = store
		cfg.Authorizer = refusingAuthorizer()
	})
	t.Run("the revoked node is still denied", func(t *testing.T) {
		second.mustDeny(t, request("printer-1", "child-key"), fractal.CodeJoinRevoked)
	})
	t.Run("an admitted node is granted with no authorization check", func(t *testing.T) {
		second.mustGrant(t, request("printer-2", "second-key"))
	})
	t.Run("a node with no admission needs a join credential", func(t *testing.T) {
		second.mustDeny(t, request("printer-3", "third-key"), fractal.CodeJoinUnauthorized)
	})
	t.Run("NewParent fails when the store cannot load", func(t *testing.T) {
		broken := &MemoryAdmissionStore{LoadErr: errors.New("the disk is gone")}
		_, err := NewParent(t.Context(), ParentConfig{
			Identity: "parent-key", Path: parentPath, Store: broken,
			Verifier: jsonVerifier{}, Authorizer: TOFU{}, Issuer: &countingIssuer{},
		})
		if err == nil {
			t.Fatal("NewParent returned no error for a store that cannot load")
		}
	})
}

// TestTheParentBoundsJoins covers fractal-nats.md, Bootstrap Account and Join
// Endpoint: a Parent limits how many join requests it accepts per second and
// how many it evaluates at the same time. It MUST deny a request over either
// limit with CodeJoinUnavailable and not drop it.
func TestTheParentBoundsJoins(t *testing.T) {
	t.Parallel()

	t.Run("a request over the per-second limit is denied and not dropped", func(t *testing.T) {
		t.Parallel()
		clock := &fixedClock{now: time.Unix(1700000000, 0)}
		f := newFixture(t, func(cfg *ParentConfig) {
			cfg.Clock = clock
			cfg.JoinsPerSecond = 1
			cfg.JoinBurst = 1
		})
		f.mustGrant(t, request("printer-1", "child-key"))
		out := f.mustDeny(t, request("printer-2", "second-key"), fractal.CodeJoinUnavailable)
		if !errors.Is(out.Cause, ErrTooManyJoins) {
			t.Errorf("the reason is %v, wanted %v", out.Cause, ErrTooManyJoins)
		}
		clock.advance(2 * time.Second)
		f.mustGrant(t, request("printer-2", "second-key"))
	})

	t.Run("a request over MaxJoinsInFlight is denied", func(t *testing.T) {
		t.Parallel()
		enter := make(chan struct{})
		leave := make(chan struct{})
		f := newFixture(t, func(cfg *ParentConfig) {
			cfg.MaxJoinsInFlight = 1
			cfg.Authorizer = AuthorizerFunc(func(context.Context, fractal.JoinRequest, fractal.NodePath) error {
				enter <- struct{}{}
				<-leave
				return nil
			})
		})
		done := make(chan Outcome, 1)
		go func() { done <- f.parent.Evaluate(t.Context(), envelope(t, request("printer-1", "child-key"))) }()
		<-enter
		f.mustDeny(t, request("printer-2", "second-key"), fractal.CodeJoinUnavailable)
		close(leave)
		if out := <-done; !out.Granted {
			t.Fatalf("the first request was denied at %q with %s", out.Check, out.Denial.Code)
		}
	})
}

// TestTheParentRefusesAnIncompleteConfiguration checks that NewParent returns
// an error when a required field of ParentConfig is missing or Path is not
// valid.
func TestTheParentRefusesAnIncompleteConfiguration(t *testing.T) {
	t.Parallel()
	full := ParentConfig{
		Identity: "parent-key", Path: parentPath, Store: &MemoryAdmissionStore{},
		Verifier: jsonVerifier{}, Authorizer: TOFU{}, Issuer: &countingIssuer{},
	}
	cases := map[string]func(*ParentConfig){
		"no identity":   func(cfg *ParentConfig) { cfg.Identity = "" },
		"no store":      func(cfg *ParentConfig) { cfg.Store = nil },
		"no verifier":   func(cfg *ParentConfig) { cfg.Verifier = nil },
		"no authorizer": func(cfg *ParentConfig) { cfg.Authorizer = nil },
		"no issuer":     func(cfg *ParentConfig) { cfg.Issuer = nil },
		"a bad path":    func(cfg *ParentConfig) { cfg.Path = "hq//brno" },
		"an extension name outside the identifier grammar": func(cfg *ParentConfig) {
			cfg.Extensions = map[fractal.Identifier]json.RawMessage{"9files": json.RawMessage(`{}`)}
		},
	}
	for name, option := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			cfg := full
			option(&cfg)
			if _, err := NewParent(t.Context(), cfg); err == nil {
				t.Error("NewParent returned no error")
			}
		})
	}
	t.Run("a complete configuration is accepted", func(t *testing.T) {
		t.Parallel()
		if _, err := NewParent(t.Context(), full); err != nil {
			t.Errorf("NewParent refused a complete configuration: %v", err)
		}
	})
}

// TestAStoreThatCannotBeWrittenDeniesTheJoin checks the answer of a Parent
// whose store cannot save an admission. The Parent denies the request with
// CodeJoinUnavailable, so the joining node tries again after a backoff. The
// Parent admits no node whose record it could not save.
func TestAStoreThatCannotBeWrittenDeniesTheJoin(t *testing.T) {
	t.Parallel()
	store := &MemoryAdmissionStore{SaveErr: errors.New("the disk is full")}
	f := newFixture(t, func(cfg *ParentConfig) { cfg.Store = store })
	f.mustDeny(t, request("printer-1", "child-key"), fractal.CodeJoinUnavailable)
	if _, ok := f.parent.Admission("printer-1"); ok {
		t.Error("the Parent has an admission that the store did not save")
	}
	store.SaveErr = nil
	f.mustGrant(t, request("printer-1", "child-key"))
}

// TestNoCredentialIsIssuedBeforeTheRecordIsSaved checks the order fractal.md
// states for a granted request: record the admission, then issue the
// connection credentials. With a store that cannot write, the Parent denies
// and never calls the issuer, so no credential exists for a node this Parent
// has no record of.
func TestNoCredentialIsIssuedBeforeTheRecordIsSaved(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.store.SaveErr = errors.New("the disk is full")
	f.mustDeny(t, request("printer-1", "child-key"), fractal.CodeJoinUnavailable)
	if got := f.issuer.calls.Load(); got != 0 {
		t.Errorf("the issuer ran %d times, want none: the record is written first", got)
	}
}

// TestTheGrantCarriesTheOfferedExtensions covers fractal.md, The Join
// Exchange: the grant carries the join data of the extensions the parent
// offers.
func TestTheGrantCarriesTheOfferedExtensions(t *testing.T) {
	t.Parallel()
	offered := map[fractal.Identifier]json.RawMessage{"files": json.RawMessage(`{"version":1}`)}
	f := newFixture(t, func(cfg *ParentConfig) { cfg.Extensions = offered })
	grant := f.mustGrant(t, request("printer-1", "child-key"))
	if !reflect.DeepEqual(grant.Extensions, offered) {
		t.Errorf("the grant carries %v, want %v", grant.Extensions, offered)
	}
}
