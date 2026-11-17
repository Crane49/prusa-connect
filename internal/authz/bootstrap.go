package authz

import (
	"context"
	"database/sql"

	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
)

const (
	countTuplesSQL = `SELECT COUNT(*) FROM authz_tuple`

	// The insert both reads the marker and writes it, in one statement. A count
	// followed by an insert is two statements, and on Postgres each takes its
	// own READ COMMITTED snapshot. Two first starts against one database would
	// both count zero and both insert, and the loser would fail on the unique
	// violation, which the composition root turns into a refusal to start. With
	// the conflict clause the loser writes no row, and the row count says
	// whether this process is the one that bootstraps. Both engines take this
	// spelling.
	insertBootstrapSQL = `INSERT INTO authz_bootstrap (id, owner) VALUES (1, ?)
	ON CONFLICT DO NOTHING`

	// The handover claims the marker the same way: one statement reads the
	// owner and moves it, and the row count says whether this process is the
	// one that hands over. A second process with the same new owner updates no
	// row and writes nothing.
	handOverBootstrapSQL = `UPDATE authz_bootstrap SET owner = ? WHERE id = 1 AND owner <> ?`
)

// Bootstrap writes the first grant, and only into a graph that has never had
// one. It returns whether it wrote one.
//
// A server whose graph is empty grants nobody anything. A fresh install would
// be unusable, and no authorized call could write the grant that fixes it. So
// the first owner is data, written once. An empty graph is never read as
// permission: every later decision reads the projection like any other.
//
// "Never had one" is read from the marker row in authz_bootstrap, claimed by
// the same insert that writes it and in the same transaction as the grant. It
// is not read from the tuple table. An operator may revoke every tuple through
// the grant API. Reading the tuple table would then hand the whole graph back
// to the configured principal on the next start, and on a deployment that
// authenticates nobody that principal is every anonymous request.
//
// A database that holds tuples and no marker is adopted and not bootstrapped
// again: the marker goes in, the grant does not, and the graph somebody
// narrowed stays narrowed.
//
// What it writes is an org, the owner as its admin, and the org over every
// configurable resource kind, which is manager on each of them. It writes
// nothing on any device: a device is invisible until it is registered, and
// whatever registers one is what has to place it in an org.
func (s *Store) Bootstrap(ctx context.Context, org string, owner Subject) (bool, error) {
	tuples, err := bootstrapTuples(org, owner)
	if err != nil {
		return false, err
	}

	var wrote bool

	err = s.mutate(ctx, func(ctx context.Context, tx *sql.Tx, moved *scope) (int64, error) {
		for _, tuple := range tuples {
			moved.moved(tuple.object)
		}

		written, markErr := s.mark(ctx, tx, owner)
		if markErr != nil {
			return 0, markErr
		}

		// No row written means the marker was already there, either from an
		// earlier run or from a process that committed while this transaction
		// was open. Either way this graph has been decided about, and nothing
		// below it may run: the tuple table is empty in a graph an operator
		// revoked everything from, and reaching the write below would hand that
		// graph back.
		if written == 0 {
			return 0, nil
		}

		held, countErr := s.count(ctx, tx, countTuplesSQL)
		if countErr != nil {
			return 0, countErr
		}

		if held > 0 {
			return written, nil
		}

		granted, writeErr := s.insertTuples(ctx, tx, tuples)
		if writeErr != nil {
			return 0, writeErr
		}

		wrote = granted > 0

		return written + granted, nil
	})
	if err != nil {
		return false, err
	}

	return wrote, nil
}

// HandOver writes the first grant again for owner when the marker names
// somebody else, and moves the marker to owner. It returns whether it wrote a
// tuple.
//
// This is the recovery for a changed core.principal. A first start in open
// mode, or before an issuer was configured, writes the marker for the
// principal of that start, and Bootstrap never writes again. Without this an
// operator who then names their own subject at the provider holds nothing, and
// no authorized call can write the grant that fixes it.
//
// It keys on the owner the marker names, and not on the tuple table, for the
// reason Bootstrap keys on the marker: a restart with the same principal hands
// back nothing an operator withdrew. What it writes is what Bootstrap writes:
// the admin role on the org, and the org over every configurable resource
// kind, because no call can write that edge and an admin role without it
// reaches nothing. A tuple already there is left alone, and the earlier owner
// keeps any grant it holds until an administrator withdraws it.
func (s *Store) HandOver(ctx context.Context, org string, owner Subject) (bool, error) {
	tuples, err := bootstrapTuples(org, owner)
	if err != nil {
		return false, err
	}

	var wrote bool

	err = s.mutate(ctx, func(ctx context.Context, tx *sql.Tx, moved *scope) (int64, error) {
		for _, tuple := range tuples {
			moved.moved(tuple.object)
		}

		result, execErr := tx.ExecContext(ctx, s.q(handOverBootstrapSQL), owner.String(), owner.String())
		if execErr != nil {
			return 0, storeFailure("hand the bootstrap marker over", execErr)
		}

		claimed, rowsErr := result.RowsAffected()
		if rowsErr != nil {
			return 0, storeFailure("count the bootstrap handover", rowsErr)
		}

		if claimed == 0 {
			return 0, nil
		}

		granted, writeErr := s.insertTuples(ctx, tx, tuples)
		if writeErr != nil {
			return 0, writeErr
		}

		wrote = granted > 0

		return claimed + granted, nil
	})
	if err != nil {
		return false, err
	}

	return wrote, nil
}

