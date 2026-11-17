package catalog

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

// store is the catalogue's own database: the tables migrations/ makes, on the
// engine the operator granted. Every change that touches more than one row
// runs in one transaction, so a reader never sees half of it.
//
// The statements are the same on both engines. Both read $1, and both answer
// RETURNING. Only a time differs: SQLite keeps it as text, and Postgres as a
// timestamp.
type store struct {
	db     *sql.DB
	engine wirerpc.DatabaseEngine
}

// at renders a time for one statement.
func (s store) at(when time.Time) any {
	if s.engine == wirerpc.DatabasePostgres {
		return when.UTC()
	}

	return when.UTC().Format(time.RFC3339Nano)
}

// stamp reads a time back, from the text SQLite keeps or the timestamp
// Postgres keeps.
type stamp struct {
	time.Time

	valid bool
}

func (s *stamp) Scan(value any) error {
	switch typed := value.(type) {
	case nil:
		*s = stamp{}

		return nil
	case time.Time:
		*s = stamp{Time: typed.UTC(), valid: true}

		return nil
	case string:
		return s.parse(typed)
	case []byte:
		return s.parse(string(typed))
	}

	return fmt.Errorf("a time stored as %T", value)
}

func (s *stamp) parse(text string) error {
	held, err := time.Parse(time.RFC3339Nano, text)
	if err != nil {
		return fmt.Errorf("a time stored as %q: %w", text, err)
	}

	*s = stamp{Time: held.UTC(), valid: true}

	return nil
}

// within runs one change in one transaction, and undoes all of it when any
// step fails.
func (s store) within(ctx context.Context, what string, change func(*sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return storeFailed(what, err)
	}

	if err = change(tx); err != nil {
		_ = tx.Rollback() // the failure of the change is the one worth reporting

		if errors.Is(err, ErrStore) || errors.Is(err, ErrConflict) {
			return err
		}

		return storeFailed(what, err)
	}

	if err = tx.Commit(); err != nil {
		return storeFailed(what, err)
	}

	return nil
}

// The three counters an id is taken from.
const (
	countFolder  = "folder"
	countPart    = "part"
	countComment = "comment"
)

// nextID takes the next id of one kind. The counter only rises, so an id is
// never given out twice, also after the row that had it was deleted.
func nextID(ctx context.Context, tx *sql.Tx, kind string) (int, error) {
	var taken int

	err := tx.QueryRowContext(ctx,
		`UPDATE id_counter SET next = next + 1 WHERE name = $1 RETURNING next - 1`, kind).Scan(&taken)
	if err != nil {
		return 0, fmt.Errorf("take the next %s id: %w", kind, err)
	}

	if taken < 1 || taken > MaxID {
		return 0, fmt.Errorf("%w: the %s counter reads %d, and an id is between 1 and %d", ErrStore, kind, taken, MaxID)
	}

	return taken, nil
}

// snapshot is every row the database holds, as Load reads it.
type snapshot struct {
	folders  []Folder
	parts    []held[Part]
	versions []Version
	comments []Comment
}

// load reads the whole catalogue.
func (s store) load(ctx context.Context) (snapshot, error) {
	var (
		out snapshot
		err error
	)

	if out.folders, err = s.loadFolders(ctx); err != nil {
		return snapshot{}, storeFailed("read the folders", err)
	}

	if out.parts, err = s.loadParts(ctx); err != nil {
		return snapshot{}, storeFailed("read the parts", err)
	}

	if out.versions, err = s.loadVersions(ctx); err != nil {
		return snapshot{}, storeFailed("read the versions", err)
	}

	if out.comments, err = s.loadComments(ctx); err != nil {
		return snapshot{}, storeFailed("read the comments", err)
	}

	return out, nil
}

// rowsOf runs one query and hands each row to read. The rows are closed
// before it returns, so the one SQLite connection is free for the next one.
func (s store) rowsOf(ctx context.Context, query string, read func(*sql.Rows) error) error {
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return err //nolint:wrapcheck // the caller names what it read.
	}

	defer func() { _ = rows.Close() }() // rows.Err below reports what this would

	for rows.Next() {
		if err = read(rows); err != nil {
			return err
		}
	}

	return rows.Err() //nolint:wrapcheck // the caller names what it read.
}

func (s store) loadFolders(ctx context.Context) ([]Folder, error) {
	var out []Folder

	err := s.rowsOf(ctx, `SELECT id, parent, name, description, created, changed, changed_by FROM folder`,
		func(rows *sql.Rows) error {
			var (
				row              Folder
				created, changed stamp
			)

			if err := rows.Scan(&row.ID, &row.Parent, &row.Name, &row.Description,
				&created, &changed, &row.ChangedBy); err != nil {
				return err //nolint:wrapcheck // the caller names what it read.
			}

			row.V, row.Created, row.Changed = Format, created.Time, changed.Time
			out = append(out, row)

			return nil
		})

	return out, err
}

