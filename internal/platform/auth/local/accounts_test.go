package local_test

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/argon2"

	"github.com/prusa3d/prusa-connect/internal/platform/auth"
	"github.com/prusa3d/prusa-connect/internal/platform/auth/local"
	"github.com/prusa3d/prusa-connect/sdk/authapi"
	"github.com/prusa3d/prusa-connect/sdk/authapi/authapitest"
)

// firstID is the id an installation gives its first administrator: the
// principal the authorization graph's first grant was written for.
const firstID = "operator"

const goodPassword = "correct horse battery"

func newAccounts(t *testing.T, options ...local.Option) (*local.Accounts, *local.MemoryStore) {
	t.Helper()

	store := local.NewMemoryStore()

	return local.New(store, nil, options...), store
}

// otherCostPrefix starts a hash made at the cost atOtherCost configures. The
// cost is neither build's default, so a path that ignores the configured
// hasher shows up as another prefix.
const otherCostPrefix = "$argon2id$v=19$m=12288,t=3,p=1$"

// atOtherCost configures a store to hash at 12 MiB with 3 passes.
func atOtherCost(t *testing.T) local.Option {
	t.Helper()

	hasher, err := auth.NewHasher(auth.Cost{MemoryKiB: 12 * 1024, Passes: 3})
	require.NoError(t, err)

	return local.WithHasher(hasher)
}

func setUp(t *testing.T, accounts *local.Accounts) local.Account {
	t.Helper()

	created, err := accounts.CreateFirst(t.Context(), firstID, local.NewAccount{
		Username: "Owner@Example.COM",
		Password: []byte(goodPassword),
	})
	require.NoError(t, err)

	return created
}

// TestTheFirstAccountTakesTheIdTheGraphAlreadyGrants is the whole of how a
// self-hoster becomes an administrator. There is no second bootstrap door: the
// account is created under the id the graph's first grant names, so the person
// who runs setup holds what that grant confers and nothing else had to be
// written.
func TestTheFirstAccountTakesTheIdTheGraphAlreadyGrants(t *testing.T) {
	t.Parallel()

	accounts, _ := newAccounts(t)

	needed, err := accounts.NeedsSetup(t.Context())
	require.NoError(t, err)
	require.True(t, needed, "a store with no account does not report the first-run state")

	created := setUp(t, accounts)
	require.Equal(t, firstID, created.ID)
	require.Equal(t, "owner@example.com", created.Username, "the username is not folded")
	require.Empty(t, created.Hash, "a created account carried its password hash out of the store")

	needed, err = accounts.NeedsSetup(t.Context())
	require.NoError(t, err)
	require.False(t, needed, "the first-run state survived the first account")
}

// TestTheSetupPathIsClosedOnceAnAccountExists is the door. An endpoint that
// creates an administrator and stays reachable is an administrator anybody can
// create.
func TestTheSetupPathIsClosedOnceAnAccountExists(t *testing.T) {
	t.Parallel()

	accounts, _ := newAccounts(t)
	setUp(t, accounts)

	_, err := accounts.CreateFirst(t.Context(), firstID, local.NewAccount{
		Username: "intruder@example.com",
		Password: []byte("another password"),
	})
	require.ErrorIs(t, err, local.ErrSetupClosed)

	// A different id is the same refusal. The count is what closes the path,
	// not the collision on the id behind it.
	_, err = accounts.CreateFirst(t.Context(), "second", local.NewAccount{
		Username: "intruder@example.com",
		Password: []byte("another password"),
	})
	require.ErrorIs(t, err, local.ErrSetupClosed)
}

