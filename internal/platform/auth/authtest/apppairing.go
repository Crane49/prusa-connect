// Package authtest holds the checks every implementation of an auth store
// port must pass, so the store in memory and the one over a database are held
// to one behaviour.
package authtest

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/prusa3d/prusa-connect/internal/platform/auth"
)

// pairingStart is the clock every check starts from.
var pairingStart = time.Date(2026, time.October, 24, 12, 0, 0, 0, time.UTC)

// The intervals after the start at which a check polls, and the generation a
// decision is fenced at.
const (
	twoIntervals      = 2
	threeIntervals    = 3
	decidedGeneration = 3
)

// AppPairingContract runs the pairing flow of RFC 8628 against a store: the
// happy path, a denial, an expiry, a code used twice, a poll that comes too
// soon and the bound on waiting pairings. open returns an empty store.
func AppPairingContract(t *testing.T, open func(t *testing.T) auth.AppPairingStore) {
	t.Helper()

	t.Run("an approved pairing is collected once with the approved scopes", func(t *testing.T) {
		t.Parallel()

		store := open(t)
		issued := request(t, store, pairingStart)

		mustPoll(t, store, issued.DeviceCode, pairingStart.Add(auth.AppPairingInterval), auth.ErrAppPairingPending)

		found, err := auth.LiveAppPairing(t.Context(), store, auth.FormatUserCode(issued.UserCode), pairingStart)
		if err != nil {
			t.Fatalf("the person cannot find the pairing by the code shown to them: %v", err)
		}

		if found.AppName != "kitchen display" || !slices.Equal(found.Scopes, []string{"read", "operate"}) {
			t.Fatalf("the person reads %q asking for %v", found.AppName, found.Scopes)
		}

		decide(t, store, issued.UserCode, auth.AppPairingApproved, []string{"read"}, pairingStart)

		collected, err := auth.PollAppPairing(t.Context(), store, issued.DeviceCode,
			pairingStart.Add(twoIntervals*auth.AppPairingInterval))
		if err != nil {
			t.Fatalf("an approved pairing was not collected: %v", err)
		}

		if collected.DecidedBy != "user:ada" || !slices.Equal(collected.DecidedScopes, []string{"read"}) {
			t.Fatalf("collected for %q with %v", collected.DecidedBy, collected.DecidedScopes)
		}

		mustPoll(
			t,
			store,
			issued.DeviceCode,
			pairingStart.Add(threeIntervals*auth.AppPairingInterval),
			auth.ErrAppPairingUnknown,
		)
	})

	t.Run("a denied pairing is answered once and then forgotten", func(t *testing.T) {
		t.Parallel()

		store := open(t)
		issued := request(t, store, pairingStart)

		decide(t, store, issued.UserCode, auth.AppPairingDenied, nil, pairingStart)

		err := auth.DecideAppPairing(t.Context(), store, auth.AppPairing{
			UserCode: issued.UserCode, State: auth.AppPairingApproved, DecidedBy: "user:ada",
			DecidedScopes: []string{"read"},
		}, pairingStart)
		if !errors.Is(err, auth.ErrAppPairingDecided) {
			t.Fatalf("a denied pairing was approved afterwards: %v", err)
		}

		mustPoll(t, store, issued.DeviceCode, pairingStart.Add(auth.AppPairingInterval), auth.ErrAppPairingDenied)
		mustPoll(
			t,
			store,
			issued.DeviceCode,
			pairingStart.Add(twoIntervals*auth.AppPairingInterval),
			auth.ErrAppPairingUnknown,
		)
	})

	t.Run("a pairing past its lifetime is neither decided nor collected", func(t *testing.T) {
		t.Parallel()

		store := open(t)
		issued := request(t, store, pairingStart)
		late := pairingStart.Add(auth.AppPairingLifetime)

		if _, err := auth.LiveAppPairing(t.Context(), store, issued.UserCode, late); !errors.Is(err,
			auth.ErrAppPairingUnknown) {
			t.Fatalf("an expired pairing is offered to a person: %v", err)
		}

		err := auth.DecideAppPairing(t.Context(), store, auth.AppPairing{
			UserCode: issued.UserCode, State: auth.AppPairingApproved, DecidedBy: "user:ada",
			DecidedScopes: []string{"read"},
		}, late)
		if !errors.Is(err, auth.ErrAppPairingDecided) {
			t.Fatalf("an expired pairing was approved: %v", err)
		}

		mustPoll(t, store, issued.DeviceCode, late, auth.ErrAppPairingExpired)
		mustPoll(t, store, issued.DeviceCode, late.Add(time.Minute), auth.ErrAppPairingUnknown)
	})

	t.Run("a poll that comes too soon slows the app down", func(t *testing.T) {
		t.Parallel()

		store := open(t)
		issued := request(t, store, pairingStart)

		// The first poll may come at once.
		mustPoll(t, store, issued.DeviceCode, pairingStart, auth.ErrAppPairingPending)

		slowed := pairingStart.Add(time.Second)
		mustPoll(t, store, issued.DeviceCode, slowed, auth.ErrAppPairingSlowDown)

		// The interval is now ten seconds from the slowed poll, so five
		// seconds later is still too soon.
		mustPoll(t, store, issued.DeviceCode, slowed.Add(auth.AppPairingInterval), auth.ErrAppPairingSlowDown)
		mustPoll(t, store, issued.DeviceCode,
			slowed.Add(auth.AppPairingInterval).Add(threeIntervals*auth.AppPairingInterval), auth.ErrAppPairingPending)
	})

	t.Run("a code nobody issued is unknown", func(t *testing.T) {
		t.Parallel()

		store := open(t)

		mustPoll(t, store, "never-issued", pairingStart, auth.ErrAppPairingUnknown)
	})

	t.Run("the waiting pairings are bounded", func(t *testing.T) {
		t.Parallel()

		store := open(t)

		for range auth.MaxPendingAppPairings {
			request(t, store, pairingStart)
		}

		_, err := auth.RequestAppPairing(t.Context(), store, auth.AppPairingRequest{
			AppName: "one too many", Scopes: []string{"read"},
		}, pairingStart)
		if !errors.Is(err, auth.ErrAppPairingBusy) {
			t.Fatalf("a request past the bound was accepted: %v", err)
		}

		// Once the waiting ones expire, the room is back.
		request(t, store, pairingStart.Add(auth.AppPairingLifetime))
	})
}

