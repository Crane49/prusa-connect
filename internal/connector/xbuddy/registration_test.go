package xbuddy

import (
	"errors"
	"strings"
	"testing"
)

// The four admission modes of earlier research, each in both directions: a
// mode with a permit test and no refusal test is half a test.
func TestTheFourAdmissionModes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		mode        Mode
		fingerprint string
		useToken    bool
		token       string
		admitted    bool
	}{
		{name: "none takes any printer", mode: ModeNone, fingerprint: "FP-1", admitted: true},
		{name: "none refuses a printer with no fingerprint at all", mode: ModeNone},

		{name: "token takes the token it issued", mode: ModeToken, fingerprint: "FP-1", useToken: true, admitted: true},
		{name: "token refuses no token", mode: ModeToken, fingerprint: "FP-1"},
		{name: "token refuses a token it did not issue", mode: ModeToken, fingerprint: "FP-1", token: "MADE-UP"},

		{name: "fingerprint takes one it approved", mode: ModeFingerprint, fingerprint: "FP-1", admitted: true},
		{name: "fingerprint refuses one it never saw", mode: ModeFingerprint, fingerprint: "FP-OTHER"},
		{name: "fingerprint refuses none at all", mode: ModeFingerprint},

		{name: "both takes both", mode: ModeBoth, fingerprint: "FP-1", useToken: true, admitted: true},
		{name: "both refuses a right fingerprint with no token", mode: ModeBoth, fingerprint: "FP-1"},
		{name: "both refuses a right token under the wrong fingerprint",
			mode: ModeBoth, fingerprint: "FP-OTHER", useToken: true},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			registry := newRegistrar(testCase.mode, defaultMaxRegistrations)

			code, _, err := registry.begin(t.Context(), Registration{Fingerprint: "FP-1", Serial: "SN-1"})
			if err != nil {
				t.Fatalf("begin: %v", err)
			}

			// Only mode none approves on arrival. This case is about the
			// credential each mode checks and not about who decided, so the
			// operator's half is done here for every one of them.
			if _, err = registry.Approve(t.Context(), code, "user:tester"); err != nil {
				t.Fatalf("approve: %v", err)
			}

			issued, err := registry.complete(t.Context(), code)
			if err != nil {
				t.Fatalf("complete: %v", err)
			}

			token := testCase.token
			if testCase.useToken {
				token = issued.Token
			}

			known, err := registry.authenticate(testCase.fingerprint, token)

			switch {
			case testCase.admitted && err != nil:
				t.Fatalf("the printer was refused: %v", err)
			case !testCase.admitted && err == nil:
				t.Fatalf("the printer was admitted as %q", known)
			case !testCase.admitted:
				if !errors.Is(err, ErrRefused) {
					t.Errorf("the refusal is %v, and callers match on ErrRefused", err)
				}

				return
			}

			if known != testCase.fingerprint {
				t.Errorf("the printer is known as %q, want %q", known, testCase.fingerprint)
			}
		})
	}
}

// A printer that retries registration gets the code it already has. Piling up
// codes for one printer is an operator reading the wrong one out.
func TestARetriedRegistrationKeepsItsCode(t *testing.T) {
	t.Parallel()

	registry := newRegistrar(ModeToken, defaultMaxRegistrations)

	first, _, err := registry.begin(t.Context(), Registration{Fingerprint: "FP-1", Firmware: "6.4.0"})
	if err != nil {
		t.Fatalf("begin: %v", err)
	}

	second, _, err := registry.begin(t.Context(), Registration{Fingerprint: "FP-1", Firmware: "6.5.0"})
	if err != nil {
		t.Fatalf("begin again: %v", err)
	}

	if first != second {
		t.Errorf("a retry issued %q on top of %q", second, first)
	}

	if _, err = registry.complete(t.Context(), first); !errors.Is(err, ErrRegistrationPending) {
		t.Errorf("an unapproved registration answers %v, and the printer polls on ErrRegistrationPending", err)
	}

	approved, err := registry.Approve(t.Context(), first, "user:tester")
	if err != nil {
		t.Fatalf("approve: %v", err)
	}

	// The retry carried newer firmware, and that is what the operator approved.
	if approved.Firmware != "6.5.0" {
		t.Errorf("the approved registration reports firmware %q, want %q", approved.Firmware, "6.5.0")
	}

	if _, err = registry.complete(t.Context(), first); err != nil {
		t.Errorf("an approved registration answers %v", err)
	}
}