// TestConcurrentSetupCallsPlantOneAdministrator is the race the count alone
// does not close. Two callers read an empty store at the same moment, and
// exactly one of them may write.
func TestConcurrentSetupCallsPlantOneAdministrator(t *testing.T) {
	t.Parallel()

	const callers = 16

	accounts, store := newAccounts(t)

	var (
		start sync.WaitGroup
		done  sync.WaitGroup
		mu    sync.Mutex
	)

	answers := make([]error, 0, callers)

	start.Add(1)
	done.Add(callers)

	for i := range callers {
		go func() {
			defer done.Done()

			start.Wait()

			_, err := accounts.CreateFirst(context.Background(), firstID, local.NewAccount{
				Username: "owner" + string(rune('a'+i)) + "@example.com",
				Password: []byte(goodPassword),
			})

			mu.Lock()
			defer mu.Unlock()

			answers = append(answers, err)
		}()
	}

	start.Done()
	done.Wait()

	won := 0

	for _, err := range answers {
		if err == nil {
			won++

			continue
		}

		require.ErrorIs(t, err, local.ErrSetupClosed,
			"a caller that lost the race was told something other than a closed path")
	}

	require.Equal(t, 1, won, "%d callers planted an administrator", won)

	held, err := store.Count(t.Context())
	require.NoError(t, err)
	require.Equal(t, 1, held, "the store holds %d accounts after one setup", held)
}

// TestTheLastAccountCannotBeDeleted keeps the setup path shut. Emptying the
// store would put the deployment back into its first-run state, where the next
// caller to reach the setup endpoint becomes the administrator.
func TestTheLastAccountCannotBeDeleted(t *testing.T) {
	t.Parallel()

	accounts, _ := newAccounts(t)
	first := setUp(t, accounts)

	require.ErrorIs(t, accounts.Delete(t.Context(), first.ID), local.ErrLastAccount)

	needed, err := accounts.NeedsSetup(t.Context())
	require.NoError(t, err)
	require.False(t, needed, "deleting the last account reopened the setup path")

	second, err := accounts.Create(t.Context(), local.NewAccount{
		Username: "colleague@example.com",
		Password: []byte(goodPassword),
	})
	require.NoError(t, err)

	require.NoError(t, accounts.Delete(t.Context(), second.ID))
	require.ErrorIs(t, accounts.Delete(t.Context(), first.ID), local.ErrLastAccount)
}

// TestAnAddedAccountLogsInAndIsNotTheAdministrator is the second half of what
// the owner asked for: the person who set the server up can add more people.
// A minted id holds no grant, so being able to log in and being able to do
// something are separate facts.
func TestAnAddedAccountLogsInAndIsNotTheAdministrator(t *testing.T) {
	t.Parallel()

	accounts, _ := newAccounts(t)
	first := setUp(t, accounts)

	colleague, err := accounts.Create(t.Context(), local.NewAccount{
		Username:    "colleague@example.com",
		DisplayName: "A Colleague",
		Password:    []byte("another good password"),
	})
	require.NoError(t, err)
	require.NotEqual(t, first.ID, colleague.ID)
	require.True(t, strings.HasPrefix(colleague.ID, "u"), "a minted id is %q", colleague.ID)

	identity, err := accounts.VerifyCredentials(t.Context(), "colleague@example.com", "another good password")
	require.NoError(t, err)
	require.Equal(t, colleague.ID, identity.Subject)
	require.Equal(t, "A Colleague", identity.DisplayName)
	require.Empty(t, identity.Roles,
		"the provider named a role, which is a second answer to what a caller may do")
}

// TestADuplicateUsernameIsRefused holds the property login depends on: one
// username names one account, whatever case it was typed in.
func TestADuplicateUsernameIsRefused(t *testing.T) {
	t.Parallel()

	accounts, _ := newAccounts(t)
	setUp(t, accounts)

	_, err := accounts.Create(t.Context(), local.NewAccount{
		Username: "OWNER@example.com",
		Password: []byte(goodPassword),
	})
	require.ErrorIs(t, err, local.ErrUsernameTaken)
}

// TestAWrongPasswordAndAnUnknownUserGiveOneAnswer is the enumeration half at
// this layer. The timing half is in timing_test.go.
func TestAWrongPasswordAndAnUnknownUserGiveOneAnswer(t *testing.T) {
	t.Parallel()

	accounts, _ := newAccounts(t)
	setUp(t, accounts)

	cases := map[string]struct{ username, password string }{
		"wrong password":  {username: "owner@example.com", password: "not the password"},
		"unknown user":    {username: "nobody@example.com", password: goodPassword},
		"empty username":  {username: "", password: goodPassword},
		"empty password":  {username: "owner@example.com", password: ""},
		"username spaced": {username: "  owner@example.com  ", password: "not the password"},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := accounts.VerifyCredentials(t.Context(), tc.username, tc.password)
			require.ErrorIs(t, err, authapi.ErrInvalidCredentials)
		})
	}
}

