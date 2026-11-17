package authz

// Whose machine a device is, as the host asks and answers it about its own
// decisions.
//
// The grant surface answers the same question for a caller, and takes a
// caller's standing to do it (Grants, WriteGrant). The functions here take
// none. The host is reading and writing the result of an approval it made on
// the operator's behalf, which is the same standing DeclareDevice and
// grantFirstSight write under. There is no principal here to check against.

import (
	"context"
	"database/sql"
	"fmt"
)

// DeviceOwners is every subject holding owner on one device, in the order the
// grant listing renders, and empty for a device nobody owns.
//
// An approval asks whether the person approving a registration is the person
// whose machine this already is, and this answers it. It reads the relation
// and not a mask, because owner is not a role and confers nothing a mask
// comparison would show (see conferredOnDevice).
func (s *Store) DeviceOwners(ctx context.Context, device string) ([]Subject, error) {
	object, err := NewObject(TypeDevice, device)
	if err != nil {
		return nil, err
	}

	rows, err := s.db.QueryContext(ctx, s.q(selectGrantsSQL), object.typ.String(), object.id)
	if err != nil {
		return nil, storeFailure("read a device's owners", err)
	}

	defer func() { _ = rows.Close() }() // the scan error below is the one worth reporting

	var owners []Subject

	for rows.Next() {
		var objectType, objectID, relation, subjectType, subjectID, subjectRelation string
		if err = rows.Scan(
			&objectType, &objectID, &relation, &subjectType, &subjectID, &subjectRelation,
		); err != nil {
			return nil, storeFailure("scan a device's owners", err)
		}

		if Relation(relation) != RelOwner {
			continue
		}

		grant, buildErr := storedTuple(objectType, objectID, relation, subjectType, subjectID, subjectRelation)
		if buildErr != nil {
			return nil, fmt.Errorf("read a device's owners: %w", buildErr)
		}

		owners = append(owners, grant.subject)
	}

	if err = rows.Err(); err != nil {
		return nil, storeFailure("read a device's owners", err)
	}

	return owners, nil
}

// ClaimDevice writes owner on a device for one subject and reports whether the
// graph did not already hold it.
//
// Ownership implies every dangerous verb of the device's kind, so the grant
// surface does not hand this relation to a caller who lacks that reach
// (impliesDangerousVerbs). It is written here without that check, and only
// here: the machine was approved onto this server by a person, and whoever
// admits a device owns it. On a device nobody owns, only an org admin can
// grant a dangerous verb.
//
// It does not look for an existing owner. The caller reads DeviceOwners first
// and claims only a device nobody owns. Ownership changes by an operator's
// grant or by the transfer an approval decides, and a second claim would hand
// a machine to whoever approved it last.
func (s *Store) ClaimDevice(ctx context.Context, device string, owner Subject) (bool, error) {
	object, err := NewObject(TypeDevice, device)
	if err != nil {
		return false, err
	}

	tuple, err := NewTuple(object, RelOwner, owner)
	if err != nil {
		return false, err
	}

	var wrote bool

	err = s.mutate(ctx, func(ctx context.Context, tx *sql.Tx, moved *scope) (int64, error) {
		moved.moved(object)

		result, execErr := tx.ExecContext(ctx, s.q(insertTupleSQL), tupleArgs(tuple)...)
		if execErr != nil {
			return 0, storeFailure("write a device's owner", execErr)
		}

		written, countErr := result.RowsAffected()
		if countErr != nil {
			return 0, storeFailure("count the device owners written", countErr)
		}

		wrote = written > 0

		return written, nil
	})
	if err != nil {
		return false, err
	}

	return wrote, nil
}