func (s store) loadParts(ctx context.Context) ([]held[Part], error) {
	tags := map[int][]string{}

	err := s.rowsOf(ctx, `SELECT part_id, tag FROM part_tag`, func(rows *sql.Rows) error {
		var (
			part int
			tag  string
		)

		if err := rows.Scan(&part, &tag); err != nil {
			return err //nolint:wrapcheck // the caller names what it read.
		}

		tags[part] = append(tags[part], tag)

		return nil
	})
	if err != nil {
		return nil, err
	}

	var out []held[Part]

	err = s.rowsOf(ctx, `SELECT id, folder, name, summary, description, cover, last_version, revision,
		created, created_by, changed, changed_by FROM part`, func(rows *sql.Rows) error {
		var (
			row              Part
			revision         uint64
			created, changed stamp
		)

		if scanErr := rows.Scan(&row.ID, &row.Folder, &row.Name, &row.Summary, &row.Description, &row.Cover,
			&row.LastVersion, &revision, &created, &row.CreatedBy, &changed, &row.ChangedBy); scanErr != nil {
			return scanErr //nolint:wrapcheck // the caller names what it read.
		}

		row.V, row.Created, row.Changed = Format, created.Time, changed.Time
		row.Tags = tags[row.ID]
		slices.Sort(row.Tags)

		out = append(out, held[Part]{row: row, version: revision})

		return nil
	})

	return out, err
}

func (s store) loadVersions(ctx context.Context) ([]Version, error) {
	type key struct{ part, number int }

	files := map[key][]File{}

	err := s.rowsOf(ctx, `SELECT part_id, number, name, storage, path, size, sha256
		FROM version_file ORDER BY part_id, number, position`, func(rows *sql.Rows) error {
		var (
			at   key
			file File
		)

		if err := rows.Scan(&at.part, &at.number, &file.Name, &file.Storage, &file.Path,
			&file.Size, &file.SHA256); err != nil {
			return err //nolint:wrapcheck // the caller names what it read.
		}

		files[at] = append(files[at], file)

		return nil
	})
	if err != nil {
		return nil, err
	}

	var out []Version

	err = s.rowsOf(ctx, `SELECT part_id, number, note, created, created_by, changed, changed_by
		FROM part_version`, func(rows *sql.Rows) error {
		var (
			row              Version
			created, changed stamp
		)

		if scanErr := rows.Scan(&row.Part, &row.Number, &row.Note, &created, &row.CreatedBy,
			&changed, &row.ChangedBy); scanErr != nil {
			return scanErr //nolint:wrapcheck // the caller names what it read.
		}

		row.V, row.Created, row.Changed = Format, created.Time, changed.Time
		row.Files = files[key{row.Part, row.Number}]

		if row.Files == nil {
			row.Files = []File{}
		}

		out = append(out, row)

		return nil
	})

	return out, err
}

func (s store) loadComments(ctx context.Context) ([]Comment, error) {
	var out []Comment

	err := s.rowsOf(ctx, `SELECT id, part_id, body, author, created, edited FROM part_comment`,
		func(rows *sql.Rows) error {
			var (
				row             Comment
				created, edited stamp
			)

			if err := rows.Scan(&row.ID, &row.Part, &row.Body, &row.By, &created, &edited); err != nil {
				return err //nolint:wrapcheck // the caller names what it read.
			}

			row.V, row.Created = Format, created.Time

			if edited.valid {
				row.Edited = &edited.Time
			}

			out = append(out, row)

			return nil
		})

	return out, err
}

// addFolder writes a new folder under the next folder id, and answers the id.
func (s store) addFolder(ctx context.Context, folder Folder) (int, error) {
	var id int

	err := s.within(ctx, "add a folder", func(tx *sql.Tx) error {
		var err error
		if id, err = nextID(ctx, tx, countFolder); err != nil {
			return err
		}

		folder.ID = id

		return insertFolder(ctx, tx, s, folder)
	})

	return id, err
}

func insertFolder(ctx context.Context, tx *sql.Tx, s store, folder Folder) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO folder (id, parent, name, description, created, changed, changed_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		folder.ID, folder.Parent, folder.Name, folder.Description,
		s.at(folder.Created), s.at(folder.Changed), folder.ChangedBy)
	if err != nil {
		return fmt.Errorf("write folder %d: %w", folder.ID, err)
	}

	return nil
}

