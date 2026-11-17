package local

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"runtime/debug"
	"time"

	"github.com/prusa3d/prusa-connect/internal/platform/auth"
	"github.com/prusa3d/prusa-connect/sdk/authapi"
)

// Type is the provider name an operator sees, and the one sdk/authapi's
// Identity is scoped by.
const Type = "local"

// The refusals this package gives that are not a store's.
var (
	// ErrInvalidAccount reports a username, display name or password this store
	// will not take.
	ErrInvalidAccount = errors.New("local: the account details are not usable")
	// ErrSetupClosed reports a first-administrator call against a store that
	// already holds one. A caller who lost a race and a caller who arrived a
	// week late get the same answer.
	ErrSetupClosed = errors.New("local: the first administrator already exists")
)

// Accounts is the local credential store, and the sdk/authapi provider over it.
//
// The zero value is not usable. New wires the hasher, the failure budget and
// the verification bound, and none of them has a meaningful zero.
type Accounts struct {
	store    Store
	now      func() time.Time
	hasher   *auth.Hasher
	attempts *throttle
	slots    gate
	memory   *memoryReturner
}

// Option changes one setting of the store New returns.
type Option func(*Accounts)

// WithHasher makes and checks passwords with the given hasher. The dummy hash
// an unknown username is checked against comes from the same hasher, so both
// paths cost one pass at the same cost. A nil hasher keeps the default.
func WithHasher(hasher *auth.Hasher) Option {
	return func(a *Accounts) {
		if hasher != nil {
			a.hasher = hasher
		}
	}
}

// WithHashesAtOnce bounds how many password hashes run at the same time. A
// login past the bound waits for a slot within its own deadline. A bound below
// one would stop every login, so it keeps DefaultHashesAtOnce.
func WithHashesAtOnce(bound int) Option {
	return func(a *Accounts) {
		if bound >= 1 {
			a.slots = newGate(bound)
		}
	}
}

var _ authapi.IdentityProvider = (*Accounts)(nil)

// New returns a credential store over an account store. A nil clock means
// time.Now. Without options it hashes at auth.DefaultCost.
func New(store Store, now func() time.Time, options ...Option) *Accounts {
	if now == nil {
		now = time.Now
	}

	accounts := &Accounts{
		store:    store,
		now:      now,
		hasher:   auth.NewDefaultHasher(),
		attempts: newThrottle(),
		slots:    newGate(DefaultHashesAtOnce),
		memory:   newMemoryReturner(ReturnMemoryEvery, debug.FreeOSMemory),
	}

	for _, option := range options {
		option(accounts)
	}

	return accounts
}

// HashesAtOnce is how many password hashes this store runs at the same time.
func (a *Accounts) HashesAtOnce() int { return cap(a.slots) }

// NewAccount is what a caller supplies to create an account. The password is a
// byte slice because that is what the hasher takes, and because a string would
// stay in memory for the life of the process.
type NewAccount struct {
	Username    string
	DisplayName string
	Password    []byte
}

// Name is the provider type an operator sees.
func (a *Accounts) Name() string { return Type }

// Capabilities reports the credential flow and revalidation.
//
// Revalidation is offered because this provider can answer it. The account
// either still exists or does not, and the store is local, so no issuer can be
// unreachable. Redirect is not offered, because there is nowhere to send a
// browser.
func (a *Accounts) Capabilities() authapi.Capabilities {
	return authapi.Capabilities{Credentials: true, Revalidation: true}
}

// VerifyCredentials authenticates a username and password.
//
// Every refusal is authapi.ErrInvalidCredentials, and every refusal costs one
// argon2id pass. An unknown username, a wrong password, an account whose
// failure budget is gone and a store that could not answer are indistinguishable
// from outside, in the answer and in the time it takes to give it.
//
// The password is checked before the failure record is touched. A record keyed
// by the account id cannot tell the owner from somebody guessing. A refusal
// taken before the check would refuse the owner's correct password because
// somebody else sent wrong ones. The record refuses nothing. throttle.go says
// what it is for and what bounds guessing.
func (a *Accounts) VerifyCredentials(ctx context.Context, username, password string) (authapi.Identity, error) {
	identity, _, err := a.VerifyCredentialsAtGeneration(ctx, username, password)

	return identity, err
}

