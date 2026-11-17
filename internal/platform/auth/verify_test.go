package auth

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// Every kind is asserted on every transport, permit and refusal alike. A permit
// test without its refusal test is half a test.
//
// The table is checked twice: against the rule itself, and against a credential
// minted and presented. Neither a wrong table nor a Verify that ignores it
// passes on its own.
func TestTheCrossTransportRuleHoldsBothWays(t *testing.T) {
	t.Parallel()

	want := map[Kind]map[Transport]bool{
		// The zero value is in the table too. A kind nobody set is accepted
		// nowhere, which is the safe direction to fail in.
		KindUnknown: {TransportHTTP: false, TransportSSE: false, TransportMQTT: false},
		KindSession: {TransportHTTP: true, TransportSSE: true, TransportMQTT: false},
		KindAPI:     {TransportHTTP: true, TransportSSE: true, TransportMQTT: false},
		KindMQTT:    {TransportHTTP: false, TransportSSE: false, TransportMQTT: true},
		// A bearer token another issuer signed is read where an api credential is.
		KindPrusaAccount: {TransportHTTP: true, TransportSSE: true, TransportMQTT: false},
	}

	for kind, byTransport := range want {
		for on, accepted := range byTransport {
			t.Run(kind.String()+" on "+on.String(), func(t *testing.T) {
				t.Parallel()

				require.Equal(t, accepted, kind.AcceptedOn(on))

				if kind == KindUnknown || kind == KindPrusaAccount {
					// A credential of no kind cannot be minted. A bearer token
					// is minted by another issuer, so the rule is checked on
					// the kind here, and through the resolver in
					// bearer_test.go.
					_, _, err := Issue(t.Context(), NewMemoryStore(), "operator", kind, time.Now())
					require.Error(t, err)

					return
				}

				store := NewMemoryStore()
				verifier := NewVerifier(store, nil)
				token, err := issueOfKind(t, store, kind, time.Now())
				require.NoError(t, err)

				cred, err := verifier.Verify(t.Context(), token, on)
				if !accepted {
					require.Error(t, err, "%s must be refused on %s", kind, on)
					requireIndistinguishableRefusal(t, err)

					return
				}

				require.NoError(t, err)
				require.Equal(t, "operator", cred.Subject)
			})
		}
	}
}

// A caller must not learn which half of a guess was right.
func TestAnExpiredTokenIsIndistinguishableFromOneThatNeverExisted(t *testing.T) {
	t.Parallel()

	store := NewMemoryStore()
	issuedAt := time.Now().Add(-2 * MQTTLifetime)
	verifier := NewVerifier(store, time.Now)

	expired, _, err := Issue(t.Context(), store, "operator", KindMQTT, issuedAt)
	require.NoError(t, err)

	neverExisted, _, _, err := Mint()
	require.NoError(t, err)

	_, expiredErr := verifier.Verify(t.Context(), expired, TransportMQTT)
	_, unknownErr := verifier.Verify(t.Context(), neverExisted, TransportMQTT)

	require.Error(t, expiredErr)
	require.Error(t, unknownErr)
	requireSameRendering(t, expiredErr, unknownErr)
}

// A caller who presents a broker credential on HTTP learns nothing that a
// caller presenting rubbish does not.
func TestAnMQTTTokenIsRefusedInTheSameWordsAsAnInvalidOne(t *testing.T) {
	t.Parallel()

	store := NewMemoryStore()
	verifier := NewVerifier(store, nil)

	broker, _, err := Issue(t.Context(), store, "operator", KindMQTT, time.Now())
	require.NoError(t, err)

	rubbish, _, _, err := Mint()
	require.NoError(t, err)

	for _, on := range []Transport{TransportHTTP, TransportSSE} {
		t.Run(on.String(), func(t *testing.T) {
			t.Parallel()

			_, brokerErr := verifier.Verify(t.Context(), broker, on)
			_, rubbishErr := verifier.Verify(t.Context(), rubbish, on)

			require.Error(t, brokerErr)
			require.Error(t, rubbishErr)
			requireSameRendering(t, brokerErr, rubbishErr)
		})
	}

	// The same credential on its own transport is admitted, so the refusals
	// above come from the rule and not from a broken mint.
	cred, err := verifier.Verify(t.Context(), broker, TransportMQTT)
	require.NoError(t, err)
	require.Equal(t, KindMQTT, cred.Kind)
}

