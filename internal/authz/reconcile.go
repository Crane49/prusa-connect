package authz

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strings"
)

const (
	insertTypeSQL  = `INSERT INTO authz_type (name) VALUES (?) ON CONFLICT DO NOTHING`
	selectTypesSQL = `SELECT name FROM authz_type`
	deleteTypeSQL  = `DELETE FROM authz_type WHERE name = ?`

	insertActionSQL  = `INSERT INTO authz_action (name) VALUES (?) ON CONFLICT DO NOTHING`
	selectActionsSQL = `SELECT name FROM authz_action`
	deleteActionSQL  = `DELETE FROM authz_action WHERE name = ?`

	insertShapeSQL = `INSERT INTO authz_relation_shape
		(object_type, relation, subject_type, subject_relation)
		VALUES (?, ?, ?, ?)
		ON CONFLICT DO NOTHING`

	selectShapesSQL = `SELECT object_type, relation, subject_type, subject_relation
		FROM authz_relation_shape`

	deleteShapeSQL = `DELETE FROM authz_relation_shape
		WHERE object_type = ? AND relation = ? AND subject_type = ? AND subject_relation = ?`

	countShapeTuplesSQL = `SELECT COUNT(*) FROM authz_tuple
		WHERE object_type = ? AND relation = ? AND subject_type = ? AND subject_relation = ?`

	// listShapeTuplesSQL is never executed. It is the query the refusal below
	// prints, so an operator can see the rows that stopped the start without
	// reconstructing it from the schema.
	listShapeTuplesSQL = `SELECT object_type, object_id, relation, subject_type, subject_id, subject_relation ` +
		`FROM authz_tuple WHERE object_type = ? AND relation = ? AND subject_type = ? AND subject_relation = ?`
)

// Reconcile writes the Go vocabulary into the lookup tables that authz_tuple's
// composite foreign key points at, and removes what the vocabulary no longer
// declares. Run it after Migrate and before anything binds a port: until it has
// run, the database knows no legal shape and refuses every tuple.
//
// Adding a type, a relation or an action is an INSERT here and not a schema
// change. That is why the vocabulary is rows and not a CHECK. Removing one
// narrows the graph, so it is refused while any tuple still names it. A
// permission model that stops recognising an edge without saying so fails
// where nobody sees it.
//
// It then recompiles the projection. The projection is a function of the tuples
// and of this binary's vocabulary, and only the tuples are in the database. A
// binary that moved an action between role levels reads rows compiled under the
// vocabulary of the binary before it. Nothing else recompiles them all: a
// mutation recompiles what it moved, and an upgrade that grants nothing moves
// nothing. A manager who could flash firmware before the upgrade would be
// refused until the deployment's next grant write. The cost is one rebuild of
// a graph that is a few hundred kilobytes at fleet scale (see reproject).
func (s *Store) Reconcile(ctx context.Context) error {
	if err := s.reconcile(ctx, Types(), Actions(), relationSpecs); err != nil {
		return err
	}

	return s.Rebuild(ctx)
}

// reconcile takes the vocabulary rather than reading the package variables, so
// a test can reconcile a narrowed one without mutating global state.
func (s *Store) reconcile(ctx context.Context, types []Type, actions []Action, specs []relationSpec) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return storeFailure("begin reconcile", err)
	}

	// Rollback after a commit is a no-op, and its error on the failure path
	// says nothing the returned error does not already say.
	defer func() { _ = tx.Rollback() }()

	if err = s.writeVocabulary(ctx, tx, types, actions, specs); err != nil {
		return err
	}

	if err = s.retireVocabulary(ctx, tx, types, actions, specs); err != nil {
		return err
	}

	if err = tx.Commit(); err != nil {
		return storeFailure("commit reconcile", err)
	}

	return nil
}

