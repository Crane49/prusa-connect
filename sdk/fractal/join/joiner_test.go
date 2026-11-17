package join

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
)

// scriptedRequester is a Requester that returns the answers of a list, one per
// request, and repeats the last answer when the list is used up. It records
// each request and the time it arrived.
type scriptedRequester struct {
	mu      sync.Mutex
	answers []answer
	asked   []fractal.JoinRequest
	at      []time.Time
	now     func() time.Time
}

// answer is one entry of that list: a Reply, or the error of a request that
// got no answer.
type answer struct {
	reply Reply
	err   error
}

// Request records the request and returns the next answer of the list.
func (r *scriptedRequester) Request(_ context.Context, req fractal.JoinRequest) (Reply, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.asked = append(r.asked, req)
	r.at = append(r.at, r.now())
	a := r.answers[min(len(r.asked), len(r.answers))-1]
	return a.reply, a.err
}

// calls returns how many requests the Joiner sent.
func (r *scriptedRequester) calls() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.asked)
}

// last returns the most recent request.
func (r *scriptedRequester) last() fractal.JoinRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.asked[len(r.asked)-1]
}

// granted returns a grant whose credentials expire at expires.
func granted(expires time.Time) answer {
	return answer{reply: Reply{Granted: true, Grant: fractal.JoinGrant{
		Version:        fractal.Version,
		NodePath:       "hq/brno/printer-1",
		ParentIdentity: "parent-key",
		Credentials:    "leaf-jwt",
		ExpiresAt:      fractal.TimestampOf(expires),
	}}}
}

// denied returns a denial with the given code.
func denied(code fractal.Code) answer {
	return answer{reply: Reply{Denial: fractal.JoinDenial{Version: fractal.Version, Code: code}}}
}

// newJoiner builds a Joiner that sends through r. Rand always returns one, so
// each backoff time is the full limit and a test can predict it.
func newJoiner(t *testing.T, r *scriptedRequester, options ...func(*JoinerConfig)) (*Joiner, *MemoryMembershipStore) {
	t.Helper()
	store := &MemoryMembershipStore{}
	cfg := JoinerConfig{
		Requester:  r,
		Store:      store,
		NodeID:     "printer-1",
		Identity:   "child-key",
		Endpoint:   "tls://brno:4222",
		Rand:       func() float64 { return 1 },
		MinBackoff: time.Second,
		MaxBackoff: time.Minute,
	}
	for _, option := range options {
		option(&cfg)
	}
	j, err := NewJoiner(cfg)
	if err != nil {
		t.Fatalf("build the joiner: %v", err)
	}
	return j, store
}

// TestTheChildRetriesAnUnansweredRequestWithBackoff covers fractal.md, The
// Join Exchange: a node whose join request gets no answer SHOULD send it again,
// with a backoff, until it gets an answer.
func TestTheChildRetriesAnUnansweredRequestWithBackoff(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		silence := errors.New("no answer")
		r := &scriptedRequester{now: time.Now, answers: []answer{
			{err: silence}, {err: silence}, granted(time.Now().Add(time.Hour)),
		}}
		j, store := newJoiner(t, r)
		start := time.Now()
		m, err := j.Join(context.Background())
		if err != nil {
			t.Fatalf("join: %v", err)
		}
		if r.calls() != 3 {
			t.Errorf("the Joiner sent %d requests, wanted 3", r.calls())
		}
		// Rand returns one, so the waits are the full limits: one second, then
		// two.
		if waited := time.Since(start); waited != 3*time.Second {
			t.Errorf("the Joiner waited %v, wanted 3s", waited)
		}
		if m.NodePath != "hq/brno/printer-1" {
			t.Errorf("the path is %q", m.NodePath)
		}
		held, ok, loadErr := store.LoadMembership(context.Background())
		if loadErr != nil || !ok || held.Credentials != "leaf-jwt" {
			t.Errorf("the membership is %+v, ok %v, err %v", held, ok, loadErr)
		}
	})
}