// TestAStoreThatCannotAnswerRefusesTheLogin is the fail-closed half. A store
// that is unreachable must not be the path a caller is admitted on.
func TestAStoreThatCannotAnswerRefusesTheLogin(t *testing.T) {
	t.Parallel()

	accounts, store := newAccounts(t)
	setUp(t, accounts)

	store.Break(errors.New("the account store is unreachable"))

	_, err := accounts.VerifyCredentials(t.Context(), "owner@example.com", goodPassword)
	require.ErrorIs(t, err, authapi.ErrInvalidCredentials,
		"an unreachable store answered a login as something other than a refusal")
}

// TestAChangedPasswordRefusesTheOldOne is the change itself. The sessions it
// has to end are held one layer up, in session, because this store issues none.
func TestAChangedPasswordRefusesTheOldOne(t *testing.T) {
	t.Parallel()

	accounts, store := newAccounts(t, atOtherCost(t))
	first := setUp(t, accounts)

	before, err := store.ByID(t.Context(), first.ID)
	require.NoError(t, err)

	require.ErrorIs(t,
		accounts.ChangePassword(t.Context(), first.ID, []byte("not the password"), []byte("a new password")),
		authapi.ErrInvalidCredentials,
		"a password change went through on the wrong current password")

	require.NoError(t,
		accounts.ChangePassword(t.Context(), first.ID, []byte(goodPassword), []byte("a new password")))

	changed, err := store.ByID(t.Context(), first.ID)
	require.NoError(t, err)
	require.NotEqual(t, before.Hash, changed.Hash)
	require.True(t, strings.HasPrefix(changed.Hash, otherCostPrefix),
		"a changed password was hashed as %s and not at the configured cost", changed.Hash)

	_, err = accounts.VerifyCredentials(t.Context(), "owner@example.com", goodPassword)
	require.ErrorIs(t, err, authapi.ErrInvalidCredentials, "the old password still logs in")

	_, err = accounts.VerifyCredentials(t.Context(), "owner@example.com", "a new password")
	require.NoError(t, err)
}

// TestAnAdministratorResetTakesNoCurrentPassword is the recovery path for a
// person who has lost theirs.
func TestAnAdministratorResetTakesNoCurrentPassword(t *testing.T) {
	t.Parallel()

	accounts, store := newAccounts(t, atOtherCost(t))
	first := setUp(t, accounts)

	require.NoError(t, accounts.SetPassword(t.Context(), first.ID, []byte("a reset password")))

	reset, err := store.ByID(t.Context(), first.ID)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(reset.Hash, otherCostPrefix),
		"a reset password was hashed as %s and not at the configured cost", reset.Hash)

	_, err = accounts.VerifyCredentials(t.Context(), "owner@example.com", "a reset password")
	require.NoError(t, err)
}

// TestAPasswordBelowTheFloorIsRefused holds NIST SP 800-63B section 5.1.1.2's
// eight character minimum at the point an account is written.
func TestAPasswordBelowTheFloorIsRefused(t *testing.T) {
	t.Parallel()

	accounts, _ := newAccounts(t)

	_, err := accounts.CreateFirst(t.Context(), firstID, local.NewAccount{
		Username: "owner@example.com",
		Password: []byte("short"),
	})
	require.ErrorIs(t, err, local.ErrInvalidAccount)
	require.ErrorIs(t, err, auth.ErrPasswordTooShort)

	needed, setupErr := accounts.NeedsSetup(t.Context())
	require.NoError(t, setupErr)
	require.True(t, needed, "a refused setup left an account behind")
}

// TestAUsernameThisStoreWillNotTakeIsRefused bounds what reaches the store.
func TestAUsernameThisStoreWillNotTakeIsRefused(t *testing.T) {
	t.Parallel()

	accounts, _ := newAccounts(t)

	cases := map[string]string{
		"empty":                 "",
		"whitespace":            "   ",
		"a space in the middle": "two words",
		"over the ceiling":      strings.Repeat("a", local.MaxUsernameBytes+1),
	}

	for name, username := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := accounts.Create(t.Context(), local.NewAccount{
				Username: username,
				Password: []byte(goodPassword),
			})
			require.ErrorIs(t, err, local.ErrInvalidAccount)
		})
	}
}