// VerifyCredentialsAtGeneration is VerifyCredentials, and it also returns the
// generation of the account's credentials, read in the same statement as the
// hash it checked. A session minted at that generation is refused if the
// password changed after the read, even if the mint lands later.
func (a *Accounts) VerifyCredentialsAtGeneration(
	ctx context.Context, username, password string,
) (authapi.Identity, int64, error) {
	folded := FoldUsername(username)
	secret := []byte(password)

	account, lookupErr := a.store.ByUsername(ctx, folded)

	release, err := a.enter(ctx)
	if err != nil {
		return authapi.Identity{}, 0, err
	}
	defer release()

	// The row above was read before the wait for a slot, and whoever holds the
	// slots decides how long that wait is. A caller with no credential can fill
	// them and leave a login sitting on a stale snapshot. So the row is read
	// again under the slot, by the id the username resolved to. An account
	// deleted or a password replaced while this login queued is refused, and
	// never checked against the row as it stood when the wait began.
	//
	// The credential is minted a layer up, in session.Passwords, so a change
	// can still land between the derivation below and that mint. The session
	// carries the generation read here, and the change raised it, so the
	// verifier refuses that session.
	if lookupErr == nil {
		account, lookupErr = a.store.ByID(ctx, account.ID)
	}

	now := a.now()

	// A username nobody holds still runs VerifyUnknownUser, which charges the
	// same derivation a real check costs, so a caller cannot read which branch
	// was taken.
	if lookupErr != nil {
		_ = a.hasher.VerifyUnknownUser(secret)

		return authapi.Identity{}, 0, authapi.ErrInvalidCredentials
	}

	// One derivation, whatever the record holds. A wrong password is charged
	// against the record here and refused. A correct one is admitted below and
	// empties the record. Both cost the same pass, so a caller cannot read how
	// much of an account's record is spent.
	if verifyErr := auth.VerifyPassword(account.Hash, secret); verifyErr != nil {
		a.attempts.record(account.ID, now)

		return authapi.Identity{}, 0, authapi.ErrInvalidCredentials
	}

	a.attempts.clear(account.ID)
	a.rehashAtOtherCost(ctx, account, secret, now)

	return identityOf(account), account.Generation, nil
}

// BeginLogin is unsupported. There is no issuer to send a browser to.
func (a *Accounts) BeginLogin(context.Context, string, string) (string, string, error) {
	return "", "", authapi.ErrUnsupportedFlow
}

// CompleteLogin is unsupported, for the reason BeginLogin gives.
func (a *Accounts) CompleteLogin(context.Context, url.Values, string) (authapi.Identity, error) {
	return authapi.Identity{}, authapi.ErrUnsupportedFlow
}

// RefreshIdentity re-reads an account. An account that has been deleted is
// reported as revoked, which is what ends its holder's live sessions.
func (a *Accounts) RefreshIdentity(ctx context.Context, subject string) (authapi.Identity, error) {
	account, err := a.store.ByID(ctx, subject)
	if errors.Is(err, ErrNoAccount) {
		return authapi.Identity{}, authapi.ErrIdentityRevoked
	}

	if err != nil {
		return authapi.Identity{}, fmt.Errorf("read account %q: %w", subject, err)
	}

	return identityOf(account), nil
}

// NeedsSetup reports whether this store holds no account yet, which is the
// first-run state the login screen offers a setup form in.
func (a *Accounts) NeedsSetup(ctx context.Context) (bool, error) {
	held, err := a.store.Count(ctx)
	if err != nil {
		return false, fmt.Errorf("count accounts: %w", err)
	}

	return held == 0, nil
}