// TestWhatTravelsBesideTheGrantIsPersisted checks that Join copies Reply.Extra,
// the bytes a transport binding sends beside a grant, to Membership.Binding
// and saves it. The node above does not send these bytes again, and a node
// needs them to connect after a restart.
func TestWhatTravelsBesideTheGrantIsPersisted(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		extra := json.RawMessage(`{"leafRemote":"nats://hq:7422"}`)
		reply := granted(time.Now().Add(time.Hour))
		reply.reply.Extra = extra
		r := &scriptedRequester{now: time.Now, answers: []answer{reply}}
		j, store := newJoiner(t, r)
		m, err := j.Join(context.Background())
		if err != nil {
			t.Fatalf("join: %v", err)
		}
		if string(m.Binding) != string(extra) {
			t.Errorf("Binding in the returned membership is %s, wanted %s", m.Binding, extra)
		}
		held, _, loadErr := store.LoadMembership(context.Background())
		if loadErr != nil {
			t.Fatalf("read the membership: %v", loadErr)
		}
		if string(held.Binding) != string(extra) {
			t.Errorf("Binding in the saved membership is %s, wanted %s", held.Binding, extra)
		}
	})
}

// TestTheChildActsOnTheDenialCode covers fractal.md, Error Codes: a joining
// node MUST decide whether to retry from the code.
func TestTheChildActsOnTheDenialCode(t *testing.T) {
	t.Parallel()
	standing := []fractal.Code{
		fractal.CodeJoinRejected, fractal.CodeJoinCollision, fractal.CodeJoinRevoked,
	}
	for _, code := range standing {
		t.Run("no retry after "+code.String(), func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				r := &scriptedRequester{now: time.Now, answers: []answer{denied(code)}}
				j, _ := newJoiner(t, r)
				_, err := j.Join(context.Background())
				var denial *DenialError
				if !errors.As(err, &denial) || denial.Denial.Code != code {
					t.Fatalf("the error is %v, wanted a denial of %s", err, code)
				}
				if r.calls() != 1 {
					t.Errorf("the Joiner sent %d requests, wanted 1", r.calls())
				}
			})
		})
	}

	t.Run("backoff after join unavailable", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			r := &scriptedRequester{now: time.Now, answers: []answer{
				denied(fractal.CodeJoinUnavailable), granted(time.Now().Add(time.Hour)),
			}}
			j, _ := newJoiner(t, r)
			start := time.Now()
			if _, err := j.Join(context.Background()); err != nil {
				t.Fatalf("join: %v", err)
			}
			if waited := time.Since(start); waited != time.Second {
				t.Errorf("the Joiner waited %v, wanted 1s", waited)
			}
		})
	})

	t.Run("a new credential after join unauthorized", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			fresh := make(chan fractal.JoinCredential, 1)
			r := &scriptedRequester{now: time.Now, answers: []answer{
				denied(fractal.CodeJoinUnauthorized), granted(time.Now().Add(time.Hour)),
			}}
			j, _ := newJoiner(t, r, func(cfg *JoinerConfig) { cfg.Credentials = fresh })
			done := make(chan error, 1)
			go func() { _, err := j.Join(context.Background()); done <- err }()
			synctest.Wait()
			if r.calls() != 1 {
				t.Fatalf("the Joiner sent %d requests before the new credential arrived, wanted 1", r.calls())
			}
			fresh <- "second-token"
			if err := <-done; err != nil {
				t.Fatalf("join: %v", err)
			}
			if got := r.last().Credential; got != "second-token" {
				t.Errorf("the credential in the second request is %q", got)
			}
		})
	})

	t.Run("join unauthorized with no Credentials channel ends the join", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			r := &scriptedRequester{now: time.Now, answers: []answer{denied(fractal.CodeJoinUnauthorized)}}
			j, _ := newJoiner(t, r)
			var denial *DenialError
			if _, err := j.Join(context.Background()); !errors.As(err, &denial) {
				t.Fatalf("the error is %v, wanted a denial", err)
			}
			if r.calls() != 1 {
				t.Errorf("the Joiner sent %d requests, wanted 1", r.calls())
			}
		})
	})

	t.Run("JoinerConfig.Retry is asked before fractal.JoinRetry", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			// awaiting is a refinement of CodeJoinUnauthorized. fractal.JoinRetry
			// says to wait for a new join credential. Retry says to back off.
			const awaiting = fractal.CodeJoinUnauthorized | 1
			r := &scriptedRequester{now: time.Now, answers: []answer{
				denied(awaiting), granted(time.Now().Add(time.Hour)),
			}}
			j, _ := newJoiner(t, r, func(cfg *JoinerConfig) {
				cfg.Retry = func(c fractal.Code) (fractal.Retry, bool) {
					if c == awaiting {
						return fractal.RetryWithBackoff, true
					}
					return 0, false
				}
			})
			if _, err := j.Join(context.Background()); err != nil {
				t.Fatalf("join: %v", err)
			}
			if r.calls() != 2 {
				t.Errorf("the Joiner sent %d requests, wanted 2", r.calls())
			}
		})
	})

	t.Run("a code Retry does not answer gets the rule of fractal.JoinRetry", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			r := &scriptedRequester{now: time.Now, answers: []answer{denied(fractal.CodeJoinRevoked)}}
			j, _ := newJoiner(t, r, func(cfg *JoinerConfig) {
				cfg.Retry = func(fractal.Code) (fractal.Retry, bool) { return 0, false }
			})
			var denial *DenialError
			if _, err := j.Join(context.Background()); !errors.As(err, &denial) {
				t.Fatalf("the error is %v, wanted a denial", err)
			}
			if r.calls() != 1 {
				t.Errorf("the Joiner sent %d requests, wanted 1", r.calls())
			}
		})
	})

	t.Run("a code that is not a join denial code is retried with backoff", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			r := &scriptedRequester{now: time.Now, answers: []answer{
				denied(fractal.CodeInternal), granted(time.Now().Add(time.Hour)),
			}}
			j, _ := newJoiner(t, r)
			if _, err := j.Join(context.Background()); err != nil {
				t.Fatalf("join: %v", err)
			}
			if r.calls() != 2 {
				t.Errorf("the Joiner sent %d requests, wanted 2", r.calls())
			}
		})
	})
}

