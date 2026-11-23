// Package account answers the one request PrusaSlicer 3 makes to Prusa
// Account once it holds a credential: the me endpoint, with the body Prusa
// Account answers with.
//
// PrusaSlicer gets its credential from this server's app pairing, not from
// this module. It asks the core for a code, the person approves the code on
// this server's /pair page, and the slicer collects the credential
// (ADR-20261025-tpp, ADR-20261112-nay). It then sends the credential as a
// bearer token, and the host resolves the person before this package runs.
package account

import (
	"context"
	"encoding/json"
	"hash/fnv"
	"math"
	"net/http"
	"strings"
)

// Person is one person the host holds, as the me endpoint needs them.
type Person struct {
	Subject string
	Name    string
}

// People is the host's directory of people.
type People interface {
	People(ctx context.Context) ([]Person, error)
}

// Me answers the me endpoint for the principal the host resolved from the
// request's credential. PrusaSlicer needs public_username and reads id, email
// and avatar_small from the reply. avatar_small is left out when
// the name holds no letter to draw.
func Me(w http.ResponseWriter, r *http.Request, principal string, people People) {
	if principal == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{
			"detail": "Authentication credentials were not provided.",
		})

		return
	}

	name := nameOf(r.Context(), principal, people)

	body := map[string]any{
		"id":              numericID(principal),
		"public_username": name,
		"first_name":      name,
		"last_name":       "",
	}

	// A relative reference, which PrusaSlicer resolves against this route's
	// address. See avatar.go.
	if avatar := avatarRefFor(name, principal); avatar != "" {
		body["avatar_small"] = avatar
	}

	writeJSON(w, http.StatusOK, body)
}

// nameOf is the name the host holds for principal, and the principal's own
// id when the host has none.
func nameOf(ctx context.Context, principal string, people People) string {
	fallback := strings.TrimPrefix(principal, "user:")
	if people == nil {
		return fallback
	}

	all, err := people.People(ctx)
	if err != nil {
		return fallback
	}

	for _, person := range all {
		if person.Subject == principal || strings.TrimPrefix(person.Subject, "user:") == fallback {
			if person.Name != "" {
				return person.Name
			}
		}
	}

	return fallback
}

// numericID is a stable number for a principal. Prusa Account answers a
// number, and PrusaSlicer keeps it as the user id.
func numericID(principal string) uint32 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(principal))

	return h.Sum32() & math.MaxInt32
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
