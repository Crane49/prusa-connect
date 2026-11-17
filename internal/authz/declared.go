package authz

import (
	"context"
	"database/sql"

	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
)

const (
	// One device's tuples of one relation. The declaration owns the structure
	// a device sits in, so a write rewrites it and does not add to it. An edge
	// left behind by a declaration that has changed would be an inheritance
	// nobody declares.
	deleteDeviceRelationSQL = `DELETE FROM authz_tuple
		WHERE object_type = 'device' AND object_id = ? AND relation = ?`
)

// DeclaredDevice is one device a person adds, with the structure it sits in.
// A camera is added this way: it is a device row of its own, optionally
// inside the printer it watches.
//
// It is not DeviceRow. A DeviceRow is what a listing hands back to a caller,
// and neither of the two structure fields here is ever read back through one.
type DeclaredDevice struct {
	// ID is the instance name the operator chose, which is the device id.
	ID string
	// Name is what a listing shows.
	Name string
	// Org is the tenant to place it in. An empty org writes no placement, and
	// a device in no org is reachable only through a grant written on it.
	Org string
	// Parent is the device this one sits inside, or empty for one that sits
	// inside nothing. A camera bound to a printer names that printer here, and
	// whoever may reach the printer then reaches the camera.
	Parent string
}

// DeclareDevice registers one device a person added and rewrites the
// structure it sits in, in one transaction with also, which writes what the
// device runs (ADR-20261116-g5x). also may be nil.
//
// The structure is rewritten and not added to. The declaration says which
// printer a camera watches, so a binding that has changed leaves no edge
// behind. A grant written on the device itself is untouched, so it survives a
// change.
func (s *Store) DeclareDevice(
	ctx context.Context, kind ref.DeviceKind, device DeclaredDevice, also func(context.Context, *sql.Tx) error,
) error {
	if !KnownKind(kind) {
		return invalidID.New("a device is declared under a device kind this build does not carry").
			With(paramDeviceKind(kind))
	}

	row, err := validDeclaration(kind, device)
	if err != nil {
		return err
	}

	return s.mutate(ctx, func(ctx context.Context, tx *sql.Tx, moved *scope) (int64, error) {
		moved.movedDevice(row.row.ID)

		changed, declareErr := s.declareDevice(ctx, tx, kind, row)
		if declareErr != nil {
			return 0, declareErr
		}

		if also != nil {
			if alsoErr := also(ctx, tx); alsoErr != nil {
				return 0, alsoErr
			}
		}

		// The write beside it changed something even when the graph did not.
		return changed + 1, nil
	})
}

// declaredTuples is the structure one declaration asks for, already validated
// through the constructors so that nothing reaches the table another way.
type declaredTuples struct {
	row     DeviceRow
	written []Tuple
}

// validDeclaration is one declaration, through the constructors. Nothing here
// reaches the tuple table another way, so a declaration naming an org outside
// the grammar is a refusal rather than a row the foreign key catches later.
func validDeclaration(kind ref.DeviceKind, device DeclaredDevice) (declaredTuples, error) {
	object, err := NewObject(TypeDevice, device.ID)
	if err != nil {
		return declaredTuples{}, err
	}

	if err = validateDeviceName(device.Name); err != nil {
		return declaredTuples{}, err
	}

	row := declaredTuples{row: DeviceRow{ID: device.ID, Name: device.Name, Kind: kind}}

	for _, edge := range []struct {
		id       string
		typ      Type
		relation Relation
	}{
		{id: device.Org, typ: TypeOrg, relation: RelOrg},
		{id: device.Parent, typ: TypeDevice, relation: RelParent},
	} {
		if edge.id == "" {
			continue
		}

		subject, subjectErr := NewSubject(edge.typ, edge.id)
		if subjectErr != nil {
			return declaredTuples{}, subjectErr
		}

		written, tupleErr := NewTuple(object, edge.relation, subject)
		if tupleErr != nil {
			return declaredTuples{}, tupleErr
		}

		row.written = append(row.written, written)
	}

	return row, nil
}

// declareDevice registers one device and rewrites the structure it declares.
func (s *Store) declareDevice(
	ctx context.Context, tx *sql.Tx, kind ref.DeviceKind, row declaredTuples,
) (int64, error) {
	if err := s.holdKind(ctx, tx, row.row); err != nil {
		return 0, err
	}

	var changed int64

	result, err := tx.ExecContext(
		ctx, s.q(upsertDeviceSQL),
		row.row.ID, row.row.Name, sortKey(row.row.Name), kind.String(),
	)
	if err != nil {
		return 0, storeFailure("register a declared device", err)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return 0, storeFailure("count registered devices", err)
	}

	changed += affected

	// The two structure relations are cleared and rewritten together, so a
	// binding removed from the configuration is removed from the graph. Only
	// the relations a declaration owns are touched: a role, a block and an
	// owner written on this device are somebody's administrative act and
	// survive every reload.
	for _, relation := range []Relation{RelOrg, RelParent} {
		cleared, clearErr := s.clearDeviceRelation(ctx, tx, row.row.ID, relation)
		if clearErr != nil {
			return 0, clearErr
		}

		changed += cleared
	}

	for _, tuple := range row.written {
		result, err = tx.ExecContext(ctx, s.q(insertTupleSQL), tupleArgs(tuple)...)
		if err != nil {
			return 0, storeFailure("bind a declared device", err)
		}

		affected, err = result.RowsAffected()
		if err != nil {
			return 0, storeFailure("count bound devices", err)
		}

		changed += affected
	}

	return changed, nil
}

// clearDeviceRelation removes every tuple of one relation written on a device.
func (s *Store) clearDeviceRelation(
	ctx context.Context, tx *sql.Tx, id string, relation Relation,
) (int64, error) {
	result, err := tx.ExecContext(ctx, s.q(deleteDeviceRelationSQL), id, relation.String())
	if err != nil {
		return 0, storeFailure("clear a declared device's structure", err)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return 0, storeFailure("count cleared edges", err)
	}

	return affected, nil
}