// TestTheChildRefusesAGrantOfAnUnsupportedVersion covers fractal.md, After the
// Join Exchange: the node MUST NOT bring up the connection and MUST NOT retry.
func TestTheChildRefusesAGrantOfAnUnsupportedVersion(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		bad := granted(time.Now().Add(time.Hour))
		bad.reply.Grant.Version = fractal.Version + 1
		r := &scriptedRequester{now: time.Now, answers: []answer{bad}}
		j, store := newJoiner(t, r)
		_, err := j.Join(context.Background())
		if !errors.Is(err, ErrUnsupportedGrantVersion) {
			t.Fatalf("the error is %v, wanted %v", err, ErrUnsupportedGrantVersion)
		}
		if r.calls() != 1 {
			t.Errorf("the Joiner sent %d requests, wanted 1", r.calls())
		}
		if _, ok, _ := store.LoadMembership(context.Background()); ok {
			t.Error("the Joiner saved a membership from a grant it refused")
		}
	})
}

// TestANodeWithChildrenFollowsItsParentToANewAddress covers fractal.md, After
// the Join Exchange: a node compares the parent identity of the grant with the
// saved one. A node with children asks a new address, and the node that
// granted its Node Path answers there, so it keeps its Node Path.
func TestANodeWithChildrenFollowsItsParentToANewAddress(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		expires := time.Now().Add(time.Hour)
		r := &scriptedRequester{now: time.Now, answers: []answer{granted(expires)}}
		j, store := newJoiner(t, r, func(c *JoinerConfig) {
			c.Endpoint = "tls://brno.example:4222"
			c.HasChildren = func() bool { return true }
		})
		held := Membership{NodePath: "hq/brno/printer-1", ParentIdentity: "parent-key", Endpoint: "tls://brno:4222"}
		if err := store.SaveMembership(context.Background(), held); err != nil {
			t.Fatalf("save the membership: %v", err)
		}
		got, err := j.Join(context.Background())
		if err != nil {
			t.Fatalf("Join: %v", err)
		}
		want := Membership{
			NodePath:       "hq/brno/printer-1",
			ParentIdentity: "parent-key",
			Credentials:    "leaf-jwt",
			ExpiresAt:      fractal.TimestampOf(expires),
			Endpoint:       "tls://brno.example:4222",
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("Join returned %+v, want %+v", got, want)
		}
	})
}

// TestAJoinDuringARefusedRenewalAsksNothing covers fractal.md, After the Join
// Exchange: a node that refuses a grant MUST NOT retry. A Join that starts
// while a renewal checks the grant waits for that check. The check refuses the
// grant, so the Join returns the refusal and sends no request.
func TestAJoinDuringARefusedRenewalAsksNothing(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		r := &scriptedRequester{now: time.Now, answers: []answer{granted(time.Now().Add(time.Hour))}}
		checking, refuse := make(chan struct{}, 1), make(chan struct{})
		refusal := errors.New("the test refuses the grant")
		j, _ := newJoiner(t, r, func(cfg *JoinerConfig) {
			cfg.CheckExtensions = func(map[fractal.Identifier]json.RawMessage) error {
				select {
				case checking <- struct{}{}:
				default:
				}
				<-refuse
				return refusal
			}
		})
		ctx := context.Background()
		renewed, joined := make(chan error, 1), make(chan error, 1)
		go func() { renewed <- j.Renew(ctx, Membership{}) }()
		<-checking
		go func() {
			_, err := j.Join(ctx)
			joined <- err
		}()
		synctest.Wait()
		askedDuringTheCheck := r.calls()
		close(refuse)
		got := map[string]any{
			"asked during the check": askedDuringTheCheck,
			"renewal refused":        errors.Is(<-renewed, refusal),
			"join refused":           errors.Is(<-joined, refusal),
			"asked":                  r.calls(),
		}
		want := map[string]any{
			"asked during the check": 1, "renewal refused": true, "join refused": true, "asked": 1,
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("the Joiner has %v, want %v", got, want)
		}
	})
}