// writeVocabulary inserts what the Go vocabulary declares. Types come first:
// a relation shape names two of them and its own foreign key says so.
func (s *Store) writeVocabulary(
	ctx context.Context, tx *sql.Tx, types []Type, actions []Action, specs []relationSpec,
) error {
	for _, typ := range types {
		if _, err := tx.ExecContext(ctx, s.q(insertTypeSQL), typ.String()); err != nil {
			return storeFailure("write type "+typ.String(), err)
		}
	}

	for _, action := range actions {
		if _, err := tx.ExecContext(ctx, s.q(insertActionSQL), action.String()); err != nil {
			return storeFailure("write action "+action.String(), err)
		}
	}

	for _, shape := range expandShapes(specs) {
		if _, err := tx.ExecContext(ctx, s.q(insertShapeSQL), shape.args()...); err != nil {
			return storeFailure("write relation shape "+shape.String(), err)
		}
	}

	return nil
}

// retireVocabulary removes rows the Go vocabulary no longer declares. Shapes go
// first: a type is only retirable once nothing names it, and the shape table's
// own foreign key is what says so.
func (s *Store) retireVocabulary(
	ctx context.Context, tx *sql.Tx, types []Type, actions []Action, specs []relationSpec,
) error {
	if err := s.retireShapes(ctx, tx, expandShapes(specs)); err != nil {
		return err
	}

	keepTypes := make([]string, 0, len(types))
	for _, typ := range types {
		keepTypes = append(keepTypes, typ.String())
	}

	if err := s.retireNames(ctx, tx, nameTable{
		what:      "type",
		selectSQL: selectTypesSQL,
		deleteSQL: deleteTypeSQL,
	}, keepTypes); err != nil {
		return err
	}

	keepActions := make([]string, 0, len(actions))
	for _, action := range actions {
		keepActions = append(keepActions, action.String())
	}

	return s.retireNames(ctx, tx, nameTable{
		what:      "action",
		selectSQL: selectActionsSQL,
		deleteSQL: deleteActionSQL,
		dependsOn: s.actionDependants,
	}, keepActions)
}

// actionDependants names what still depends on a core action the vocabulary
// has dropped. It is the reason retireNames has a dependency hook.
//
// An action appears in no tuple, so no foreign key covers it and there is no
// row to count. Its dependant is the compiled vocabulary itself. An action
// Actions no longer declares but Action.mask still answers a bit for is two
// lists that disagree. Retiring the row without removing the bit would leave
// the database saying the action is gone while every projected row still
// carries it, and the next recompile would write the bit back.
//
// It is a refusal and not a revocation. A revocation would decide on an
// operator's behalf that a permission they granted confers less.
//
// A verb grant is not a dependant. It names a verb, a verb carries a class,
// and a class is a core action with a compiled bit, so no action retirement
// touches it.
func (s *Store) actionDependants(_ context.Context, _ *sql.Tx, name string) (string, error) {
	if mask := Action(name).mask(); mask != 0 {
		return fmt.Sprintf(
			"this build still compiles it to bit %d. Actions and Action.mask are two lists and this "+
				"is them disagreeing: remove the case from mask, the level masks and the two role "+
				"tables in the same change, or the next recompile writes the bit back into every "+
				"projected row", mask,
		), nil
	}

	return "", nil
}

// retireShapes deletes every stored shape the vocabulary has dropped, after
// proving no tuple depends on one. The foreign key would refuse the delete on
// its own; the count exists so the refusal names what is in the way rather than
// returning a driver's constraint message.
func (s *Store) retireShapes(ctx context.Context, tx *sql.Tx, wanted []relationShape) error {
	stored, err := s.storedShapes(ctx, tx)
	if err != nil {
		return err
	}

	keep := make(map[relationShape]bool, len(wanted))
	for _, shape := range wanted {
		keep[shape] = true
	}

	var stale []relationShape

	for _, shape := range stored {
		if !keep[shape] {
			stale = append(stale, shape)
		}
	}

	if len(stale) == 0 {
		return nil
	}

	if err = s.refuseRetiringUsedShapes(ctx, tx, stale); err != nil {
		return err
	}

	for _, shape := range stale {
		if _, err = tx.ExecContext(ctx, s.q(deleteShapeSQL), shape.args()...); err != nil {
			return storeFailure("retire relation shape "+shape.String(), err)
		}
	}

	return nil
}