// saveFolder writes the fields a person changes on a folder.
func (s store) saveFolder(ctx context.Context, folder Folder) error {
	return s.within(ctx, "change a folder", func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE folder SET parent = $1, name = $2, description = $3,
			changed = $4, changed_by = $5 WHERE id = $6`,
			folder.Parent, folder.Name, folder.Description, s.at(folder.Changed), folder.ChangedBy, folder.ID)

		return err //nolint:wrapcheck // within names the change.
	})
}

// deleteFolder removes one folder.
func (s store) deleteFolder(ctx context.Context, id int) error {
	return s.within(ctx, "delete a folder", func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `DELETE FROM folder WHERE id = $1`, id)

		return err //nolint:wrapcheck // within names the change.
	})
}

// addPart writes a new part under the next part id, at revision 1, and
// answers the id.
func (s store) addPart(ctx context.Context, part Part) (int, error) {
	var id int

	err := s.within(ctx, "add a part", func(tx *sql.Tx) error {
		var err error
		if id, err = nextID(ctx, tx, countPart); err != nil {
			return err
		}

		part.ID = id

		return insertPart(ctx, tx, s, part, 1)
	})

	return id, err
}

func insertPart(ctx context.Context, tx *sql.Tx, s store, part Part, revision uint64) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO part (id, folder, name, summary, description, cover,
		last_version, revision, created, created_by, changed, changed_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
		part.ID, part.Folder, part.Name, part.Summary, part.Description, part.Cover,
		part.LastVersion, revision, s.at(part.Created), part.CreatedBy, s.at(part.Changed), part.ChangedBy)
	if err != nil {
		return fmt.Errorf("write part %d: %w", part.ID, err)
	}

	return writeTags(ctx, tx, part)
}

// writeTags replaces the tags of one part.
func writeTags(ctx context.Context, tx *sql.Tx, part Part) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM part_tag WHERE part_id = $1`, part.ID); err != nil {
		return fmt.Errorf("clear the tags of part %d: %w", part.ID, err)
	}

	for _, tag := range part.Tags {
		if _, err := tx.ExecContext(ctx, `INSERT INTO part_tag (part_id, tag) VALUES ($1, $2)`,
			part.ID, tag); err != nil {
			return fmt.Errorf("tag part %d: %w", part.ID, err)
		}
	}

	return nil
}

// savePart writes a part over the revision the caller read, and answers the
// revision it wrote. A part another change moved first is ErrConflict.
func (s store) savePart(ctx context.Context, part Part, revision uint64) (uint64, error) {
	err := s.within(ctx, "change a part", func(tx *sql.Tx) error {
		done, err := tx.ExecContext(ctx, `UPDATE part SET folder = $1, name = $2, summary = $3,
			description = $4, cover = $5, revision = revision + 1, changed = $6, changed_by = $7
			WHERE id = $8 AND revision = $9`,
			part.Folder, part.Name, part.Summary, part.Description, part.Cover,
			s.at(part.Changed), part.ChangedBy, part.ID, revision)
		if err != nil {
			return fmt.Errorf("write part %d: %w", part.ID, err)
		}

		if count, countErr := done.RowsAffected(); countErr == nil && count == 0 {
			return fmt.Errorf("%w: part %d is no longer at revision %d", ErrConflict, part.ID, revision)
		}

		return writeTags(ctx, tx, part)
	})

	return revision + 1, err
}

// deletePart removes a part with its tags, versions, files and comments, in
// one transaction.
func (s store) deletePart(ctx context.Context, id int) error {
	return s.within(ctx, "delete a part", func(tx *sql.Tx) error {
		for _, statement := range []string{
			`DELETE FROM version_file WHERE part_id = $1`,
			`DELETE FROM part_version WHERE part_id = $1`,
			`DELETE FROM part_comment WHERE part_id = $1`,
			`DELETE FROM part_tag WHERE part_id = $1`,
			`DELETE FROM part WHERE id = $1`,
		} {
			if _, err := tx.ExecContext(ctx, statement, id); err != nil {
				return fmt.Errorf("delete part %d: %w", id, err)
			}
		}

		return nil
	})
}

// addVersion writes a new version with its files, and records its number on
// the part, so the number is never given out again.
func (s store) addVersion(ctx context.Context, version Version) error {
	return s.within(ctx, "add a version", func(tx *sql.Tx) error {
		if err := insertVersion(ctx, tx, s, version); err != nil {
			return err
		}

		return markVersion(ctx, tx, version.Part, version.Number)
	})
}