// The verifier is fail-closed, so nothing above it has to remember to check.
func TestAStoreThatCannotAnswerRefuses(t *testing.T) {
	t.Parallel()

	store := NewMemoryStore()
	verifier := NewVerifier(store, nil)

	token, _, err := IssueAPI(
		t.Context(),
		store,
		APISpec{Subject: "operator", Name: "script", Scopes: []string{"read"}},
		time.Now(),
	)
	require.NoError(t, err)

	cred, err := verifier.Verify(t.Context(), token, TransportHTTP)
	require.NoError(t, err, "the credential is good while the store answers")
	require.Equal(t, "operator", cred.Subject)

	store.Break(errors.New("database is unreachable"))

	_, err = verifier.Verify(t.Context(), token, TransportHTTP)
	require.Error(t, err, "an unreachable store must refuse, not admit")
	requireIndistinguishableRefusal(t, err)

	store.Break(nil)

	_, err = verifier.Verify(t.Context(), token, TransportHTTP)
	require.NoError(t, err, "the same credential is admitted again once the store answers")
}

// A session that keeps being used keeps living. An api token's life is
// absolute.
func TestASessionSlidesAndAnAPITokenDoesNot(t *testing.T) {
	t.Parallel()

	store := NewMemoryStore()
	start := time.Now()
	now := start
	verifier := NewVerifier(store, func() time.Time { return now })

	session, cred, err := Issue(t.Context(), store, "operator", KindSession, start)
	require.NoError(t, err)
	require.Equal(t, SessionIdle, cred.Idle)

	// Use it just inside the idle window, repeatedly, past what a single window
	// would have allowed.
	for range 3 {
		now = now.Add(SessionIdle - time.Minute)

		_, err = verifier.Verify(t.Context(), session, TransportHTTP)
		require.NoError(t, err, "an active session must not expire at %s", now.Sub(start))
	}

	// Then stop using it for longer than the window.
	now = now.Add(SessionIdle + time.Minute)
	_, err = verifier.Verify(t.Context(), session, TransportHTTP)
	require.Error(t, err, "an idle session must expire")

	apiToken, apiCred, err := IssueAPI(
		t.Context(),
		store,
		APISpec{Subject: "operator", Name: "script", Scopes: []string{"read"}},
		start,
	)
	require.NoError(t, err)
	require.Zero(t, apiCred.Idle, "an api credential does not slide")

	now = start.Add(APILifetime - time.Minute)
	_, err = verifier.Verify(t.Context(), apiToken, TransportHTTP)
	require.NoError(t, err)

	now = start.Add(APILifetime + time.Minute)
	_, err = verifier.Verify(t.Context(), apiToken, TransportHTTP)
	require.Error(t, err)
}

// A revoked credential is refused on the next request. This is why the
// credential is opaque and stored, and not self-describing.
func TestRevokeTakesEffectOnTheNextRequest(t *testing.T) {
	t.Parallel()

	store := NewMemoryStore()
	verifier := NewVerifier(store, nil)

	token, cred, err := IssueAPI(
		t.Context(),
		store,
		APISpec{Subject: "operator", Name: "script", Scopes: []string{"read"}},
		time.Now(),
	)
	require.NoError(t, err)

	_, err = verifier.Verify(t.Context(), token, TransportHTTP)
	require.NoError(t, err)

	require.NoError(t, verifier.Revoke(t.Context(), cred.PublicID))

	_, err = verifier.Verify(t.Context(), token, TransportHTTP)
	require.Error(t, err)
	requireIndistinguishableRefusal(t, err)
}

// The store holds only the SHA-256 of a secret half, so a stolen database does
// not yield a usable token.
func TestTheStoreHoldsNoSecret(t *testing.T) {
	t.Parallel()

	store := NewMemoryStore()

	token, cred, err := IssueAPI(
		t.Context(),
		store,
		APISpec{Subject: "operator", Name: "script", Scopes: []string{"read"}},
		time.Now(),
	)
	require.NoError(t, err)

	_, secret, err := Split(token)
	require.NoError(t, err)

	row := store.Rows()[cred.PublicID]
	require.NotContains(t, string(row.Digest[:]), secret)
	require.Equal(t, DigestOf(secret), row.Digest)
	require.NotEqual(t, secret, row.PublicID)
}