// TestTheChildRenewsBeforeTheCredentialsExpire covers fractal.md,
// Authentication Identity and Connection Credentials: a node that has no valid
// credentials MUST get new ones by running the join exchange again. The
// protocol has no other way to renew them.
func TestTheChildRenewsBeforeTheCredentialsExpire(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		const life = time.Hour
		r := &scriptedRequester{now: time.Now}
		r.answers = []answer{granted(time.Now().Add(life))}
		var grants int
		j, _ := newJoiner(t, r, func(cfg *JoinerConfig) {
			cfg.RenewFraction = 0.5
			cfg.OnGrant = func(context.Context, Decision) { grants++ }
		})
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { done <- j.Run(ctx) }()

		synctest.Wait()
		if r.calls() != 1 {
			t.Fatalf("the Joiner sent %d requests, wanted 1", r.calls())
		}
		time.Sleep(life/2 + time.Millisecond)
		synctest.Wait()
		if r.calls() != 2 {
			t.Fatalf("the Joiner sent %d requests after half the lifetime, wanted 2", r.calls())
		}
		if grants != 2 {
			t.Errorf("OnGrant ran %d times, wanted 2", grants)
		}
		cancel()
		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Errorf("Run returned %v, wanted context.Canceled", err)
		}
	})
}

// TestRenewWaitsForTheHeldCredentials gives Renew a membership whose
// credentials are still valid. It sends no request at once, only after
// RenewFraction of their lifetime.
func TestRenewWaitsForTheHeldCredentials(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		const life = time.Hour
		r := &scriptedRequester{now: time.Now}
		r.answers = []answer{granted(time.Now().Add(2 * life))}
		j, _ := newJoiner(t, r, func(cfg *JoinerConfig) { cfg.RenewFraction = 0.5 })
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		held := Membership{NodePath: "hq/brno/printer-1", ExpiresAt: fractal.TimestampOf(time.Now().Add(life))}
		go func() { done <- j.Renew(ctx, held) }()

		synctest.Wait()
		before := r.calls()
		time.Sleep(life/2 + time.Millisecond)
		synctest.Wait()
		if got := []int{before, r.calls()}; !slices.Equal(got, []int{0, 1}) {
			t.Errorf("the Joiner sent %v requests at the start and after half the lifetime, wanted [0 1]", got)
		}
		cancel()
		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Errorf("Renew returned %v, wanted context.Canceled", err)
		}
	})
}

// TestTheBackoffStaysWithinItsBounds checks the backoff time of each attempt.
// The time is random between zero and a limit. The limit doubles once per
// attempt and stops at MaxBackoff.
func TestTheBackoffStaysWithinItsBounds(t *testing.T) {
	j, _ := newJoiner(t, &scriptedRequester{now: time.Now}, func(cfg *JoinerConfig) {
		cfg.MinBackoff = time.Second
		cfg.MaxBackoff = 8 * time.Second
	})
	want := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 8 * time.Second}
	for attempt, ceiling := range want {
		if got := j.backoff(attempt); got != ceiling {
			t.Errorf("attempt %d waits up to %v, wanted %v", attempt, got, ceiling)
		}
	}
	t.Run("a random factor of zero gives no wait", func(t *testing.T) {
		zero, _ := newJoiner(t, &scriptedRequester{now: time.Now}, func(cfg *JoinerConfig) {
			cfg.Rand = func() float64 { return 0 }
		})
		if got := zero.backoff(4); got != 0 {
			t.Errorf("the wait is %v, wanted none", got)
		}
	})
	t.Run("attempt 4000 still waits at most MaxBackoff", func(t *testing.T) {
		if got := j.backoff(4000); got != 8*time.Second {
			t.Errorf("the wait is %v, wanted 8s", got)
		}
	})
}

