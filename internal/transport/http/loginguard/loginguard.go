// Package loginguard holds a caller in front of a credential check for as long
// as the failures already charged to their account and to their address say.
//
// It is the limit NIST SP 800-63B section 5.2.2 asks for, in the one form that
// section offers for keeping a limit off the legitimate subscriber: a wait that
// grows as an account approaches its allowance. The account record only ever
// holds, so a correct password is admitted whatever that record holds, which is
// the property internal/platform/auth/local exists to keep and cannot keep on
// its own. That package is handed a username and a password and nothing else;
// this one is handed the request, so it is where the client is known.
//
// The address record also queues. Attempts from one address are let through one
// after another, each after the hold the one before it earned, so a client that
// opens many connections at once gets no more guesses than one that waits for
// each answer. An attempt whose turn is further off than the ceiling is refused
// with the time its turn comes. The account record does not queue: a queue
// there is one that guessers on many addresses fill, and its owner is then
// refused too.
package loginguard

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/bits"
	"net"
	"net/http"
	"net/netip"
	"slices"
	"strings"
	"sync"
	"time"
)

// The numbers a Guard runs on when the matching Options field is zero.
const (
	// DefaultAccountFree is how many attempts one account absorbs before an
	// attempt on it is held at all. Three covers a person mistyping a password
	// they know.
	DefaultAccountFree = 3
	// DefaultAddressFree is the same allowance for one client address. It is
	// larger because a NAT is one address for a whole workshop, and a password
	// somebody there got wrong is not evidence about anybody else.
	DefaultAddressFree = 12
	// DefaultBase is the hold the first attempt past the allowance earns. It
	// doubles with each further attempt.
	DefaultBase = 250 * time.Millisecond
	// DefaultCeiling is the longest any attempt is held. It bounds what
	// somebody guessing at a named account can cost that account's owner, and
	// it is far under the write timeout, so a held login is no more expensive
	// than a slow reader on any other route.
	DefaultCeiling = 5 * time.Second
	// DefaultWindow is how long a key's record survives with nothing charged to
	// it. A person who got a password wrong this morning starts clean.
	DefaultWindow = 15 * time.Minute
	// DefaultMaxKeys bounds each of the two tables. A caller chooses the
	// username they send and, behind a declared proxy, the address as well, so
	// what is held has to be bounded by something they do not choose.
	DefaultMaxKeys = 4096
)

// maxKeyBytes is the longest key either table stores. The login body already
// bounds a username at 254 bytes and an address is shorter than that; this
// holds the bound whatever calls the Guard, so the memory one entry costs is a
// number rather than a hope.
const maxKeyBytes = 254

// ipv6RoutingPrefix is how much of an IPv6 client is keyed. A single host is
// routinely given a /64, so keying by the whole address means one guesser holds
// every key they like for nothing. /64 is the smallest allocation a site is
// expected to hand out (RFC 4291 section 2.5.1), which makes it the address.
const ipv6RoutingPrefix = 64

// doubling is the factor one further attempt multiplies the hold by.
const doubling = 2

// overflowKey is the one key every new key is counted under while its table is
// full. No username or address folds to it, because neither holds a NUL.
const overflowKey = "\x00overflow"

// forwardedFor is the header a reverse proxy writes the client address into.
// It is read only from a peer the operator declared, because every hop before
// that peer wrote whatever it liked into it.
const forwardedFor = "X-Forwarded-For"

// Options is how a Guard is built. Every field has a working zero, so
// New(Options{}) is the shipping configuration.
type Options struct {
	// TrustedProxies are the networks a reverse proxy reaches this server from.
	// Empty trusts nothing and reads the peer's own address, which is what a
	// server on its own address wants and what a server behind a proxy must not
	// be left on: every client would then share one key.
	TrustedProxies []netip.Prefix
	// AccountFree is how many attempts one account absorbs before it is held.
	// Zero takes DefaultAccountFree; a negative number gives no allowance.
	AccountFree int
	// AddressFree is the same for one client address.
	AddressFree int
	// Base is the hold the first attempt past the allowance earns.
	Base time.Duration
	// Ceiling is the longest any attempt is held.
	Ceiling time.Duration
	// Window is how long a key's record survives with nothing charged to it.
	Window time.Duration
	// MaxKeys bounds each table.
	MaxKeys int
	// Now is the clock. Nil is time.Now.
	Now func() time.Time
	// Sleep is how a hold is spent. Nil waits on a timer and on the caller's
	// own context, so a client that hangs up stops holding anything.
	Sleep func(ctx context.Context, d time.Duration) error
	// Logger records every attempt that is held or refused. Nil discards.
	Logger *slog.Logger
}