// refuseRetiringUsedShapes stops the start. A vocabulary that has lost a shape
// still named by stored tuples cannot be reconciled without either deleting
// authorization data or leaving the database disagreeing with the code, so the
// process stops here and binds no port.
func (s *Store) refuseRetiringUsedShapes(ctx context.Context, tx *sql.Tx, stale []relationShape) error {
	var (
		blocked []string
		total   int64
	)

	for _, shape := range stale {
		var count int64
		if err := tx.QueryRowContext(ctx, s.q(countShapeTuplesSQL), shape.args()...).Scan(&count); err != nil {
			return storeFailure("count tuples of "+shape.String(), err)
		}

		if count == 0 {
			continue
		}

		total += count

		blocked = append(blocked, fmt.Sprintf("%s (%d)", shape, count))
	}

	if len(blocked) == 0 {
		return nil
	}

	return invalidStoredTuple.New(fmt.Sprintf(
		"the vocabulary no longer declares %d relation shape(s), and %d stored tuple(s) still name them: %s; "+
			"list them with %q bound to the four names of each shape",
		len(blocked), total, strings.Join(blocked, ", "), s.q(listShapeTuplesSQL),
	))
}

// nameTable is one single-column lookup table, named so the two of them share
// one retirement path instead of two that can drift.
type nameTable struct {
	what      string
	selectSQL string
	deleteSQL string
	// dependsOn names what still depends on a stored name the vocabulary has
	// dropped, or the empty string when nothing does. A nil hook means the
	// table's own foreign keys are the whole closure, which is what a type has:
	// a type a relation shape still names is refused by that table's key.
	dependsOn func(context.Context, *sql.Tx, string) (string, error)
}

// retireNames deletes stored names the vocabulary no longer declares, after
// proving nothing still depends on one. A type still referenced by a relation
// shape is refused by that table's foreign key, which is the same closure
// retireShapes gets from authz_tuple's; an action has dependants no foreign key
// covers, and dependsOn is what names them. See actionDependants.
func (s *Store) retireNames(ctx context.Context, tx *sql.Tx, table nameTable, keep []string) error {
	stored, err := scanNames(ctx, tx, s.dialect, table.selectSQL)
	if err != nil {
		return storeFailure("read stored "+table.what+"s", err)
	}

	for _, name := range stored {
		if slices.Contains(keep, name) {
			continue
		}

		if table.dependsOn != nil {
			why, checkErr := table.dependsOn(ctx, tx, name)
			if checkErr != nil {
				return checkErr
			}

			if why != "" {
				return invalidStoredTuple.New(fmt.Sprintf(
					"the vocabulary no longer declares the %s %q and it cannot be retired: %s",
					table.what, name, why,
				))
			}
		}

		if _, err = tx.ExecContext(ctx, s.q(table.deleteSQL), name); err != nil {
			return storeFailure("retire "+table.what+" "+name, err)
		}
	}

	return nil
}

func (s *Store) storedShapes(ctx context.Context, q queryer) ([]relationShape, error) {
	rows, err := q.QueryContext(ctx, s.q(selectShapesSQL))
	if err != nil {
		return nil, storeFailure("read relation shapes", err)
	}

	defer func() { _ = rows.Close() }() // the scan error below is the one worth reporting

	var out []relationShape

	for rows.Next() {
		var objectType, relation, subjectType, subjectRelation string
		if err = rows.Scan(&objectType, &relation, &subjectType, &subjectRelation); err != nil {
			return nil, storeFailure("scan relation shape", err)
		}

		out = append(out, relationShape{
			ObjectType:      Type(objectType),
			Relation:        Relation(relation),
			SubjectType:     Type(subjectType),
			SubjectRelation: Relation(subjectRelation),
		})
	}

	if err = rows.Err(); err != nil {
		return nil, storeFailure("read relation shapes", err)
	}

	return out, nil
}

// scanNames reads one column of names. It takes no arguments, because every
// statement it serves reads a whole vocabulary table: a query with a parameter
// is a query about one row and has its own reader.
func scanNames(ctx context.Context, q queryer, dialect Dialect, query string) ([]string, error) {
	rows, err := q.QueryContext(ctx, dialect.render(query))
	if err != nil {
		return nil, err
	}

	defer func() { _ = rows.Close() }() // the scan error below is the one worth reporting

	var out []string

	for rows.Next() {
		var name string
		if err = rows.Scan(&name); err != nil {
			return nil, err
		}

		out = append(out, name)
	}

	return out, rows.Err()
}
