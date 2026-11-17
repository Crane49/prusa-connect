package xbuddy

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// approvedPrinter registers one printer, approves it as an operator would and
// collects its token, which is the state a printer is in once it has joined.
func approvedPrinter(t *testing.T, registry *registrar, fingerprint string) (code, token string) {
	t.Helper()

	code, _, err := registry.begin(t.Context(), Registration{Fingerprint: fingerprint, Serial: "SN-" + fingerprint})
	require.NoError(t, err)

	_, err = registry.Approve(t.Context(), code, "user:ada")
	require.NoError(t, err)

	issued, err := registry.complete(t.Context(), code)
	require.NoError(t, err)

	return code, issued.Token
}

// A printer whose record was retired is refused on /p/ws in every mode that
// checks a credential, with the token it was issued and with its fingerprint.
// Before it, it was admitted, so each case is tested in both directions.
func TestAForgottenPrinterIsRefusedInEveryModeThatChecksACredential(t *testing.T) {
	t.Parallel()

	for _, mode := range []Mode{ModeToken, ModeFingerprint, ModeBoth} {
		t.Run(string(mode), func(t *testing.T) {
			t.Parallel()

			driver := NewDriver(mode, 4, 0, "", nil)
			_, token := approvedPrinter(t, driver.registrar, "FP-1")

			_, err := driver.registrar.authenticate("FP-1", token)
			require.NoError(t, err, "the approved printer is refused before it is forgotten")

			require.NoError(t, driver.ForgetDevice(t.Context(),
				connectorapi.Identity{UnitUID: "FP-1"}, time.Now().Add(time.Second)))

			_, err = driver.registrar.authenticate("FP-1", token)
			require.ErrorIs(t, err, ErrRefused, "the forgotten printer still authenticates")

			_, held := driver.registrar.registrationFor("FP-1")
			require.False(t, held, "the registration outlived the forget")
		})
	}
}

// Another printer in the same table keeps its credential.
func TestForgettingOnePrinterLeavesTheOthers(t *testing.T) {
	t.Parallel()

	registry := newRegistrar(ModeBoth, 0)
	_, gone := approvedPrinter(t, registry, "FP-1")
	_, kept := approvedPrinter(t, registry, "FP-2")

	require.NoError(t, registry.forgetDevice(t.Context(), "FP-1", time.Now().Add(time.Second)))

	_, err := registry.authenticate("FP-2", kept)
	require.NoError(t, err, "forgetting one printer refused another")

	_, err = registry.authenticate("FP-1", gone)
	require.ErrorIs(t, err, ErrRefused)
}

// A registration made at or after the moment is a new request and is kept. The
// host passes the moment the record was retired, so a printer that registered
// again since keeps the code a person may already have approved.
func TestARegistrationMadeAfterTheRetirementIsKept(t *testing.T) {
	t.Parallel()

	registry := newRegistrar(ModeToken, 0)
	retiredAt := registry.now()

	registry.now = func() time.Time { return retiredAt.Add(time.Minute) }

	_, token := approvedPrinter(t, registry, "FP-1")

	require.NoError(t, registry.forgetDevice(t.Context(), "FP-1", retiredAt))

	_, err := registry.authenticate("FP-1", token)
	require.NoError(t, err, "a registration made after the retirement was forgotten")
}

// A printer nobody registered is not an error, because the host calls this
// again for a device it retired before.
func TestForgettingAPrinterTheTableDoesNotHoldIsNotAnError(t *testing.T) {
	t.Parallel()

	registry := newRegistrar(ModeToken, 0)

	require.NoError(t, registry.forgetDevice(t.Context(), "FP-NOBODY", time.Now()))
	require.NoError(t, registry.forgetDevice(t.Context(), "", time.Now()))
}

// The withdrawal reaches the store, so the next process does not restore the
// credential. Without the store write a restart would let the printer back in.
func TestAForgottenPrinterIsRefusedByTheNextProcess(t *testing.T) {
	t.Parallel()

	store := newMemoryRegistrations()

	first := newRegistrar(ModeToken, 0)
	require.NoError(t, first.restore(t.Context(), "xbuddy", "printer", store))

	_, token := approvedPrinter(t, first, "FP-1")
	require.Len(t, store.rows, 1)

	require.NoError(t, first.forgetDevice(t.Context(), "FP-1", time.Now().Add(time.Second)))
	require.Empty(t, store.rows, "the store still holds the forgotten registration")

	second := newRegistrar(ModeToken, 0)
	require.NoError(t, second.restore(t.Context(), "xbuddy", "printer", store))

	_, err := second.authenticate("FP-1", token)
	if !errors.Is(err, ErrRefused) {
		t.Fatalf("the next process authenticated the forgotten printer with %v, want ErrRefused", err)
	}
}