// TestListNamesNoPasswordHash is what keeps a hash out of an administration
// page. The store holds one and the accessor clears it.
func TestListNamesNoPasswordHash(t *testing.T) {
	t.Parallel()

	accounts, store := newAccounts(t)
	setUp(t, accounts)

	rows, err := accounts.List(t.Context())
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Empty(t, rows[0].Hash, "the account listing carries a password hash")

	one, err := accounts.Get(t.Context(), firstID)
	require.NoError(t, err)
	require.Empty(t, one.Hash, "reading one account carries a password hash")

	held, err := store.ByID(t.Context(), firstID)
	require.NoError(t, err)
	require.NotEmpty(t, held.Hash, "the store kept no hash, so nothing was cleared")
	require.True(t, strings.HasPrefix(held.Hash, "$argon2id$"), "the stored hash is %q", held.Hash)
}

// TestADeletedAccountIsReportedRevoked is the revalidation contract. A host
// running the background sync ends that person's sessions on this answer.
func TestADeletedAccountIsReportedRevoked(t *testing.T) {
	t.Parallel()

	accounts, _ := newAccounts(t)
	setUp(t, accounts)

	colleague, err := accounts.Create(t.Context(), local.NewAccount{
		Username: "colleague@example.com",
		Password: []byte(goodPassword),
	})
	require.NoError(t, err)

	identity, err := accounts.RefreshIdentity(t.Context(), colleague.ID)
	require.NoError(t, err)
	require.Equal(t, colleague.ID, identity.Subject)

	require.NoError(t, accounts.Delete(t.Context(), colleague.ID))

	_, err = accounts.RefreshIdentity(t.Context(), colleague.ID)
	require.ErrorIs(t, err, authapi.ErrIdentityRevoked)
}

// TestTheRedirectFlowIsReportedUnsupported holds what the contract requires of
// a provider that serves one flow and not the other.
func TestTheRedirectFlowIsReportedUnsupported(t *testing.T) {
	t.Parallel()

	accounts, _ := newAccounts(t)

	_, _, err := accounts.BeginLogin(t.Context(), "https://host/callback", "state")
	require.ErrorIs(t, err, authapi.ErrUnsupportedFlow)

	_, err = accounts.CompleteLogin(t.Context(), url.Values{"code": {"x"}}, "")
	require.ErrorIs(t, err, authapi.ErrUnsupportedFlow)
}

// TestTheLocalProviderPassesTheContract runs the suite every identity provider
// must pass, which is the point of putting this store behind sdk/authapi.
func TestTheLocalProviderPassesTheContract(t *testing.T) {
	t.Parallel()

	authapitest.RunIdentityProviderContract(t, func(t *testing.T) authapitest.Harness {
		t.Helper()

		accounts, _ := newAccounts(t)
		setUp(t, accounts)

		return authapitest.Harness{
			Provider:      accounts,
			ValidUsername: "owner@example.com",
			ValidPassword: goodPassword,
		}
	})
}

// TestARaisedWorkFactorReachesAnExistingRow is what carries a stronger argon2id
// profile onto a password nobody has changed.
func TestARaisedWorkFactorReachesAnExistingRow(t *testing.T) {
	t.Parallel()

	accounts, store := newAccounts(t)
	setUp(t, accounts)

	weaker := hashAtWeakerParameters(t, goodPassword)
	require.True(t, auth.NewDefaultHasher().NeedsRehash(weaker), "the fixture did not weaken the hash")
	require.NoError(t, store.SetPassword(t.Context(), firstID, "", weaker, time.Now()))

	_, err := accounts.VerifyCredentials(t.Context(), "owner@example.com", goodPassword)
	require.NoError(t, err, "a row at weaker parameters no longer verifies")

	rehashed, err := store.ByID(t.Context(), firstID)
	require.NoError(t, err)
	require.False(t, auth.NewDefaultHasher().NeedsRehash(rehashed.Hash),
		"the row kept its weaker parameters after a login")
}