// BusyError refuses an attempt from an address whose queue already runs past
// the ceiling. It is not a verdict on a password: the attempt never reached the
// check.
type BusyError struct {
	// RetryAfter is how long until an attempt from this address is let
	// through again.
	RetryAfter time.Duration
	// Refused is how many attempts from this address have been refused since
	// its record was last cleared, this one included.
	Refused int
}

func (e *BusyError) Error() string {
	return fmt.Sprintf("login attempts from this address are queued, retry after %s, %d refused",
		e.RetryAfter, e.Refused)
}

// counter is one key's record: how many attempts have been charged to it, when
// the last of them arrived, when its queue empties, and how many were refused.
type counter struct {
	charged int
	last    time.Time
	next    time.Time
	refused int
}

// table is one keyspace and the allowance its keys get. It is bounded by the
// Guard's MaxKeys, and it is never evicted from: an eviction is how a flood
// erases the record of the account it is guessing at.
type table struct {
	entries map[string]counter
	free    int
	queued  bool
}

// Guard is the hold in front of a credential check. The zero value is not
// usable; New wires the tables and the clock.
type Guard struct {
	mu        sync.Mutex
	accounts  table
	addresses table
	swept     time.Time

	trusted []netip.Prefix
	base    time.Duration
	ceiling time.Duration
	window  time.Duration
	maxKeys int
	now     func() time.Time
	sleep   func(context.Context, time.Duration) error
	log     *slog.Logger
}

// New returns a Guard on the shipping numbers, with anything Options names
// taking their place.
func New(opts Options) *Guard {
	guard := &Guard{
		accounts: table{entries: map[string]counter{}, free: allowance(opts.AccountFree, DefaultAccountFree)},
		addresses: table{
			entries: map[string]counter{},
			free:    allowance(opts.AddressFree, DefaultAddressFree),
			queued:  true,
		},
		trusted: opts.TrustedProxies,
		base:    span(opts.Base, DefaultBase),
		ceiling: span(opts.Ceiling, DefaultCeiling),
		window:  span(opts.Window, DefaultWindow),
		maxKeys: allowance(opts.MaxKeys, DefaultMaxKeys),
		now:     opts.Now,
		sleep:   opts.Sleep,
		log:     opts.Logger,
	}

	if guard.now == nil {
		guard.now = time.Now
	}

	if guard.sleep == nil {
		guard.sleep = spend
	}

	if guard.log == nil {
		guard.log = slog.New(slog.DiscardHandler)
	}

	return guard
}

// Hold charges one attempt to the account and to the address, and returns when
// the wait the two of them have earned is over.
//
// The attempt is charged before the password is checked rather than after it
// fails, so somebody sending a thousand guesses at once is held by the
// thousandth charge rather than by the first failure to come back. A correct
// password clears both records through Passed, which is what section 5.2.2 asks
// for on a successful authentication.
//
// It refuses an attempt only when the address already has attempts queued past
// the ceiling, with a *BusyError that says when to come back. The account is
// never the reason, so somebody guessing at an account from elsewhere cannot
// keep its owner out. The only other error is the caller's own context ending,
// which is a client that hung up rather than a verdict on a password.
func (g *Guard) Hold(ctx context.Context, address, account string) error {
	held, err := g.charge(address, account)
	if err != nil {
		// A refusal costs its sender nothing, so logging each one lets one
		// address write to the log as fast as it can send. The first, second,
		// fourth and every doubling after are logged with the running count,
		// which records every refusal in a number of lines that grows with the
		// logarithm of the flood.
		if busy, ok := errors.AsType[*BusyError](err); !ok || bits.OnesCount(uint(busy.Refused)) == 1 {
			g.log.WarnContext(ctx, "login attempt refused",
				"account", AccountKey(account), "address", address, "error", err)
		}

		return err
	}

	if held <= 0 {
		return nil
	}

	g.log.WarnContext(ctx, "login attempt held",
		"account", AccountKey(account), "address", address, "hold", held)

	return g.sleep(ctx, held)
}