// requireIndistinguishableRefusal asserts a refusal is the one refusal, and
// that nothing about its cause reaches a client.
func requireIndistinguishableRefusal(t *testing.T, err error) {
	t.Helper()

	require.True(t, Refused(err), "not the one refusal: %v", err)

	var f *fault.Fault
	require.ErrorAs(t, err, &f)
	require.Equal(t, refusalDetail, f.Message())
	require.True(t, f.Redacted(), "the refusal must not carry its detail to a client")
}

// requireSameRendering asserts two refusals are the same in everything a client
// can see.
func requireSameRendering(t *testing.T, a, b error) {
	t.Helper()

	requireIndistinguishableRefusal(t, a)
	requireIndistinguishableRefusal(t, b)

	var fa, fb *fault.Fault
	require.ErrorAs(t, a, &fa)
	require.ErrorAs(t, b, &fb)

	require.Equal(t, fa.Code(), fb.Code())
	require.Equal(t, fa.Status(), fb.Status())
	require.Equal(t, fa.Title(), fb.Title())
	require.Equal(t, fa.Message(), fb.Message())
	require.Equal(t, fa.Params(), fb.Params())

	// The causes differ. They reach the log and not the client.
	require.NotEqual(t, fa.Unwrap().Error(), fb.Unwrap().Error())
}

// TestACredentialFromAnEarlierGenerationIsRefused is the race a password change
// meets. A login or a mint reads the generation, the change raises it, and then
// the row lands. The row carries the older generation, so it is refused. No
// clock takes part, so a clock that steps back changes nothing.
func TestACredentialFromAnEarlierGenerationIsRefused(t *testing.T) {
	t.Parallel()

	store := NewMemoryStore()
	verifier := NewVerifier(store, nil)
	now := time.Date(2026, time.September, 24, 12, 0, 0, 0, time.UTC)

	checked, err := store.Generation(t.Context(), "ada")
	require.NoError(t, err)

	minter, err := store.Generation(t.Context(), "user:ada")
	require.NoError(t, err)

	store.Bump("ada", "user:ada")

	raced, _, err := IssueAtGeneration(t.Context(), store, "ada", KindSession, now, checked)
	require.NoError(t, err)

	_, err = verifier.Verify(t.Context(), raced, TransportHTTP)
	require.True(t, Refused(err), "a session from before the change was accepted")

	forService, _, err := IssueAPI(t.Context(), store, APISpec{
		Scopes: []string{"administer"}, Subject: "service_account:ci", Name: "ci", MintedBy: "user:ada",
		Fence: &Fence{Minter: minter},
	}, now)
	require.NoError(t, err)

	_, err = verifier.Verify(t.Context(), forService, TransportHTTP)
	require.True(t, Refused(err), "a token the person minted from before the change was accepted")

	current, err := store.Generation(t.Context(), "ada")
	require.NoError(t, err)

	// The clock has stepped back a day, the way a board with no real-time
	// clock starts before its first time sync. The new session still works.
	earlier := now.Add(-24 * time.Hour)
	fresh, _, err := IssueAtGeneration(t.Context(), store, "ada", KindSession, earlier, current)
	require.NoError(t, err)

	_, err = NewVerifier(store, func() time.Time { return earlier }).Verify(t.Context(), fresh, TransportHTTP)
	require.NoError(t, err, "a session from the current generation was refused")

	ended, err := verifier.EndSubjects(t.Context(), "ada", "user:ada")
	require.NoError(t, err)
	require.Equal(t, Ended{Held: []int{1, 0}, MintedForOthers: 1}, ended,
		"the end removed a row of the current generation, or missed an older one")

	_, err = NewVerifier(store, func() time.Time { return earlier }).Verify(t.Context(), fresh, TransportHTTP)
	require.NoError(t, err, "ending the older rows removed the new session")
}

