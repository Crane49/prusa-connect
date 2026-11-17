package authz

import (
	"context"
	"database/sql"
)

// This file holds one rule, read from two places.
//
// The dangerous tier is reached by a row naming a subject, a device and a
// verb, and by ownership of the device. Ownership implies every dangerous verb
// of the device's kind, and that is computed and not stored. Neither is a bit
// in a projected mask, so a comparison of masks gets two cases wrong:
//
//   - A device manager writes themselves `device#owner`. conferred(device,
//     owner) is ownerMask, and ownerMask is inside the manager mask they
//     already hold, so the mask comparison passes. But the tuple also hands
//     out every dangerous verb of that device's kind, and the mask cannot say
//     so.
//   - The first dangerous-tier row on a device. The write takes administer and
//     reach of the verb. Ownership gives the reach and not administer. A
//     manager and an org admin give administer and not the reach.
//
// The rule covers both: the dangerous tier of a device belongs to whoever owns
// the device, and to whoever administers an org the device is in. An owner may
// hand the tier out on their own machine. An org admin may hand it out on any
// machine of their org. A manager who is neither may not: a manager is
// delegated the day-to-day of a machine somebody else owns, and no role
// confers a dangerous verb.

// standsOverDangerousTier reports whether who may hand out the dangerous verbs
// of one device: the device's owner, or an admin of an org the device is in.
//
// It reads the compiled graph and not a projection, for the reason
// administersOrg gives: an org holds no actions, so no projected row says who
// administers one, and ownership is a tuple on the device and not a bit in the
// mask.
func (s *Store) standsOverDangerousTier(
	ctx context.Context, q queryer, who Subject, device Object,
) (bool, error) {
	if err := asks(who); err != nil {
		return false, err
	}

	if device.typ != TypeDevice {
		return false, nil
	}

	tuples, err := loadTuples(ctx, q, s.dialect)
	if err != nil {
		return false, err
	}

	return standsOverDevice(indexTuples(tuples), who, device.id), nil
}

// standsOverDevice is the rule itself, over an already compiled graph, so a
// caller that has loaded the tuples for something else does not load them
// twice.
func standsOverDevice(g *graph, who Subject, device string) bool {
	if ownsDevice(g, who, device) {
		return true
	}

	for _, org := range g.orgsOf(device) {
		if administersOrg(g, who, Object{typ: TypeOrg, id: org}) {
			return true
		}
	}

	return false
}

// ownsDevice reports whether who holds the owner relation on the device,
// directly or through a group they are a member of. The expansion is the same
// one the projection makes for an owner tuple, so a membership change reaches
// this the moment it reaches everything else.
func ownsDevice(g *graph, who Subject, device string) bool {
	target := principal{typ: who.typ, id: who.id}
	held := false

	for _, owner := range g.owners[device] {
		g.expand(owner, func(reached principal) {
			if reached == target {
				held = true
			}
		})
	}

	return held
}

// impliesDangerousVerbs reports whether writing this relation on this object
// type hands out reach the action mask does not carry.
//
// Ownership is the only relation that does. It is named here and not tested
// inline, so that a second one added later has a place to be declared.
// escalates compares masks, and the mask comparison cannot see what a relation
// named here confers.
func impliesDangerousVerbs(objectType Type, relation Relation) bool {
	return objectType == TypeDevice && relation == RelOwner
}

// reachRefusal is what a caller is told when a grant would confer reach they do
// not hold themselves. It is the conferral rule applied to the dangerous tier
// and not to the mask, so both refusals say the same thing.
const reachRefusal = "a grant may not confer reach the caller does not hold. Ownership of a " +
	"device implies every dangerous verb of its kind, so writing it takes " +
	"either ownership of that device or admin of an org it is in"

// dangerousTierRefusal is what a caller is told when they may not write a row
// of the dangerous tier on a device.
const dangerousTierRefusal = "a dangerous verb is conferred by no class, role or level, so a " +
	"caller confers one only where they already hold it. Ownership of the device implies every " +
	"dangerous verb of its kind, and an admin of an org the device is in may " +
	"hand the tier out on it"

// verbGrantStanding decides who may write or withdraw a dangerous-tier row on
// one device. It reports true for a caller who stands over the tier, and false
// for a caller who only administers the object.
//
// A caller needs one of the two. A delegated manager administers the device,
// and that carries no reach: such a caller still has to hold the verb
// themselves. An owner or an org admin stands over the tier, and that carries
// the reach. The first row on a device is written this way.
func (s *Store) verbGrantStanding(
	ctx context.Context, tx *sql.Tx, who Subject, object Object,
) (bool, error) {
	stands, err := s.standsOverDangerousTier(ctx, tx, who, object)
	if err != nil {
		return false, err
	}

	if stands {
		return true, nil
	}

	if _, err = s.mayAdminister(ctx, tx, who, object); err != nil {
		return false, err
	}

	return false, nil
}