// CreateFirst creates the first administrator, under an id the caller chooses.
//
// The id comes from the caller because it has to be one the authorization graph
// already grants. The graph's first grant is written at startup for the
// configured principal. Giving this account that same id makes the person who
// runs setup an administrator, and no second bootstrap path is needed.
//
// The path closes once an account exists. Two concurrent calls both read an
// empty store and both try to write. The id is the caller's and not minted, so
// the two writes name one row and the store takes one of them. A later caller
// is refused by the count, and a concurrent one by the collision.
func (a *Accounts) CreateFirst(ctx context.Context, id string, spec NewAccount) (Account, error) {
	needed, err := a.NeedsSetup(ctx)
	if err != nil {
		return Account{}, err
	}

	if !needed {
		return Account{}, ErrSetupClosed
	}

	created, err := a.create(ctx, id, spec)
	if errors.Is(err, ErrIDTaken) {
		return Account{}, ErrSetupClosed
	}

	return created, err
}

// ImportFirst writes the first administrator from a password hash made before
// this process started. The first-run wizard hashes the password before the
// server restarts on a new configuration, because the account belongs in the
// database that configuration names, and the password is never written down
// in clear. It is refused like CreateFirst once any account exists.
func (a *Accounts) ImportFirst(ctx context.Context, id, username, hash string) (Account, error) {
	needed, err := a.NeedsSetup(ctx)
	if err != nil {
		return Account{}, err
	}

	if !needed {
		return Account{}, ErrSetupClosed
	}

	folded := FoldUsername(username)
	if err = ValidateUsername(folded); err != nil {
		return Account{}, err
	}

	if hash == "" {
		return Account{}, fmt.Errorf("%w: the account carries no password hash", ErrInvalidAccount)
	}

	now := a.now()
	account := Account{
		ID:          id,
		Username:    folded,
		DisplayName: folded,
		Hash:        hash,
		CreatedAt:   now,
		PasswordAt:  now,
	}

	if err = a.store.Create(ctx, account); err != nil {
		if errors.Is(err, ErrIDTaken) {
			return Account{}, ErrSetupClosed
		}

		return Account{}, err
	}

	account.Hash = ""

	return account, nil
}

// Create adds an account under a minted id.
func (a *Accounts) Create(ctx context.Context, spec NewAccount) (Account, error) {
	id, err := NewID()
	if err != nil {
		return Account{}, err
	}

	return a.create(ctx, id, spec)
}

// List returns every account. The hash is cleared on the way out, so a caller
// that renders the list cannot render a password hash by forgetting to.
func (a *Accounts) List(ctx context.Context) ([]Account, error) {
	rows, err := a.store.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list accounts: %w", err)
	}

	for i := range rows {
		rows[i].Hash = ""
	}

	return rows, nil
}

// Get returns one account without its hash.
func (a *Accounts) Get(ctx context.Context, id string) (Account, error) {
	account, err := a.store.ByID(ctx, id)
	if err != nil {
		return Account{}, fmt.Errorf("read account %q: %w", id, err)
	}

	account.Hash = ""

	return account, nil
}

// Delete removes an account. The store refuses the last one, so the setup path
// cannot be reopened by emptying the store.
func (a *Accounts) Delete(ctx context.Context, id string) error {
	if err := a.store.Delete(ctx, id); err != nil {
		return fmt.Errorf("delete account %q: %w", id, err)
	}

	a.attempts.clear(id)

	return nil
}