// TestALoginMovesARowToTheConfiguredCost is a server whose operator changed the
// cost. A row hashed at the old cost still lets its owner in, and that login
// writes the row again at the new cost. The old cost here is the stronger
// normal one, which a printer must not keep paying on every login.
func TestALoginMovesARowToTheConfiguredCost(t *testing.T) {
	t.Parallel()

	printer, err := auth.NewHasher(auth.Cost{MemoryKiB: 7 * 1024, Passes: 5})
	require.NoError(t, err)

	normal, err := auth.NewHasher(auth.Cost{MemoryKiB: 19 * 1024, Passes: 2})
	require.NoError(t, err)

	store := local.NewMemoryStore()
	accounts := local.New(store, nil, local.WithHasher(printer))
	setUp(t, accounts)

	created, err := store.ByID(t.Context(), firstID)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(created.Hash, "$argon2id$v=19$m=7168,t=5,p=1$"),
		"a new account was hashed at %s and not at the configured cost", created.Hash)

	old, err := normal.Hash([]byte(goodPassword))
	require.NoError(t, err)
	require.NoError(t, store.SetPassword(t.Context(), firstID, "", old, time.Now()))

	_, err = accounts.VerifyCredentials(t.Context(), "owner@example.com", "not the password")
	require.ErrorIs(t, err, authapi.ErrInvalidCredentials)

	kept, err := store.ByID(t.Context(), firstID)
	require.NoError(t, err)
	require.Equal(t, old, kept.Hash, "a wrong password rewrote the row")

	_, err = accounts.VerifyCredentials(t.Context(), "owner@example.com", goodPassword)
	require.NoError(t, err, "a row at the old cost no longer verifies")

	moved, err := store.ByID(t.Context(), firstID)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(moved.Hash, "$argon2id$v=19$m=7168,t=5,p=1$"),
		"the row is still at %s after a login", moved.Hash)

	_, err = accounts.VerifyCredentials(t.Context(), "owner@example.com", goodPassword)
	require.NoError(t, err, "the rewritten row does not verify")
}

// hashAtWeakerParameters writes the PHC string an older build would have
// stored. It is built here rather than by weakening a current hash in place,
// because the parameters are inputs to the derivation and editing them leaves a
// key no password produces.
func hashAtWeakerParameters(t *testing.T, password string) string {
	t.Helper()

	const (
		memory uint32 = 8 * 1024
		passes uint32 = 1
		lanes  uint8  = 1
		keyLen uint32 = 32
	)

	salt := make([]byte, 16)
	_, err := rand.Read(salt)
	require.NoError(t, err)

	key := argon2.IDKey([]byte(password), salt, passes, memory, lanes, keyLen)
	raw := base64.RawStdEncoding

	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, memory, passes, lanes,
		raw.EncodeToString(salt), raw.EncodeToString(key))
}

// TestTwoChangesThatKnowTheOldPasswordCannotBothWin is two people who both know
// the old password, changing it at once. Both check the old password before
// either writes. The write is conditional on the hash that was checked, so
// exactly one change lands and the other is refused.
func TestTwoChangesThatKnowTheOldPasswordCannotBothWin(t *testing.T) {
	t.Parallel()

	store := &bothReadFirst{MemoryStore: local.NewMemoryStore(), readers: 2}
	store.ready = make(chan struct{})

	accounts := local.New(store, nil)

	first, err := accounts.CreateFirst(t.Context(), firstID, local.NewAccount{
		Username: "owner@example.com", Password: []byte(goodPassword),
	})
	require.NoError(t, err)

	store.arm()

	results := make(chan error, 2)

	for _, next := range []string{"the first new password", "the second new password"} {
		go func() {
			results <- accounts.ChangePassword(t.Context(), first.ID, []byte(goodPassword), []byte(next))
		}()
	}

	won := 0

	for range 2 {
		if <-results == nil {
			won++
		}
	}

	require.Equal(t, 1, won, "two changes that checked the same old password both landed")
}

// bothReadFirst is an account store that holds every password write until the
// armed number of reads has happened, so two changes both read the old hash
// before either writes.
type bothReadFirst struct {
	*local.MemoryStore

	mu      sync.Mutex
	armed   bool
	readers int
	ready   chan struct{}
}

