package local_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/platform/auth"
	"github.com/prusa3d/prusa-connect/internal/platform/auth/local"
	"github.com/prusa3d/prusa-connect/internal/platform/auth/session"
	"github.com/prusa3d/prusa-connect/sdk/authapi"
)

const victimName = "victim@example.com"

const victimPassword = "the victim's own password"

// gateStore is a local.Store that lets a test stop a login exactly where the
// login path holds its widest window: after the account row has been read and
// before the argon2id verification has run.
//
// It has two seams. ByID blocks for one chosen id once a test arms it, which is
// how a test fills every verification slot: ChangePassword reads the row it is
// about to check under the slot it took, so a read that never returns is a slot
// that is never given back. It is armed and not always on, because the fixture
// creates its accounts through the same read. ByUsername announces the read,
// which is how a test knows a login has taken its snapshot of an account and is
// waiting for a slot.
type gateStore struct {
	*local.MemoryStore

	holdID  string
	armed   atomic.Bool
	arrived chan string
	release chan struct{}

	watchUser string
	looked    chan string
}

func newGateStore(holdID, watchUser string) *gateStore {
	return &gateStore{
		MemoryStore: local.NewMemoryStore(),
		holdID:      holdID,
		arrived:     make(chan string, local.DefaultHashesAtOnce),
		release:     make(chan struct{}),
		watchUser:   watchUser,
		looked:      make(chan string, lookedBuffer),
	}
}

// lookedBuffer keeps ByUsername from blocking on a login no test is watching.
const lookedBuffer = 16

func (g *gateStore) ByUsername(ctx context.Context, username string) (local.Account, error) {
	row, err := g.MemoryStore.ByUsername(ctx, username)

	if username == g.watchUser {
		g.looked <- username
	}

	return row, err
}

func (g *gateStore) ByID(ctx context.Context, id string) (local.Account, error) {
	if id == g.holdID && g.armed.Load() {
		g.arrived <- id
		<-g.release
	}

	return g.MemoryStore.ByID(ctx, id)
}

// fillTheVerificationSlots takes every argon2id slot and holds them until the
// returned function is called.
//
// This is what widens the window. VerifyCredentials reads the account row
// before it asks for a slot, so a caller that cannot get one sits on a snapshot
// of an account for as long as the slots stay busy, and whoever is using them
// decides how long that is.
func fillTheVerificationSlots(t *testing.T, accounts *local.Accounts, store *gateStore) func() {
	t.Helper()

	var holders sync.WaitGroup

	store.armed.Store(true)

	for range local.DefaultHashesAtOnce {
		holders.Go(func() {
			_ = accounts.ChangePassword(
				context.WithoutCancel(t.Context()), store.holdID,
				[]byte(goodPassword), []byte("a replacement password"))
		})
	}

	for range local.DefaultHashesAtOnce {
		select {
		case <-store.arrived:
		case <-time.After(30 * time.Second):
			t.Fatal("a verification slot was never taken")
		}
	}

	return func() {
		store.armed.Store(false)
		close(store.release)
		holders.Wait()
	}
}

// racedLogin starts a login and stops it in the window: the account has been
// read and no slot is free, so nothing has been verified yet.
func racedLogin(
	t *testing.T, passwords *session.Passwords, store *gateStore, username, password string,
) <-chan loginResult {
	t.Helper()

	// Whatever earlier logins the fixture ran is not what this one is waiting on.
	for len(store.looked) > 0 {
		<-store.looked
	}

	done := make(chan loginResult, 1)

	go func() {
		result, err := passwords.Login(context.WithoutCancel(t.Context()), username, password)
		done <- loginResult{result: result, err: err}
	}()

	select {
	case <-store.looked:
	case <-time.After(30 * time.Second):
		t.Fatal("the login never read the account")
	}

	return done
}

type loginResult struct {
	result session.Result
	err    error
}

func awaitLogin(t *testing.T, done <-chan loginResult) loginResult {
	t.Helper()

	select {
	case got := <-done:
		return got
	case <-time.After(30 * time.Second):
		t.Fatal("the login never finished")
	}

	return loginResult{}
}

// wireVictim builds a store holding an administrator and one other person, and
// the credential login over it.
func wireVictim(t *testing.T) (*local.Accounts, *gateStore, *session.Passwords, *auth.Verifier, local.Account) {
	t.Helper()

	store := newGateStore(firstID, victimName)
	accounts := local.New(store, nil)

	_, err := accounts.CreateFirst(t.Context(), firstID, local.NewAccount{
		Username: "owner@example.com",
		Password: []byte(goodPassword),
	})
	require.NoError(t, err)

	victim, err := accounts.Create(t.Context(), local.NewAccount{
		Username: victimName,
		Password: []byte(victimPassword),
	})
	require.NoError(t, err)

	credentials := auth.NewMemoryStore()

	return accounts, store, session.NewPasswords(accounts, credentials, nil),
		auth.NewVerifier(credentials, nil), victim
}

