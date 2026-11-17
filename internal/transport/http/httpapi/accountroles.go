package httpapi

import (
	"context"
	"slices"

	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpfault"
)

// The role layer as the account listing reports it. It is the org's own
// relation set, highest first, so a row that holds more than one names the
// strongest as its role and keeps the rest under grants.
//
// The order is the graph's own, read through authz.OrgRoles, because the
// operation that writes a role and the listing that renders one must agree
// about which relations are roles and which of them is the strongest.
//
//nolint:gochecknoglobals // the org's role order, read-only after init.
var orgRoles = authz.OrgRoles()

// orgRolesHeld is every org relation each account holds, keyed by the subject
// the listing renders, strongest role first.
//
// It reads the org's grants once and indexes them, rather than asking per
// account: a page listing forty people would otherwise cost forty round trips
// through the graph, and the answer is the same rows either way.
//
// A server with no grant surface answers an empty map. The listing then carries
// no role on any row, which is the truth about a deployment where nothing
// records one.
func orgRolesHeld(ctx context.Context, cfg Config) (map[string][]string, error) {
	if cfg.Grants == nil {
		return map[string][]string{}, nil
	}

	who, err := caller(ctx)
	if err != nil {
		return nil, err
	}

	org, err := authz.NewObject(authz.TypeOrg, bootstrapOrgName)
	if err != nil {
		return nil, httpfault.Unexpected("name this server's org")
	}

	tuples, err := cfg.Grants.Grants(ctx, who, org)
	if err != nil {
		return nil, err
	}

	held := make(map[string][]string, len(tuples))

	for _, tuple := range tuples {
		if !slices.Contains(orgRoles, tuple.Relation()) {
			continue
		}

		subject := tuple.Subject().String()
		held[subject] = append(held[subject], string(tuple.Relation()))
	}

	for subject := range held {
		slices.SortFunc(held[subject], func(a, b string) int {
			return slices.Index(orgRoles, authz.Relation(a)) - slices.Index(orgRoles, authz.Relation(b))
		})
	}

	return held, nil
}
