package authz

import (
	"context"
	"fmt"
)

// This file holds what a camera bound to a printer inherits: the grants copied
// onto its own row when it is attached, and the block that reaches it from the
// printer above.
//
// Every camera is placed in a tenant, so one bound to a printer this server
// has never met is still grantable, and a camera holds its own permissions.
// Attaching it to a printer copies the printer's permissions onto the camera,
// and the camera keeps them as its own. Both give the camera a path that does
// not run through the printer, so an administrator blocked on a printer could
// still reach its camera. BlockedDevices closes that path.

// CopyDeviceGrants copies the role grants written directly on one device onto
// another, once, and reports how many tuples it wrote.
//
// The copy is a write of tuples on the camera's own row at the moment of
// attach, and not a walk of the parent edge at check time. The camera then
// carries permissions of its own, and whoever administers the camera may
// withdraw one of them without touching the printer.
//
// It copies once and never again. A camera that already carries a role tuple
// of its own has been attached before. Copying a second time would bring back
// every grant somebody has since withdrawn from it, and a restart would
// re-grant a person who was removed. The copy is a snapshot, and the parent
// edge carries a grant written on the printer after the attach.
//
// Only the role relations travel. A block is not copied, because it confers no
// standing and because BlockedDevices reads it live. Ownership is not copied,
// because it says whose machine a device is and a camera is not that machine.
// Structure edges are not copied, because the declaration itself writes them.
func (s *Store) CopyDeviceGrants(ctx context.Context, from, to string) (int, error) {
	source, err := NewObject(TypeDevice, from)
	if err != nil {
		return 0, err
	}

	target, err := NewObject(TypeDevice, to)
	if err != nil {
		return 0, err
	}

	held, err := s.roleGrantsOn(ctx, target)
	if err != nil {
		return 0, err
	}

	if len(held) > 0 {
		return 0, nil
	}

	inherited, err := s.roleGrantsOn(ctx, source)
	if err != nil {
		return 0, err
	}

	copied := make([]Tuple, 0, len(inherited))

	for _, grant := range inherited {
		written, tupleErr := NewTuple(target, grant.relation, grant.subject)
		if tupleErr != nil {
			return 0, tupleErr
		}

		copied = append(copied, written)
	}

	if err = s.Write(ctx, copied...); err != nil {
		return 0, err
	}

	return len(copied), nil
}

// roleGrantsOn reads the role tuples written directly on one object.
//
// It reads the tuple table and not the projection, because a copy has to
// reproduce what somebody wrote and not what the compiler resolved. A mask
// carries no relation to write back, and what an org or a group confers on the
// printer is that tree's tenancy and not the printer's own grant.
//
// It takes no caller. Every caller of it is the host reconciling its own
// declaration, which is the same standing DeclareDevice writes under, so
// there is no principal here to check against.
func (s *Store) roleGrantsOn(ctx context.Context, object Object) ([]Tuple, error) {
	rows, err := s.db.QueryContext(ctx, s.q(selectGrantsSQL), object.typ.String(), object.id)
	if err != nil {
		return nil, storeFailure("read the grants to copy", err)
	}

	defer func() { _ = rows.Close() }() // the scan error below is the one worth reporting

	var out []Tuple

	for rows.Next() {
		var objectType, objectID, relation, subjectType, subjectID, subjectRelation string
		if err = rows.Scan(
			&objectType, &objectID, &relation, &subjectType, &subjectID, &subjectRelation,
		); err != nil {
			return nil, storeFailure("scan a grant to copy", err)
		}

		grant, buildErr := storedTuple(objectType, objectID, relation, subjectType, subjectID, subjectRelation)
		if buildErr != nil {
			return nil, fmt.Errorf("read the grants to copy: %w", buildErr)
		}

		if !isRole(grant.relation) {
			continue
		}

		out = append(out, grant)
	}

	if err = rows.Err(); err != nil {
		return nil, storeFailure("read the grants to copy", err)
	}

	return out, nil
}

// isRole reports whether a relation belongs to the role layer. It reads
// directRoles and keeps no list of its own, so a role added there is copied
// too.
func isRole(relation Relation) bool {
	for _, role := range directRoles {
		if role.Relation == relation {
			return true
		}
	}

	return false
}