// The printer gets back in the way a new one does: it registers, it is given a
// new code, and the token it collects after an approval is accepted.
func TestAForgottenPrinterCanRegisterAgain(t *testing.T) {
	t.Parallel()

	registry := newRegistrar(ModeToken, 0)
	oldCode, oldToken := approvedPrinter(t, registry, "FP-1")

	require.NoError(t, registry.forgetDevice(t.Context(), "FP-1", time.Now().Add(time.Second)))

	newCode, _, err := registry.begin(t.Context(), Registration{Fingerprint: "FP-1"})
	require.NoError(t, err)
	require.NotEqual(t, oldCode, newCode, "the printer was handed the code it had before")

	_, err = registry.complete(t.Context(), newCode)
	require.ErrorIs(t, err, ErrRegistrationPending, "the new code was let through without an approval")

	_, err = registry.Approve(t.Context(), newCode, "user:ada")
	require.NoError(t, err)

	issued, err := registry.complete(t.Context(), newCode)
	require.NoError(t, err)

	_, err = registry.authenticate("FP-1", issued.Token)
	require.NoError(t, err)

	_, err = registry.authenticate("FP-1", oldToken)
	require.ErrorIs(t, err, ErrRefused, "the token from before the forget works again")
}

// Mode none checks no credential, so a forget there keeps the registration. A
// printer in that mode keeps its token and does not register again, and what
// it said when it registered, such as its serial, would be lost for good.
func TestModeNoneKeepsTheRegistrationOfARemovedPrinter(t *testing.T) {
	t.Parallel()

	registry := newRegistrar(ModeNone, 0)

	code, _, err := registry.begin(t.Context(), Registration{Fingerprint: "FP-1", Serial: "SN-1"})
	require.NoError(t, err)

	_, err = registry.complete(t.Context(), code)
	require.NoError(t, err)

	require.NoError(t, registry.forgetDevice(t.Context(), "FP-1", time.Now().Add(time.Second)))

	held, ok := registry.registrationFor("FP-1")
	require.True(t, ok, "mode none forgot the registration of a printer it never checks")
	require.Equal(t, "SN-1", held.Serial)
}

// A device met in mode none carries the sixteen characters of a header as its
// unit id. Removing it must not take the credential of the registration that
// owns that prefix, which may be another machine's.
func TestForgettingAHeaderSpellingLeavesThePrefixOwnerAlone(t *testing.T) {
	t.Parallel()

	const full = "C2UV69LUJ3KIRTJFNK6FFU1AON1SGF4R2TFMBFEPUVVMOF4TDO"

	registry := newRegistrar(ModeToken, 0)
	_, token := approvedPrinter(t, registry, full)

	require.NoError(t, registry.forgetDevice(t.Context(), full[:fingerprintHeaderLength], time.Now().Add(time.Second)))

	_, err := registry.authenticate(full[:fingerprintHeaderLength], token)
	require.NoError(t, err, "forgetting a sixteen character unit id revoked the registration owning that prefix")
}

// failingForget is a store whose Forget fails while fail is set.
type failingForget struct {
	*memoryRegistrations

	fail bool
}

func (f *failingForget) Forget(ctx context.Context, connector, code string) error {
	if f.fail {
		return errors.New("the disk is gone")
	}

	return f.memoryRegistrations.Forget(ctx, connector, code)
}

// A store write that failed is made again by the next call for the same
// printer, so a second removal that answers nil has removed the row too.
func TestAForgetWhoseStoreWriteFailedIsFinishedByTheNextCall(t *testing.T) {
	t.Parallel()

	store := &failingForget{memoryRegistrations: newMemoryRegistrations(), fail: true}

	registry := newRegistrar(ModeToken, 0)
	require.NoError(t, registry.restore(t.Context(), "xbuddy", "printer", store))

	_, token := approvedPrinter(t, registry, "FP-1")
	before := time.Now().Add(time.Second)

	require.Error(t, registry.forgetDevice(t.Context(), "FP-1", before))

	_, err := registry.authenticate("FP-1", token)
	require.ErrorIs(t, err, ErrRefused, "the printer is let in while the store write is owed")
	require.Len(t, store.rows, 1)

	store.fail = false

	require.NoError(t, registry.forgetDevice(t.Context(), "FP-1", before))
	require.Empty(t, store.rows, "the second call answered nil and left the row on the disk")
}