// count runs one of this file's COUNT statements inside the transaction that
// decides on it.
func (s *Store) count(ctx context.Context, tx *sql.Tx, query string) (int, error) {
	var total int

	if err := tx.QueryRowContext(ctx, s.q(query)).Scan(&total); err != nil {
		return 0, storeFailure("count rows", err)
	}

	return total, nil
}

// mark records that this graph has been bootstrapped, naming the owner the
// first grant was written for. It returns how many rows it wrote, which is zero
// when another process already holds the marker.
func (s *Store) mark(ctx context.Context, tx *sql.Tx, owner Subject) (int64, error) {
	result, err := tx.ExecContext(ctx, s.q(insertBootstrapSQL), owner.String())
	if err != nil {
		return 0, storeFailure("mark the graph as bootstrapped", err)
	}

	written, err := result.RowsAffected()
	if err != nil {
		return 0, storeFailure("count the bootstrap marker", err)
	}

	return written, nil
}

func (s *Store) insertTuples(ctx context.Context, tx *sql.Tx, tuples []Tuple) (int64, error) {
	var written int64

	for _, tuple := range tuples {
		result, err := tx.ExecContext(ctx, s.q(insertTupleSQL), tupleArgs(tuple)...)
		if err != nil {
			return 0, storeFailure("insert the first grant", err)
		}

		affected, err := result.RowsAffected()
		if err != nil {
			return 0, storeFailure("count the first grant", err)
		}

		written += affected
	}

	return written, nil
}

// bootstrapTuples builds the first grant, through the same constructors any
// other caller goes through, so an owner the vocabulary will not take is
// refused before a transaction is opened.
func bootstrapTuples(org string, owner Subject) ([]Tuple, error) {
	tenant, err := NewObject(TypeOrg, org)
	if err != nil {
		return nil, err
	}

	admin, err := NewTuple(tenant, RelAdmin, owner)
	if err != nil {
		return nil, err
	}

	tenantAsSubject, err := NewSubject(TypeOrg, org)
	if err != nil {
		return nil, err
	}

	kinds := ref.ConfigurableKinds()
	out := make([]Tuple, 0, len(kinds)+1)
	out = append(out, admin)

	for _, kind := range kinds {
		object, objectErr := NewObject(TypeResourceKind, kind.String())
		if objectErr != nil {
			return nil, objectErr
		}

		tuple, tupleErr := NewTuple(object, RelOrg, tenantAsSubject)
		if tupleErr != nil {
			return nil, tupleErr
		}

		out = append(out, tuple)
	}

	return out, nil
}

// grantFirstSight writes the org-viewer tuple a history provider needs, and
// only in the reconcile that first registers the instance.
//
// A history provider holding no grant stores nothing and reports no error, and
// the deployment finds out when somebody needs the rows. The grant is one
// tuple on the org, so it covers every device this server has and every one it
// admits later. A grant per device cannot do that.
//
// It is keyed on the directory row and not on the tuple table, for the reason
// Bootstrap is keyed on its marker: an operator who withdrew this grant must
// not have it handed back on the next start. A re-registered instance is seen
// for the first time again and is granted again, as a new instance under a
// name that was used before would be.
func (s *Store) grantFirstSight(
	ctx context.Context, tx *sql.Tx, moved *scope, instance ref.Ref, registered int64,
) (int64, error) {
	if registered == 0 || instance.Kind() != ref.KindHistory {
		return 0, nil
	}

	subject, err := NewSubject(TypeHistory, instance.ID())
	if err != nil {
		return 0, err
	}

	tenant, err := NewObject(TypeOrg, bootstrapOrgName)
	if err != nil {
		return 0, err
	}

	tuple, err := NewTuple(tenant, RelViewer, subject)
	if err != nil {
		return 0, err
	}

	moved.moved(tenant)

	return s.insertTuples(ctx, tx, []Tuple{tuple})
}

// bootstrapOrgName is the org the first grants are written on. The composition
// root spells the same name; a resource instance does not federate and the
// graph is node-local, so one name serves every deployment.
const bootstrapOrgName = "local"