func request(t *testing.T, store auth.AppPairingStore, now time.Time) auth.IssuedAppPairing {
	t.Helper()

	issued, err := auth.RequestAppPairing(t.Context(), store, auth.AppPairingRequest{
		AppName: "kitchen display", Scopes: []string{"read", "operate"}, ClientAddress: "192.0.2.7",
	}, now)
	if err != nil {
		t.Fatalf("request a pairing: %v", err)
	}

	return issued
}

func decide(t *testing.T, store auth.AppPairingStore, userCode, state string, scopes []string, now time.Time) {
	t.Helper()

	err := auth.DecideAppPairing(t.Context(), store, auth.AppPairing{
		UserCode: userCode, State: state, DecidedBy: "user:ada", DecidedScopes: scopes,
		DecidedGeneration: decidedGeneration, DecidedRoot: "user:ada", RootGeneration: decidedGeneration,
	}, now)
	if err != nil {
		t.Fatalf("decide the pairing: %v", err)
	}
}

func mustPoll(t *testing.T, store auth.AppPairingStore, deviceCode string, now time.Time, want error) {
	t.Helper()

	_, err := auth.PollAppPairing(t.Context(), store, deviceCode, now)
	if !errors.Is(err, want) {
		t.Fatalf("a poll at %s answered %v, want %v", now.Sub(pairingStart), err, want)
	}
}