// SetPassword replaces an account's password without asking for the current
// one. It is the administrator's reset, for a person who has lost theirs.
//
// It takes no verification slot. The bound exists for a caller with no
// credential: POST /api/v1/auth/setup reaches Create on a server nobody has set
// up yet, and a login reaches VerifyCredentials with nothing but a username.
// This is reached only through the administrator reset, by a caller who already
// holds a session and a grant on the auth resource kind. Queueing it behind the
// logins would let whoever fills the slots block the reset as well as the
// login, and MaxWriteWait would turn that wait into a refusal.
func (a *Accounts) SetPassword(ctx context.Context, id string, password []byte) error {
	hash, err := a.hasher.Hash(password)
	a.memory.request()

	if err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidAccount, err)
	}

	_, err = a.writePassword(ctx, id, "", hash)

	return err
}

// ChangePassword replaces an account's own password after checking the current
// one. The current password is checked through the same refusal a login gives,
// so a caller who guessed at somebody else's session learns nothing from it.
//
// One slot covers both derivations and is given up before the row is written.
// Taking a second slot while holding the first would let concurrent changes
// wait on each other forever. Holding the first across the store write would
// turn a slow disk into a login outage, because a login queues for a slot with
// no deadline and password changes stuck in an fsync would hold every slot.
//
// The write lands only over the hash the current password was checked
// against. Two changes that both knew the old password cannot both land. The
// one that finds the hash already replaced is refused as a wrong current
// password.
func (a *Accounts) ChangePassword(ctx context.Context, id string, current, next []byte) error {
	_, err := a.ChangePasswordAtGeneration(ctx, id, current, next)

	return err
}

// ChangePasswordAtGeneration is ChangePassword, and it also returns the
// generation the change wrote. A session minted for the person after the
// change carries it, so a later change or reset still ends that session.
//
// A write that finds the stored hash changed reads the row again and tries
// once more. A login's rehash of the same password changes the stored hash
// without changing the password, and the current password still verifies
// against it. A real change by somebody else does not verify, and is refused.
func (a *Accounts) ChangePasswordAtGeneration(ctx context.Context, id string, current, next []byte) (int64, error) {
	var err error

	for range 2 {
		var (
			checked, hash string
			generation    int64
		)

		checked, hash, err = a.deriveReplacement(ctx, id, current, next)
		if err != nil {
			return 0, err
		}

		generation, err = a.writePassword(ctx, id, checked, hash)
		if !errors.Is(err, ErrPasswordChanged) {
			return generation, err
		}
	}

	return 0, fmt.Errorf("%w: %w", authapi.ErrInvalidCredentials, err)
}

// deriveReplacement checks the current password and derives the replacement,
// both under one verification slot, and returns holding none. It returns the
// hash it checked the current password against, and the new hash.
func (a *Accounts) deriveReplacement(ctx context.Context, id string, current, next []byte) (string, string, error) {
	release, err := a.enterToDerive(ctx)
	if err != nil {
		return "", "", err
	}
	defer release()

	// Read under the slot, for the reason VerifyCredentials gives: a row read
	// before the wait may have been reset while this call queued.
	account, err := a.store.ByID(ctx, id)
	if err != nil {
		return "", "", fmt.Errorf("read account %q: %w", id, err)
	}

	if verifyErr := auth.VerifyPassword(account.Hash, current); verifyErr != nil {
		return "", "", authapi.ErrInvalidCredentials
	}

	hash, err := a.hasher.Hash(next)
	if err != nil {
		return "", "", fmt.Errorf("%w: %w", ErrInvalidAccount, err)
	}

	return account.Hash, hash, nil
}

// writePassword stores a hash somebody has already derived, ends the account's
// credentials by raising their generation, and forgets the account's failures,
// which a new password makes meaningless.
func (a *Accounts) writePassword(ctx context.Context, id, expected, hash string) (int64, error) {
	generation, err := a.store.ReplacePassword(ctx, id, expected, hash, a.now())
	if err != nil {
		return 0, fmt.Errorf("set the password on %q: %w", id, err)
	}

	a.attempts.clear(id)

	return generation, nil
}

