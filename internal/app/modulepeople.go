package app

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"

	"github.com/prusa3d/prusa-connect/internal/app/resource"
	"github.com/prusa3d/prusa-connect/internal/app/resourcehost"
	"github.com/prusa3d/prusa-connect/internal/platform/auth/local"
	"github.com/prusa3d/prusa-connect/sdk/accessapi"
)

// modulePeople is the list of people the supervision plane holds a module's
// questions to. It is a late binding: the plane starts the modules before the
// identity layer that owns the accounts exists, and until then it keeps
// nobody.
type modulePeople struct {
	source atomic.Pointer[accountPeople]
}

var _ resourcehost.People = (*modulePeople)(nil)

// bind sets the list. Nil is a deployment that keeps none.
func (m *modulePeople) bind(people *accountPeople) { m.source.Store(people) }

// Kept reports whether a list is bound and keeps people.
func (m *modulePeople) Kept() bool {
	people := m.source.Load()

	return people != nil && people.kept()
}

// Holds answers from the bound list, and false when none is bound.
func (m *modulePeople) Holds(ctx context.Context, principal string) (bool, error) {
	people := m.source.Load()
	if people == nil {
		return false, nil
	}

	return people.holds(ctx, principal)
}

// List answers every person the bound list keeps, and nobody when no list is
// bound or the bound one keeps no people.
func (m *modulePeople) List(ctx context.Context) ([]accessapi.Person, error) {
	people := m.source.Load()
	if people == nil || !people.kept() {
		return nil, nil
	}

	return people.list(ctx)
}

// accountReader is the two reads of the account store this needs.
type accountReader interface {
	Get(ctx context.Context, id string) (local.Account, error)
	List(ctx context.Context) ([]local.Account, error)
}

// accountPeople answers from this server's local accounts. A person is one
// account row.
type accountPeople struct {
	accounts accountReader
	// mode is the first-run decision on an install that had taken none when
	// it started, and nil on every other. Once it records open mode, every
	// request is served as one configured principal, and the accounts do not
	// say who is a person.
	mode *setupMode
}

// kept reports whether the accounts say who is a person.
func (p *accountPeople) kept() bool {
	return p.mode == nil || p.mode.get() != resource.SetupOpen
}

// holds answers whether the principal is a person with an account here.
func (p *accountPeople) holds(ctx context.Context, principal string) (bool, error) {
	id, isPerson := resourcehost.PersonID(principal)
	if !isPerson {
		return false, nil
	}

	_, err := p.accounts.Get(ctx, id)

	switch {
	case errors.Is(err, local.ErrNoAccount):
		return false, nil
	case err != nil:
		return false, fmt.Errorf("read the account of %s: %w", principal, err)
	}

	return true, nil
}

// list answers one row per account: the subject the graph is keyed by, and the
// display name, or the username when the account has none. Nothing else of
// the account leaves this function.
func (p *accountPeople) list(ctx context.Context) ([]accessapi.Person, error) {
	accounts, err := p.accounts.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list the accounts: %w", err)
	}

	out := make([]accessapi.Person, 0, len(accounts))

	for _, account := range accounts {
		name := account.DisplayName
		if name == "" {
			name = account.Username
		}

		out = append(out, accessapi.Person{
			Subject: subjectOf(account.ID), Name: name, Kind: accessapi.KindUser,
		})
	}

	return out, nil
}