// A code is drawn from the alphabet an earlier frontend accepted from an operator
// typing it, which excludes every character pair a reader confuses.
func TestARegistrationCodeIsReadableAloud(t *testing.T) {
	t.Parallel()

	for range 64 {
		code, err := randomCode()
		if err != nil {
			t.Fatalf("randomCode: %v", err)
		}

		if len(code) != codeLength {
			t.Fatalf("the code %q is %d characters, want %d", code, len(code), codeLength)
		}

		if index := strings.IndexFunc(code, func(r rune) bool {
			return !strings.ContainsRune(codeAlphabet, r)
		}); index >= 0 {
			t.Fatalf("the code %q carries %q, which is not in the alphabet", code, code[index])
		}
	}
}

// TestRejectingARegistrationTakesEveryCredentialWithIt is the operator's other
// half. Rejecting has to revoke as well as forget: a registration approved by
// mistake writes the printer's token and fingerprint into the allowlists the
// websocket reads, and a reject that only dropped the row would leave a machine
// that still authenticates.
func TestRejectingARegistrationTakesEveryCredentialWithIt(t *testing.T) {
	t.Parallel()

	registry := newRegistrar(ModeBoth, 0)

	code, _, err := registry.begin(t.Context(), Registration{Fingerprint: "FP-1", Serial: "SN-1"})
	if err != nil {
		t.Fatalf("begin: %v", err)
	}

	if _, err = registry.Approve(t.Context(), code, "user:tester"); err != nil {
		t.Fatalf("approve: %v", err)
	}

	approved, err := registry.complete(t.Context(), code)
	if err != nil {
		t.Fatalf("complete: %v", err)
	}

	if _, err = registry.authenticate("FP-1", approved.Token); err != nil {
		t.Fatalf("an approved printer is refused before the reject: %v", err)
	}

	if err = registry.Reject(t.Context(), code); err != nil {
		t.Fatalf("reject: %v", err)
	}

	if _, err = registry.authenticate("FP-1", approved.Token); !errors.Is(err, ErrRefused) {
		t.Errorf("a rejected printer authenticates with %v, want ErrRefused", err)
	}

	if _, err = registry.complete(t.Context(), code); !errors.Is(err, ErrUnknownCode) {
		t.Errorf("a rejected code polls with %v, want ErrUnknownCode", err)
	}

	if len(registry.pending()) != 0 {
		t.Errorf("%d registrations are still waiting, want 0", len(registry.pending()))
	}
}

// TestRejectingACodeNobodyIssuedIsRefused holds the other direction. A code the
// table does not carry and a code somebody already rejected are one answer, so
// nothing here tells a caller which codes this server has seen.
func TestRejectingACodeNobodyIssuedIsRefused(t *testing.T) {
	t.Parallel()

	registry := newRegistrar(ModeToken, 0)

	if err := registry.Reject(t.Context(), "NOSUCHCODE"); !errors.Is(err, ErrUnknownCode) {
		t.Errorf("rejecting an unknown code answered %v, want ErrUnknownCode", err)
	}
}

// TestThePendingListingIsInCodeOrder holds what a person reading it twice sees.
// The table is a map, so an unsorted walk gives two readings of one table in two
// orders, and a client paging the second one skips a row.
func TestThePendingListingIsInCodeOrder(t *testing.T) {
	t.Parallel()

	registry := newRegistrar(ModeToken, 0)

	for _, fingerprint := range []string{"FP-1", "FP-2", "FP-3", "FP-4", "FP-5"} {
		if _, _, err := registry.begin(t.Context(), Registration{Fingerprint: fingerprint}); err != nil {
			t.Fatalf("begin %s: %v", fingerprint, err)
		}
	}

	first := registry.pending()
	if len(first) != 5 {
		t.Fatalf("%d registrations are waiting, want 5", len(first))
	}

	for index := 1; index < len(first); index++ {
		if first[index-1].Code >= first[index].Code {
			t.Fatalf("the listing is not in code order: %q then %q",
				first[index-1].Code, first[index].Code)
		}
	}

	for range 8 {
		again := registry.pending()

		for index := range first {
			if again[index].Code != first[index].Code {
				t.Fatalf("two readings of one table disagree at %d: %q then %q",
					index, first[index].Code, again[index].Code)
			}
		}
	}
}
