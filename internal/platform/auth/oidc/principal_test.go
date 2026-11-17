package oidc

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
)

const authentikIssuer = "http://127.0.0.1:58190/application/o/prusa-connect/"

// A subject that already is a valid id keeps it, so a person who signed in
// before ids were derived still holds every grant written for them.
func TestPrincipalIDKeepsASubjectThatIsAlreadyAnID(t *testing.T) {
	t.Parallel()

	for _, subject := range []string{
		"6c1f0b2a-5d4e-4f8a-9b3c-2e1d0a9f8e71", // Keycloak, a UUID
		"110248495921238986420",                // Google, a number
		"ada",
		strings.Repeat("a", 63),
	} {
		require.Equal(t, subject, PrincipalID("https://issuer.example", subject), subject)
	}
}

// The defaults of real providers that the id grammar refuses. Each one maps to
// an id the graph takes.
func TestPrincipalIDDerivesAnIDForASubjectTheGraphCannotName(t *testing.T) {
	t.Parallel()

	for _, subject := range []string{
		strings.Repeat("0123456789abcdef", 4),                          // authentik: the hashed user id
		"CiQwOGE4Njg0Yi1kYjg4LTRiNzMtOTBhOS0zY2QxNjYxZjU0NjYSBWxvY2Fs", // Dex
		"auth0|5f7c8ec7c33c6c004bbafe82",                               // Auth0
		"6C1F0B2A-5D4E-4F8A-9B3C-2E1D0A9F8E71",                         // a UUID in capitals
		"-starts-with-a-dash",
		strings.Repeat("a", 64),
	} {
		id := PrincipalID("https://issuer.example", subject)

		require.True(t, strings.HasPrefix(id, DerivedPrefix), id)
		require.Len(t, id, len(DerivedPrefix)+derivedChars, id)
		require.NoError(t, ref.ValidateID(id), subject)
	}
}

// The values come from Python's hashlib and base64, not from this package, so
// a change to the derivation that would move every derived person's grants
// fails here.
func TestPrincipalIDIsStable(t *testing.T) {
	t.Parallel()

	require.Equal(t, "oidc-wyj7c7bxbvgxunmhhus7ehtwaoxuz7r3",
		PrincipalID(authentikIssuer, strings.Repeat("a", 64)))
	require.Equal(t, "oidc-li22zny5qfz4bispgm2cpzu73gu76qru",
		PrincipalID("https://example.eu.auth0.com", "auth0|5f7c8ec7c33c6c004bbafe82"))
}

// A trailing slash is the same issuer. Another issuer is another person, even
// under the same subject.
func TestPrincipalIDTiesTheIDToTheIssuer(t *testing.T) {
	t.Parallel()

	subject := "auth0|5f7c8ec7c33c6c004bbafe82"

	require.Equal(t,
		PrincipalID("https://example.eu.auth0.com/", subject),
		PrincipalID("https://example.eu.auth0.com", subject))
	require.NotEqual(t,
		PrincipalID("https://example.eu.auth0.com", subject),
		PrincipalID("https://other.eu.auth0.com", subject))
}

// A subject a person can choose at some providers, such as a username, must
// not be kept as an id another person's subject derives to. So a subject that
// starts with the prefix is derived too, even when it fits the grammar.
func TestPrincipalIDNeverKeepsASubjectThatLooksDerived(t *testing.T) {
	t.Parallel()

	victim := PrincipalID(authentikIssuer, strings.Repeat("a", 64))

	forged := PrincipalID(authentikIssuer, victim)

	require.NotEqual(t, victim, forged)
	require.True(t, strings.HasPrefix(forged, DerivedPrefix))
}

func TestCheckSubject(t *testing.T) {
	t.Parallel()

	for _, subject := range []string{
		"6c1f0b2a-5d4e-4f8a-9b3c-2e1d0a9f8e71",
		"auth0|5f7c8ec7c33c6c004bbafe82",
		strings.Repeat("x", maxSubjectBytes),
	} {
		require.NoError(t, CheckSubject(subject), subject)
	}

	for _, subject := range []string{
		"",
		strings.Repeat("x", maxSubjectBytes+1),
		"two words",
		"line\nbreak",
		"tab\t",
		"del\x7f",
	} {
		require.ErrorIs(t, CheckSubject(subject), ErrSubject, subject)
	}
}