// enterToDerive takes one of the verification slots for a call that is about to
// derive a password hash.
//
// The bound on hashes exists because one pass holds a large block of memory
// and the caller presenting a password has no credential. POST
// /api/v1/auth/setup is such a caller: on a server nobody has set up yet it
// takes no credential and derives a full hash. A bound on the login alone
// would leave the setup path open to the same attack.
func (a *Accounts) enterToDerive(ctx context.Context) (func(), error) {
	waiting, cancel := context.WithTimeout(ctx, MaxWriteWait)
	defer cancel()

	return a.enter(waiting)
}

// enter takes one of the hashing slots. Giving the slot up also asks for the
// memory of the pass to go back to the operating system.
func (a *Accounts) enter(ctx context.Context) (func(), error) {
	release, err := a.slots.enter(ctx)
	if err != nil {
		return nil, err
	}

	return func() {
		release()
		a.memory.request()
	}, nil
}

// create is the one place an account row is written.
func (a *Accounts) create(ctx context.Context, id string, spec NewAccount) (Account, error) {
	folded := FoldUsername(spec.Username)
	if err := ValidateUsername(folded); err != nil {
		return Account{}, err
	}

	display := spec.DisplayName
	if len(display) > MaxDisplayNameBytes {
		return Account{}, fmt.Errorf("%w: the display name is %d bytes, the limit is %d",
			ErrInvalidAccount, len(display), MaxDisplayNameBytes)
	}

	if display == "" {
		display = folded
	}

	release, err := a.enterToDerive(ctx)
	if err != nil {
		return Account{}, err
	}
	defer release()

	// The store settles a collision when the row is written, below. This asks
	// first, under the slot, because deriving a hash for a row that is already
	// refused spends memory on nothing, and on the setup path a caller with no
	// credential spends it. Many callers racing for the first administrator
	// then derive a few times between them.
	if err = a.taken(ctx, id, folded); err != nil {
		return Account{}, err
	}

	hash, err := a.hasher.Hash(spec.Password)
	if err != nil {
		return Account{}, fmt.Errorf("%w: %w", ErrInvalidAccount, err)
	}

	now := a.now()
	account := Account{
		ID:          id,
		Username:    folded,
		DisplayName: display,
		Hash:        hash,
		CreatedAt:   now,
		PasswordAt:  now,
	}

	if err = a.store.Create(ctx, account); err != nil {
		return Account{}, err
	}

	account.Hash = ""

	return account, nil
}

// taken reports an id or a username another row already holds. A store that
// cannot answer reports neither, because the write below decides. Refusing here
// on an unreadable store would turn a store outage into a wrong answer about
// who exists.
func (a *Accounts) taken(ctx context.Context, id, username string) error {
	if _, err := a.store.ByID(ctx, id); err == nil {
		return fmt.Errorf("%w: %s", ErrIDTaken, id)
	}

	if _, err := a.store.ByUsername(ctx, username); err == nil {
		return ErrUsernameTaken
	}

	return nil
}

// rehashAtOtherCost carries the configured cost onto a row written at another
// cost. It runs after a correct password and never fails the login. A store
// that will not take the write leaves the old hash, which still verifies.
func (a *Accounts) rehashAtOtherCost(ctx context.Context, account Account, password []byte, now time.Time) {
	if !a.hasher.NeedsRehash(account.Hash) {
		return
	}

	hash, err := a.hasher.Hash(password)
	if err != nil {
		return
	}

	// The result is discarded. The login has already succeeded, and a failed
	// rehash costs the next login one more pass at the old cost.
	// It lands only over the hash that was checked, so a rehash that races a
	// password change cannot put the old password back.
	_ = a.store.SetPassword(ctx, account.ID, account.Hash, hash, now)
}

// identityOf renders an account as the contract's Identity.
//
// Roles are left empty. This server's authorization is the relation graph, and
// a role list beside the subject would be a second answer to who may do what.
func identityOf(account Account) authapi.Identity {
	return authapi.Identity{
		Subject:     account.ID,
		Email:       account.Username,
		DisplayName: account.DisplayName,
	}
}