// TestARowPastItsKindsUpperLimitIsRefused covers a row saved before the upper
// limit existed, or written by hand. Its own expiry says a hundred years, and
// the verifier refuses it once the kind's longest life has passed.
func TestARowPastItsKindsUpperLimitIsRefused(t *testing.T) {
	t.Parallel()

	issued := time.Date(2026, time.September, 24, 12, 0, 0, 0, time.UTC)
	farOff := issued.AddDate(100, 0, 0)

	cases := []struct {
		name  string
		kind  Kind
		limit time.Duration
	}{
		{name: "an api credential", kind: KindAPI, limit: APIMaxLifetime},
		{name: "a session", kind: KindSession, limit: SessionLifetime},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			store := NewMemoryStore()
			token, publicID, digest, err := Mint()
			require.NoError(t, err)

			require.NoError(t, store.Save(t.Context(), Credential{
				PublicID: publicID, Subject: "user:ada", Kind: tc.kind, Digest: digest,
				IssuedAt: issued, ExpiresAt: farOff, LastSeen: issued, Scopes: scopesOfKind(tc.kind),
			}))

			within := NewVerifier(store, func() time.Time { return issued.Add(tc.limit - time.Minute) })
			_, err = within.Verify(t.Context(), token, TransportHTTP)
			require.NoError(t, err, "a row inside its kind's upper limit was refused")

			past := NewVerifier(store, func() time.Time { return issued.Add(tc.limit + time.Minute) })
			_, err = past.Verify(t.Context(), token, TransportHTTP)
			require.True(t, Refused(err), "a row past its kind's upper limit was accepted")
		})
	}
}

// The memory store refuses a subject that names nobody, as the database does.
func TestTheMemoryStoreRefusesToEndASubjectThatNamesNobody(t *testing.T) {
	t.Parallel()

	for _, subject := range []string{"", "user:", "service_account:"} {
		_, err := NewMemoryStore().EndSubjects(t.Context(), []string{subject})
		require.ErrorIsf(t, err, ErrInvalidSubject, "ending %q was not refused", subject)

		_, err = NewMemoryPairings().DeletePrincipal(t.Context(), subject)
		require.ErrorIsf(t, err, ErrInvalidSubject, "unpairing %q was not refused", subject)
	}
}

// TestATokenMintedDownAChainEndsWithItsRoot is a token minted through a second
// person. With ada's stolen session an attacker pairs an account to bob, and
// with that token mints for a service account. The token names bob as its
// minter and ada as its root, and ada's password change ends it.
func TestATokenMintedDownAChainEndsWithItsRoot(t *testing.T) {
	t.Parallel()

	store := NewMemoryStore()
	verifier := NewVerifier(store, nil)
	now := time.Date(2026, time.September, 25, 12, 0, 0, 0, time.UTC)

	token, _, err := IssueAPI(t.Context(), store, APISpec{
		Scopes: []string{
			"administer",
		},
		Subject:     "service_account:ci",
		Name:        "ci",
		MintedBy:    "user:bob",
		RootCreator: "user:ada",
	}, now)
	require.NoError(t, err)

	_, err = verifier.Verify(t.Context(), token, TransportHTTP)
	require.NoError(t, err)

	store.Bump("ada", "user:ada")

	_, err = verifier.Verify(t.Context(), token, TransportHTTP)
	require.True(t, Refused(err), "a token whose root changed their password was accepted")

	ended, err := verifier.EndSubjects(t.Context(), "ada", "user:ada")
	require.NoError(t, err)
	require.Equal(t, 1, ended.MintedForOthers, "the end did not remove the token by its root")
}

// issueOfKind mints a credential of any kind that is minted here. An api
// credential takes IssueAPI, which is the only door for that kind.
func issueOfKind(t *testing.T, store Store, kind Kind, now time.Time) (Token, error) {
	t.Helper()

	if kind == KindAPI {
		token, _, err := IssueAPI(t.Context(), store, APISpec{
			Subject: "operator", Name: "script", Scopes: []string{"read"},
		}, now)

		return token, err
	}

	token, _, err := Issue(t.Context(), store, "operator", kind, now)

	return token, err
}

// scopesOfKind is what a row of the kind carries: an api credential always
// has a scope, and no other kind has one.
func scopesOfKind(kind Kind) []string {
	if kind == KindAPI {
		return []string{"read"}
	}

	return nil
}