// An administrator who removes somebody expects that person off this server.
//
// The login path reads the account row before it asks for an argon2id slot, so
// a login that arrived first sits on a snapshot of a live account for as long
// as the slots are busy. When a slot comes free, the row the snapshot names is
// gone, and the login must not mint.
//
// Nothing calls RefreshIdentity on a request, so a session minted here would
// never be re-checked against the account store. It would be a working
// credential for a person the administrator removed, for the whole session
// lifetime.
func TestALoginThatStartedBeforeADeleteMintsNothing(t *testing.T) {
	t.Parallel()

	accounts, store, passwords, verifier, victim := wireVictim(t)

	freeTheSlots := fillTheVerificationSlots(t, accounts, store)
	done := racedLogin(t, passwords, store, victimName, victimPassword)

	// The administrator removes the account and ends its sessions, which is what
	// internal/app's Delete does in that order.
	require.NoError(t, accounts.Delete(t.Context(), victim.ID))

	_, err := verifier.RevokeSubject(t.Context(), victim.ID)
	require.NoError(t, err)

	_, err = accounts.Get(t.Context(), victim.ID)
	require.ErrorIs(t, err, local.ErrNoAccount, "the account was not removed")

	freeTheSlots()

	got := awaitLogin(t, done)

	if got.err == nil {
		cred, verifyErr := verifier.Verify(t.Context(), got.result.Token, auth.TransportHTTP)
		if verifyErr == nil {
			t.Errorf("the removed account holds a working session for subject %q until %s",
				cred.Subject, cred.ExpiresAt)
		}
	}

	require.ErrorIs(t, got.err, authapi.ErrInvalidCredentials,
		"a login that started before the delete still authenticated the removed account")
}

// TestALoginThatStartedBeforeAPasswordChangeMintsNothing is the other half of
// the same window, against the claim that changing a password revokes that
// subject's sessions.
//
// A change revokes the sessions that exist when it runs. It could not revoke
// one minted a moment later from the password it replaced. The old hash is read
// before the login queues for a slot, so the window is as wide as the queue.
func TestALoginThatStartedBeforeAPasswordChangeMintsNothing(t *testing.T) {
	t.Parallel()

	accounts, store, passwords, verifier, victim := wireVictim(t)

	// The session the victim already holds. The reset must end this one, and it
	// does; the point is the one that arrives after it.
	before, err := passwords.Login(t.Context(), victimName, victimPassword)
	require.NoError(t, err)

	freeTheSlots := fillTheVerificationSlots(t, accounts, store)
	done := racedLogin(t, passwords, store, victimName, victimPassword)

	// The administrator resets the password and ends the sessions, which is what
	// internal/app's ResetPassword does in that order.
	require.NoError(t, accounts.SetPassword(t.Context(), victim.ID, []byte("a password nobody guessed")))

	_, err = verifier.RevokeSubject(t.Context(), victim.ID)
	require.NoError(t, err)

	_, err = verifier.Verify(t.Context(), before.Token, auth.TransportHTTP)
	require.Error(t, err, "the reset did not end the session that already existed")

	freeTheSlots()

	got := awaitLogin(t, done)

	if got.err == nil {
		cred, verifyErr := verifier.Verify(t.Context(), got.result.Token, auth.TransportHTTP)
		if verifyErr == nil {
			t.Errorf("the replaced password minted a working session for subject %q until %s, "+
				"which the reset could not have revoked", cred.Subject, cred.ExpiresAt)
		}
	}

	require.ErrorIs(t, got.err, authapi.ErrInvalidCredentials,
		"the replaced password still opened a session after the reset had revoked everything")
}

// Every path that derives a hash takes a verification slot, not only the login.
//
// DefaultHashesAtOnce exists because one argon2id pass holds a large
// block of memory and the caller presenting a password has no credential. POST
// /auth/setup is such a caller: it takes no credential and runs a full pass, as
// Create does. With every slot held, none of them may hash.
func TestNothingHashesOutsideTheVerificationBound(t *testing.T) {
	t.Parallel()

	const askers = 16

	accounts, store, _, _, _ := wireVictim(t)

	freeTheSlots := fillTheVerificationSlots(t, accounts, store)
	defer freeTheSlots()

	var (
		hashed sync.WaitGroup
		passed atomic.Int64
	)

	for i := range askers {
		hashed.Go(func() {
			if _, err := accounts.Create(context.WithoutCancel(t.Context()), local.NewAccount{
				Username: "added-" + string(rune('a'+i)) + "@example.com",
				Password: []byte("a password for the added person"),
			}); err == nil {
				passed.Add(1)
			}
		})
	}

	settled := make(chan struct{})
	go func() { hashed.Wait(); close(settled) }()

	select {
	case <-settled:
	case <-time.After(30 * time.Second):
		t.Fatal("the creates never finished")
	}

	require.Zero(t, passed.Load(),
		"%d argon2id hashes ran with every one of the %d verification slots held",
		passed.Load(), local.DefaultHashesAtOnce)
}

// The same rule as it is reached from the network.
//
// A server that has not been set up answers POST /auth/setup with no credential
// at all, and each of those calls derives a hash before the id collision
// refuses it. The bound is what keeps the passes of such a flood, and the memory
// they hold, inside what the slots allow.
func TestAnUnauthenticatedSetupFloodStaysInsideTheBound(t *testing.T) {
	const askers = 16

	accounts, _ := newAccounts(t)

	needed, err := accounts.NeedsSetup(t.Context())
	require.NoError(t, err)
	require.True(t, needed)

	high := peakDerivationsDuring(t, askers, func() {
		// Every one of these is a request with no credential on a server that
		// has not been set up yet.
		_, _ = accounts.CreateFirst(context.WithoutCancel(t.Context()), firstID, local.NewAccount{
			Username: "owner@example.com",
			Password: []byte(goodPassword),
		})
	})

	// Setup is still closed against itself, which is the claim that holds.
	held, err := accounts.List(t.Context())
	require.NoError(t, err)
	require.Len(t, held, 1, "concurrent setup wrote more than one administrator")

	require.LessOrEqual(t, high, local.DefaultHashesAtOnce,
		"%d unauthenticated setup calls ran %d argon2id passes at once; the bound allows %d",
		askers, high, local.DefaultHashesAtOnce)
}
