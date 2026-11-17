package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/prusa3d/prusa-connect/internal/platform/auth"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpfault"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// subjectPairings is the pairing surface as the HTTP layer consumes it, over
// the store the bearer path reads. It is here, as apiCredentials is, because
// the transport may not import the platform group and the platform may not
// name the transport's types.
type subjectPairings struct {
	store auth.Pairings
	// generations reads a principal's current generation, from the credential
	// store that holds the counters.
	generations func(ctx context.Context, subject string) (int64, error)
	now         func() time.Time
}

var _ httpapi.Pairings = subjectPairings{}

// Pair writes one pairing. The HTTP surface has already checked the
// principal. This fills the generations the caller left out and writes the
// row once.
func (p subjectPairings) Pair(ctx context.Context, spec httpapi.NewPairing) (httpapi.Pairing, error) {
	var generation int64

	if spec.Generation != nil {
		generation = *spec.Generation
	} else {
		current, err := p.Fence(ctx, spec.Principal)
		if err != nil {
			return httpapi.Pairing{}, err
		}

		generation = current
	}

	var creatorGeneration int64

	switch {
	case spec.CreatorGeneration != nil:
		creatorGeneration = *spec.CreatorGeneration
	case spec.CreatedBy != "":
		current, err := p.Fence(ctx, spec.CreatedBy)
		if err != nil {
			return httpapi.Pairing{}, err
		}

		creatorGeneration = current
	}

	var rootGeneration int64

	switch {
	case spec.RootGeneration != nil:
		rootGeneration = *spec.RootGeneration
	case spec.RootCreator != "":
		current, err := p.Fence(ctx, spec.RootCreator)
		if err != nil {
			return httpapi.Pairing{}, err
		}

		rootGeneration = current
	}

	row := auth.Pairing{
		RootCreator:       spec.RootCreator,
		RootGeneration:    rootGeneration,
		Provider:          spec.Provider,
		Subject:           spec.Subject,
		Principal:         spec.Principal,
		Label:             spec.Label,
		CreatedAt:         p.now().UTC(),
		Generation:        generation,
		CreatedBy:         spec.CreatedBy,
		CreatorGeneration: creatorGeneration,
	}

	if err := p.store.Save(ctx, row); err != nil {
		return httpapi.Pairing{}, pairingRefusal(err, row)
	}

	return pairingOf(row), nil
}

// Fence reads a principal's current generation.
func (p subjectPairings) Fence(ctx context.Context, principal string) (int64, error) {
	generation, err := p.generations(ctx, principal)
	if err != nil {
		return 0, fmt.Errorf("read the generation of %q: %w", principal, err)
	}

	return generation, nil
}

// List returns the pairings, every provider's or one's.
func (p subjectPairings) List(ctx context.Context, provider string) ([]httpapi.Pairing, error) {
	rows, err := p.store.List(ctx, provider)
	if err != nil {
		return nil, fmt.Errorf("list the pairings: %w", err)
	}

	out := make([]httpapi.Pairing, 0, len(rows))
	for _, row := range rows {
		out = append(out, pairingOf(row))
	}

	return out, nil
}

// Unpair removes one pairing, answering not-found for a subject nobody paired.
func (p subjectPairings) Unpair(ctx context.Context, provider, subject string) error {
	err := p.store.Delete(ctx, provider, subject)

	switch {
	case errors.Is(err, auth.ErrNoPairing):
		return httpapi.UnknownPairing(provider, subject)
	case err != nil:
		return fmt.Errorf("remove the pairing of %s subject %q: %w", provider, subject, err)
	}

	return nil
}

// pairingOf renders a row the way the HTTP surface publishes it.
func pairingOf(row auth.Pairing) httpapi.Pairing {
	return httpapi.Pairing{
		Provider:  row.Provider,
		Subject:   row.Subject,
		Principal: row.Principal,
		Label:     row.Label,
		CreatedAt: row.CreatedAt,
	}
}

// pairingRefusal turns what the store refused into the fault the caller is
// told. A subject already paired and an incomplete row are the caller's; a
// store that could not write is this server's failure and stays an error.
func pairingRefusal(err error, row auth.Pairing) error {
	switch {
	case errors.Is(err, auth.ErrPairingExists):
		return fault.AuthPairingExists.New("that subject is already paired; unpair it first to change whom it acts as").
			With(fault.StringParam("provider", row.Provider), fault.StringParam("subject", row.Subject)).
			Wrap(err)
	case errors.Is(err, auth.ErrInvalidPairing):
		return httpfault.RequestInvalid("body", "provider, subject and principal", err.Error())
	}

	return fmt.Errorf("pair %s subject %q: %w", row.Provider, row.Subject, err)
}