func insertVersion(ctx context.Context, tx *sql.Tx, s store, version Version) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO part_version (part_id, number, note, created, created_by,
		changed, changed_by) VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		version.Part, version.Number, version.Note, s.at(version.Created), version.CreatedBy,
		s.at(version.Changed), version.ChangedBy)
	if err != nil {
		return fmt.Errorf("write version %d of part %d: %w", version.Number, version.Part, err)
	}

	for position, file := range version.Files {
		if _, err = tx.ExecContext(ctx, `INSERT INTO version_file (part_id, number, position, name, storage,
			path, size, sha256) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
			version.Part, version.Number, position, file.Name, file.Storage, file.Path, file.Size,
			file.SHA256); err != nil {
			return fmt.Errorf("write a file of version %d of part %d: %w", version.Number, version.Part, err)
		}
	}

	return nil
}

// markVersion raises the highest version number a part ever gave out. It
// does not move the part's revision: a new version is not an edit of the
// words a person may be changing at the same time.
func markVersion(ctx context.Context, tx *sql.Tx, part, number int) error {
	_, err := tx.ExecContext(ctx, `UPDATE part SET last_version = $1 WHERE id = $2 AND last_version < $1`,
		number, part)
	if err != nil {
		return fmt.Errorf("record version %d on part %d: %w", number, part, err)
	}

	return nil
}

// saveVersion writes the note of one version.
func (s store) saveVersion(ctx context.Context, version Version) error {
	return s.within(ctx, "change a version", func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE part_version SET note = $1, changed = $2, changed_by = $3
			WHERE part_id = $4 AND number = $5`,
			version.Note, s.at(version.Changed), version.ChangedBy, version.Part, version.Number)

		return err //nolint:wrapcheck // within names the change.
	})
}

// deleteVersion removes one version with its files. The part keeps the
// number, so the next version does not take it again.
func (s store) deleteVersion(ctx context.Context, part, number int) error {
	return s.within(ctx, "delete a version", func(tx *sql.Tx) error {
		if err := markVersion(ctx, tx, part, number); err != nil {
			return err
		}

		for _, statement := range []string{
			`DELETE FROM version_file WHERE part_id = $1 AND number = $2`,
			`DELETE FROM part_version WHERE part_id = $1 AND number = $2`,
		} {
			if _, err := tx.ExecContext(ctx, statement, part, number); err != nil {
				return fmt.Errorf("delete version %d of part %d: %w", number, part, err)
			}
		}

		return nil
	})
}

// addComment writes a new comment under the next comment id, and answers the
// id.
func (s store) addComment(ctx context.Context, comment Comment) (int, error) {
	var id int

	err := s.within(ctx, "add a comment", func(tx *sql.Tx) error {
		var err error
		if id, err = nextID(ctx, tx, countComment); err != nil {
			return err
		}

		comment.ID = id

		return insertComment(ctx, tx, s, comment)
	})

	return id, err
}

func insertComment(ctx context.Context, tx *sql.Tx, s store, comment Comment) error {
	var edited any
	if comment.Edited != nil {
		edited = s.at(*comment.Edited)
	}

	_, err := tx.ExecContext(ctx, `INSERT INTO part_comment (id, part_id, body, author, created, edited)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		comment.ID, comment.Part, comment.Body, comment.By, s.at(comment.Created), edited)
	if err != nil {
		return fmt.Errorf("write comment %d: %w", comment.ID, err)
	}

	return nil
}

// saveComment writes the body of one comment.
func (s store) saveComment(ctx context.Context, comment Comment) error {
	var edited any
	if comment.Edited != nil {
		edited = s.at(*comment.Edited)
	}

	return s.within(ctx, "change a comment", func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE part_comment SET body = $1, edited = $2 WHERE id = $3`,
			comment.Body, edited, comment.ID)

		return err //nolint:wrapcheck // within names the change.
	})
}

// deleteComment removes one comment.
func (s store) deleteComment(ctx context.Context, id int) error {
	return s.within(ctx, "delete a comment", func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `DELETE FROM part_comment WHERE id = $1`, id)

		return err //nolint:wrapcheck // within names the change.
	})
}

// size answers how many bytes the database takes, and zero when the engine
// cannot say.
func (s store) size(ctx context.Context) int64 {
	query := `SELECT page_count * page_size FROM pragma_page_count(), pragma_page_size()`
	if s.engine == wirerpc.DatabasePostgres {
		query = `SELECT pg_database_size(current_database())`
	}

	var bytes int64
	if err := s.db.QueryRowContext(ctx, query).Scan(&bytes); err != nil {
		return 0
	}

	return bytes
}