// Passed forgets what is charged to an account, and forgives an address down to
// its allowance, which a correct password does.
//
// The address is forgiven so a workshop behind one address stays usable while
// somebody on it is guessing: one person getting in there brings everybody
// else back to the shortest hold. It is not forgotten. A guesser who holds an
// account of their own would log in to it between guesses, and a cleared
// record hands them a fresh allowance and an empty queue each time. So the
// count drops no lower than the allowance, which hands out no free attempts,
// and the queue stays as it is.
func (g *Guard) Passed(address, account string) {
	g.mu.Lock()
	defer g.mu.Unlock()

	delete(g.accounts.entries, AccountKey(account))

	key := addressKey(address)
	if held, known := g.addresses.entries[key]; known {
		held.charged = min(held.charged, g.addresses.free)
		g.addresses.entries[key] = held
	}
}

// Address is the client a request came from, as this deployment's trust settles
// it.
//
// RemoteAddr is the only address this server observes for itself, so it is the
// answer unless the connection arrived from a network the operator declared as
// a proxy. X-Forwarded-For is written by the hop that spoke last and carries
// whatever every hop before it wrote, so from an undeclared peer it is a key
// the caller chooses and is read as nothing at all. From a declared one, the
// address is the rightmost entry that is not itself declared: the entries to
// the left of it were written by somebody this deployment has not vouched for.
func (g *Guard) Address(r *http.Request) string {
	if r == nil {
		return ""
	}

	peer, readable := hostOf(r.RemoteAddr)
	if !readable {
		// A peer this cannot read as an address is a unix socket, or a listener
		// nothing here has seen. It is keyed as it arrived, which keys one such
		// peer and not all of them together.
		return r.RemoteAddr
	}

	if !within(g.trusted, peer) {
		return peer.String()
	}

	for _, hop := range forwardedHops(r) {
		if !within(g.trusted, hop) {
			return hop.String()
		}
	}

	return peer.String()
}

// charge takes one attempt off both allowances and reports the longer of the
// two holds they have earned. The address is charged first, so an attempt it
// refuses costs the account nothing: it never reaches the password check.
func (g *Guard) charge(address, account string) (time.Duration, error) {
	now := g.now()

	g.mu.Lock()
	defer g.mu.Unlock()

	g.sweep(now)

	fromAddress, err := g.tally(&g.addresses, addressKey(address), now)
	if err != nil {
		return 0, err
	}

	onAccount, err := g.tally(&g.accounts, AccountKey(account), now)
	if err != nil {
		return 0, err
	}

	return max(onAccount, fromAddress), nil
}

// tally charges one attempt to a key and reports the hold it has earned. On a
// queued table that hold starts when the attempts already waiting on the key
// are through, and an attempt it would put past the ceiling is refused and not
// charged. The overflow record never queues, on either table. The caller holds
// the lock.
func (g *Guard) tally(t *table, key string, now time.Time) (time.Duration, error) {
	if _, known := t.entries[key]; !known && len(t.entries) >= g.maxKeys {
		// Every key in this table is still inside the window, so nothing can be
		// dropped to make room. A new key is not admitted: a caller varies the
		// username they send, and behind a declared proxy the address as well,
		// so admitting one per attempt would put this table's size in their
		// hands. Every new key shares the one overflow record instead, so a
		// caller who fills the table is still held. That record holds and
		// never refuses: it stands for every client the table could not
		// record, the owner among them, and a queue there is one a caller who
		// filled the table keeps full to keep all of them out. What is already
		// recorded stays recorded, and the entries age out on their own.
		key = overflowKey
	}

	held, known := t.entries[key]
	if known && now.Sub(held.last) >= g.window {
		held = counter{}
	}

	before := held
	held.charged++
	held.last = now
	wait := g.hold(held.charged - t.free)

	if t.queued && key != overflowKey {
		wait += max(held.next.Sub(now), 0)
		if wait > g.ceiling {
			before.refused++
			t.entries[key] = before

			return 0, &BusyError{RetryAfter: wait - g.ceiling, Refused: before.refused}
		}

		held.next = now.Add(wait)
	}

	t.entries[key] = held

	return wait, nil
}