// BlockedDevices returns every device this subject is blocked on, directly or
// through a group it belongs to.
//
// A block has to reach one hop further than the projection takes it. A camera
// is placed in a tenant and carries grants copied from its printer, so it has
// a path that does not run through the printer. An administrator blocked on a
// printer must not go on watching its camera through that path. The media
// plane reads this set and refuses a camera whose printer blocks the caller.
//
// The projection does not do this. A delegate holding administer on one device
// may write a parent edge. If a parent's deny travelled down that edge inside
// reachDevice, the delegate could lock the org administrator out of every
// device below it, permanently.
// TestLaunderingIsRefusedThroughAGrandparentAndADiamond and
// TestAnOrgAdminBlockedOnAPrinterCanStillHideItsCamera hold the graph to that.
// Reading the block here leaves administration alone: an administrator blocked
// off a printer still administers its camera and can still hide it. Only the
// frames are refused, which is what the block was written for.
//
// The read is two small queries and a walk in memory. Blocks are rare: a
// deployment writes one when somebody is being kept away from a machine, and
// the table holds one row per such decision, not one per grant.
func (s *Store) BlockedDevices(ctx context.Context, subject Subject) (map[string]bool, error) {
	covering, err := s.principalsCovering(ctx, subject)
	if err != nil {
		return nil, err
	}

	rows, err := s.db.QueryContext(ctx, s.q(selectDeviceBlocksSQL))
	if err != nil {
		return nil, storeFailure("read the blocks", err)
	}

	defer func() { _ = rows.Close() }() // the scan error below is the one worth reporting

	blocked := make(map[string]bool)

	for rows.Next() {
		var device, subjectType, subjectID, subjectRelation string
		if err = rows.Scan(&device, &subjectType, &subjectID, &subjectRelation); err != nil {
			return nil, storeFailure("scan a block", err)
		}

		if covering[principal{typ: Type(subjectType), id: subjectID}] &&
			(subjectRelation == "" || Relation(subjectRelation) == RelMember) {
			blocked[device] = true
		}
	}

	if err = rows.Err(); err != nil {
		return nil, storeFailure("read the blocks", err)
	}

	return blocked, nil
}

// principalsCovering is the subject itself and every group it belongs to,
// however deeply nested, as the principals a tuple may name it by.
//
// Membership is walked upward from the subject and not downward from every
// group, because one caller belongs to a handful of groups and a deployment has
// more groups than that. The visited set stops a membership cycle, which is a
// configuration error.
func (s *Store) principalsCovering(ctx context.Context, subject Subject) (map[principal]bool, error) {
	rows, err := s.db.QueryContext(ctx, s.q(selectGroupMembersSQL))
	if err != nil {
		return nil, storeFailure("read the group memberships", err)
	}

	defer func() { _ = rows.Close() }() // the scan error below is the one worth reporting

	// parents maps one principal onto the groups that name it as a member.
	parents := make(map[principal][]principal)

	for rows.Next() {
		var group, memberType, memberID string
		if err = rows.Scan(&group, &memberType, &memberID); err != nil {
			return nil, storeFailure("scan a group membership", err)
		}

		member := principal{typ: Type(memberType), id: memberID}
		parents[member] = append(parents[member], principal{typ: TypeGroup, id: group})
	}

	if err = rows.Err(); err != nil {
		return nil, storeFailure("read the group memberships", err)
	}

	covering := make(map[principal]bool)
	queue := []principal{{typ: subject.typ, id: subject.id}}

	for len(queue) > 0 {
		who := queue[0]
		queue = queue[1:]

		if covering[who] {
			continue
		}

		covering[who] = true
		queue = append(queue, parents[who]...)
	}

	return covering, nil
}

// The reads BlockedDevices takes. Both are whole-table scans of shapes a
// deployment holds few of. Neither is filtered in SQL: a filter on the subject
// would miss a block written on a group the subject belongs to, and resolving
// that in SQL means a recursive query the two engines spell differently.
const (
	selectDeviceBlocksSQL = `SELECT object_id, subject_type, subject_id, subject_relation
		FROM authz_tuple
		WHERE object_type = 'device' AND relation = 'blocked'`

	selectGroupMembersSQL = `SELECT object_id, subject_type, subject_id
		FROM authz_tuple
		WHERE object_type = 'group' AND relation = 'member'`
)