func (s *bothReadFirst) arm() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.armed = true
}

func (s *bothReadFirst) ByID(ctx context.Context, id string) (local.Account, error) {
	row, err := s.MemoryStore.ByID(ctx, id)

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.armed && s.readers > 0 {
		s.readers--
		if s.readers == 0 {
			close(s.ready)
		}
	}

	return row, err
}

func (s *bothReadFirst) ReplacePassword(
	ctx context.Context, id, expected, hash string, at time.Time,
) (int64, error) {
	<-s.ready

	return s.MemoryStore.ReplacePassword(ctx, id, expected, hash, at)
}

// TestARehashCannotPutAnOldPasswordBack is a login's rehash racing a password
// change. The login checked the old password against a weak row, and the change
// lands before the rehash writes. The rehash names the hash it checked, so it
// finds the row changed and writes nothing, and the new password stands.
func TestARehashCannotPutAnOldPasswordBack(t *testing.T) {
	t.Parallel()

	inner := local.NewMemoryStore()
	store := &changeBeforeRehash{MemoryStore: inner}
	accounts := local.New(store, nil)
	setUp(t, accounts)

	weaker := hashAtWeakerParameters(t, goodPassword)
	require.NoError(t, inner.SetPassword(t.Context(), firstID, "", weaker, time.Now()))

	changed, err := auth.NewDefaultHasher().Hash([]byte("the changed password"))
	require.NoError(t, err)

	store.changed = changed

	_, err = accounts.VerifyCredentials(t.Context(), "owner@example.com", goodPassword)
	require.NoError(t, err)

	_, err = accounts.VerifyCredentials(t.Context(), "owner@example.com", "the changed password")
	require.NoError(t, err, "the rehash of the old password replaced the change")

	_, err = accounts.VerifyCredentials(t.Context(), "owner@example.com", goodPassword)
	require.ErrorIs(t, err, authapi.ErrInvalidCredentials, "the old password came back")
}

// changeBeforeRehash is an account store where a password change lands just
// before the first rehash write, the way a change from another request would.
type changeBeforeRehash struct {
	*local.MemoryStore

	changed string
	landed  bool
}

func (s *changeBeforeRehash) SetPassword(ctx context.Context, id, expected, hash string, at time.Time) error {
	if s.changed != "" && !s.landed {
		s.landed = true

		if _, err := s.MemoryStore.ReplacePassword(ctx, id, "", s.changed, at); err != nil {
			return err
		}
	}

	return s.MemoryStore.SetPassword(ctx, id, expected, hash, at)
}

// TestAChangeSurvivesARehashThatLandedFirst is a login's rehash landing between
// a change's read and its write. The stored hash changed, but the password did
// not. The change reads the row again, finds the current password still
// verifies, and lands.
func TestAChangeSurvivesARehashThatLandedFirst(t *testing.T) {
	t.Parallel()

	inner := local.NewMemoryStore()
	store := &rehashBeforeChange{MemoryStore: inner}
	accounts := local.New(store, nil)
	setUp(t, accounts)

	rehashed, err := auth.NewDefaultHasher().Hash([]byte(goodPassword))
	require.NoError(t, err)

	store.rehash = rehashed

	require.NoError(t,
		accounts.ChangePassword(t.Context(), firstID, []byte(goodPassword), []byte("a new password")),
		"a correct change failed because a rehash of the same password landed first")

	_, err = accounts.VerifyCredentials(t.Context(), "owner@example.com", "a new password")
	require.NoError(t, err)
}

// rehashBeforeChange is an account store where a rehash of the same password
// lands just before the first password change writes.
type rehashBeforeChange struct {
	*local.MemoryStore

	rehash string
	landed bool
}

func (s *rehashBeforeChange) ReplacePassword(
	ctx context.Context, id, expected, hash string, at time.Time,
) (int64, error) {
	if s.rehash != "" && !s.landed {
		s.landed = true

		if err := s.MemoryStore.SetPassword(ctx, id, "", s.rehash, at); err != nil {
			return 0, err
		}
	}

	return s.MemoryStore.ReplacePassword(ctx, id, expected, hash, at)
}