// hold is the wait a key has earned, from how many attempts it has spent past
// its allowance. It doubles per attempt and stops at the ceiling.
func (g *Guard) hold(over int) time.Duration {
	if over <= 0 {
		return 0
	}

	held := g.base
	for range over - 1 {
		if held >= g.ceiling/doubling {
			return g.ceiling
		}

		held *= doubling
	}

	return min(held, g.ceiling)
}

// sweep drops the keys nothing has been charged to for a window, at most once
// per window. The caller holds the lock.
//
// It runs from charge, which every attempt passes through, so a table does not
// wait for a failure to be tidied. Without it a table that filled during an
// attack stays full for as long as the process runs.
func (g *Guard) sweep(now time.Time) {
	if now.Sub(g.swept) < g.window {
		return
	}

	g.swept = now

	for _, t := range []*table{&g.accounts, &g.addresses} {
		for key, held := range t.entries {
			if now.Sub(held.last) >= g.window {
				delete(t.entries, key)
			}
		}
	}
}

// AccountKey folds a username the way the account store does, so one account
// reached under two spellings is one key. A caller that logs a username writes
// this, so the guard's records and its own name one account the same way.
func AccountKey(username string) string {
	return clamp(strings.ToLower(strings.TrimSpace(username)))
}

// addressKey is the key one client address is counted under.
//
// An IPv6 client is keyed by its /64 rather than by its address, because a
// single host is routinely given a /64 and keying by the address means one
// guesser holds as many keys as they care to use. An address this cannot parse
// is keyed as it arrived, which is what a unix socket peer is.
func addressKey(address string) string {
	parsed, err := netip.ParseAddr(address)
	if err != nil {
		return clamp(address)
	}

	parsed = parsed.Unmap().WithZone("")

	if parsed.Is6() {
		if network, maskErr := parsed.Prefix(ipv6RoutingPrefix); maskErr == nil {
			return network.String()
		}
	}

	return parsed.String()
}

// hostOf is the address half of a RemoteAddr, which net/http writes as host and
// port, and whether it read as one at all.
func hostOf(remote string) (netip.Addr, bool) {
	if parsed, err := netip.ParseAddrPort(remote); err == nil {
		return parsed.Addr().Unmap().WithZone(""), true
	}

	if host, _, err := net.SplitHostPort(remote); err == nil {
		if parsed, parseErr := netip.ParseAddr(host); parseErr == nil {
			return parsed.Unmap().WithZone(""), true
		}
	}

	parsed, err := netip.ParseAddr(remote)
	if err != nil {
		return netip.Addr{}, false
	}

	return parsed.Unmap().WithZone(""), true
}

// forwardedHops is every address the forwarding header carries, rightmost
// first. Repeated field lines are one list, which RFC 9110 section 5.3 says
// they are equal to, so they are walked from the last line back.
func forwardedHops(r *http.Request) []netip.Addr {
	lines := r.Header.Values(forwardedFor)

	out := make([]netip.Addr, 0, len(lines))

	for _, line := range slices.Backward(lines) {
		hops := strings.Split(line, ",")
		for _, hop := range slices.Backward(hops) {
			hop, err := netip.ParseAddr(strings.TrimSpace(hop))
			if err != nil {
				continue
			}

			out = append(out, hop.Unmap().WithZone(""))
		}
	}

	return out
}

// within reports whether an address sits in one of the declared networks.
func within(trusted []netip.Prefix, addr netip.Addr) bool {
	if !addr.IsValid() {
		return false
	}

	for _, network := range trusted {
		if network.Contains(addr) {
			return true
		}
	}

	return false
}

// clamp bounds a key, so one entry's cost is a number and not whatever a caller
// sent.
func clamp(key string) string {
	if len(key) <= maxKeyBytes {
		return key
	}

	return key[:maxKeyBytes]
}

// allowance takes the configured count, the default for zero, and none for a
// negative number.
func allowance(configured, fallback int) int {
	switch {
	case configured == 0:
		return fallback
	case configured < 0:
		return 0
	default:
		return configured
	}
}

// span takes the configured duration, and the default for anything that is not
// a positive one.
func span(configured, fallback time.Duration) time.Duration {
	if configured <= 0 {
		return fallback
	}

	return configured
}

// spend is the default hold: a timer and the caller's own context, so a client
// that hangs up frees the connection rather than holding it to the ceiling.
func spend(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("hold a login attempt: %w", ctx.Err())
	}
}