// TestTheJoinerRefusesAnIncompleteConfiguration checks that NewJoiner returns
// an error when a required field of JoinerConfig is missing or NodeID is not
// valid.
func TestTheJoinerRefusesAnIncompleteConfiguration(t *testing.T) {
	t.Parallel()
	full := JoinerConfig{
		Requester: &scriptedRequester{now: time.Now},
		Store:     &MemoryMembershipStore{},
		NodeID:    "printer-1",
		Identity:  "child-key",
	}
	cases := map[string]func(*JoinerConfig){
		"no requester": func(cfg *JoinerConfig) { cfg.Requester = nil },
		"no store":     func(cfg *JoinerConfig) { cfg.Store = nil },
		"no identity":  func(cfg *JoinerConfig) { cfg.Identity = "" },
		"a bad nodeid": func(cfg *JoinerConfig) { cfg.NodeID = "-bad" },
	}
	for name, option := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			cfg := full
			option(&cfg)
			if _, err := NewJoiner(cfg); err == nil {
				t.Error("NewJoiner returned no error")
			}
		})
	}
	t.Run("a complete configuration is accepted", func(t *testing.T) {
		t.Parallel()
		j, err := NewJoiner(full)
		if err != nil {
			t.Fatalf("NewJoiner refused a complete configuration: %v", err)
		}
		if j.backoff(0) > defaultMinBackoff {
			t.Error("the default first wait is over one second")
		}
	})
}

// TestTheChildStopsWhenTheContextEnds checks that Join returns when its
// context is cancelled while it waits for a backoff.
func TestTheChildStopsWhenTheContextEnds(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		r := &scriptedRequester{now: time.Now, answers: []answer{{err: errors.New("no answer")}}}
		j, _ := newJoiner(t, r)
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { _, err := j.Join(ctx); done <- err }()
		synctest.Wait()
		cancel()
		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Errorf("Join returned %v, wanted context.Canceled", err)
		}
	})
}

// TestTheJoinerKeepsTheJoinDataOfTheExtensionsItRuns covers fractal.md, After
// the Join Exchange: the child persists the join data of the extensions it
// runs. JoinerConfig.CheckExtensions sees that data first, and its error
// refuses the grant before anything is saved or the link comes up.
func TestTheJoinerKeepsTheJoinDataOfTheExtensionsItRuns(t *testing.T) {
	t.Parallel()
	files := json.RawMessage(`{"version":1}`)
	kept := map[fractal.Identifier]json.RawMessage{"files": files}
	unsupported := errors.New("the files protocol version is not supported")
	cases := []struct {
		name    string
		check   func(map[fractal.Identifier]json.RawMessage) error
		wantErr error
		saved   map[fractal.Identifier]json.RawMessage
		granted int
	}{
		{name: "no check accepts the grant", saved: kept, granted: 1},
		{
			name:    "a check that accepts",
			check:   func(map[fractal.Identifier]json.RawMessage) error { return nil },
			saved:   kept,
			granted: 1,
		},
		{
			name:    "a check that refuses",
			check:   func(map[fractal.Identifier]json.RawMessage) error { return unsupported },
			wantErr: ErrExtensionRefused,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			reply := granted(time.Now().Add(time.Hour))
			reply.reply.Grant.Extensions = map[fractal.Identifier]json.RawMessage{
				"files": files,
				"maps":  json.RawMessage(`{}`),
			}
			r := &scriptedRequester{now: time.Now, answers: []answer{reply}}
			var seen map[fractal.Identifier]json.RawMessage
			grants := 0
			j, store := newJoiner(t, r, func(cfg *JoinerConfig) {
				cfg.Extensions = []fractal.Identifier{"files"}
				cfg.OnGrant = func(context.Context, Decision) { grants++ }
				if c.check != nil {
					cfg.CheckExtensions = func(data map[fractal.Identifier]json.RawMessage) error {
						seen = data
						return c.check(data)
					}
				}
			})
			if _, err := j.Join(t.Context()); !errors.Is(err, c.wantErr) {
				t.Fatalf("Join returned %v, want %v", err, c.wantErr)
			}
			held, _, err := store.LoadMembership(t.Context())
			if err != nil {
				t.Fatalf("read the membership: %v", err)
			}
			if !reflect.DeepEqual(held.Extensions, c.saved) {
				t.Errorf("the saved membership keeps %v, want %v", held.Extensions, c.saved)
			}
			if grants != c.granted {
				t.Errorf("OnGrant ran %d times, want %d", grants, c.granted)
			}
			if c.check != nil && !reflect.DeepEqual(seen, kept) {
				t.Errorf("the check saw %v, want only the extensions the node runs, %v", seen, kept)
			}
		})
	}
}
