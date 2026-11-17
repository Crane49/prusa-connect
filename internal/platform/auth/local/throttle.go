package local

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/prusa3d/prusa-connect/internal/kernel/buildprofile"
)

// The failure budget one account gets, and the limit of what it can be.
//
// NIST SP 800-63B section 5.2.2 asks a verifier for two things that conflict
// here. It requires consecutive failed attempts on one
// account to be limited, with a ceiling of 100. It also asks that the limit not
// lock the real subscriber out, and every mechanism it offers for that is a
// signal about who is asking: an address allowlist, a CAPTCHA, risk-based
// authentication.
//
// VerifyCredentials receives a username and a password and nothing else, so it
// has none of those signals. A budget keyed by the account id cannot tell the
// owner from whoever is guessing at them, so refusing on a spent budget refuses
// the owner too, and spending that budget takes ten requests. That is a denial
// of service anybody can aim at a named account.
//
// So this is a record of failures and not a gate on admission. A correct
// password is admitted whatever the record holds, and clears it, as section
// 5.2.2 asks on a successful authentication. The cost of a guess bounds
// guessing: one argon2id pass, and no more than the configured number of them
// at once. A limit that counts attempts belongs in a transport, where the
// client is known. internal/transport/http/loginguard holds a login for as
// long as the attempts charged to its account and its address say.
const (
	// MaxFailures is how many failures inside the window one account's record
	// holds. It stays under section 5.2.2's ceiling, so a limit enforced over
	// this record could stay under it too.
	MaxFailures = 10
	// FailureWindow is how long a failure stays in the record.
	FailureWindow = 15 * time.Minute
)

// DefaultHashesAtOnce bounds how many argon2id passes run at once, when
// WithHashesAtOnce sets no other bound. The printer build runs one.
//
// One pass holds a large block of memory, and the caller presenting a password
// has no credential. Without a bound, anything that can open sockets can reach
// all the memory of a small board. The bound is what such a board can spend
// without slowing a running print. A login past the bound waits within its own
// request deadline and is never refused for the wait alone, so an attacker
// cannot make logins fail by filling the bound.
const DefaultHashesAtOnce = buildprofile.PasswordHashesAtOnce

// MaxWriteWait is how long a call that writes a password waits for one of those
// slots before it gives up.
//
// A login queues with no deadline of its own, because refusing one would let a
// caller use the memory bound to stop people logging in. A call that writes a
// password is not on that path: setup, adding somebody, a change. It queues
// with a deadline, so a flood of setup calls from a caller with no credential
// is refused after the wait and does not queue without end.
const MaxWriteWait = 10 * time.Second

// throttle is the failure record, keyed by account id.
//
// It holds nothing for a username no account answers to. An unknown username is
// charged the same argon2id pass and refused whatever its history, so tracking
// one would add no protection and would let a caller grow this map by sending
// names at random.
type throttle struct {
	mu     sync.Mutex
	failed map[string][]time.Time
}

func newThrottle() *throttle { return &throttle{failed: make(map[string][]time.Time)} }

// record charges one failure against the account.
func (t *throttle) record(id string, now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()

	kept := t.prune(id, now)
	if len(kept) >= MaxFailures {
		// The record is full. Appending would push the window's end forward on
		// every further failure, so somebody who keeps guessing would hold one
		// entry per account for as long as they keep going, and this map would
		// grow with the number of accounts guessed at.
		return
	}

	t.failed[id] = append(kept, now)
}

// clear forgets an account's failures, which a correct password does.
func (t *throttle) clear(id string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	delete(t.failed, id)
}

// prune drops the failures that have aged out and returns what is left. The
// caller holds the lock.
func (t *throttle) prune(id string, now time.Time) []time.Time {
	held := t.failed[id]

	cut := now.Add(-FailureWindow)
	kept := held[:0]

	for _, at := range held {
		if at.After(cut) {
			kept = append(kept, at)
		}
	}

	if len(kept) == 0 {
		delete(t.failed, id)

		return nil
	}

	t.failed[id] = kept

	return kept
}

// gate bounds concurrent password verifications. A caller waits on the context
// it arrived with, so a client that gave up frees its place instead of holding
// one.
type gate chan struct{}

func newGate(size int) gate { return make(gate, size) }

// enter takes a place, or reports the caller's own context ending first.
//
// The cancellation is read before the select and not only inside it. A select
// whose cases are both ready picks at random, so a caller that had already
// given up would be admitted about half the time on an idle server.
func (g gate) enter(ctx context.Context) (release func(), err error) {
	if err = ctx.Err(); err != nil {
		return nil, fmt.Errorf("wait for a password verification slot: %w", err)
	}

	select {
	case g <- struct{}{}:
		return func() { <-g }, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("wait for a password verification slot: %w", ctx.Err())
	}
}
