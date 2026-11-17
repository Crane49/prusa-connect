package catalog

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/stateapi"
	"github.com/prusa3d/prusa-connect/sdk/storageapi"
	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

// Releases up to 0.2.3 kept the catalogue in the server's record store, one
// JSON record per folder, part, version and comment. This file reads such a
// store into the database, once, at the first request of a newer release.
//
// The move is not a moduledb step. A step runs in plugin.configure, and the
// host answers stateapi.ErrNotReady to every record call made before the
// module reports ready. So the move runs at the first request instead, in one
// transaction of its own, which also writes the row of record_import. A move
// that fails writes nothing, and the next request tries again. A database
// that holds that row is never moved into again.
//
// The records stay in the record store. A release before 0.3.0, installed
// again, still finds them. What it finds is the catalogue as it was at the
// move.

// Records is the part of the host's record store the move reads. It is
// declared here, where it is used, so a test hands in a store of its own.
// `*stateapi.Client` satisfies it.
type Records interface {
	// Get answers the record under key, and stateapi.ErrNoRecord when there is
	// none.
	Get(ctx context.Context, key stateapi.Key) (stateapi.Value, error)
	// List answers every key under prefix, in sorted order, and never a value.
	List(ctx context.Context, prefix stateapi.Key) ([]stateapi.Key, error)
}

// Files is the part of the host's storage door this module uses. It asks one
// question: is this file there, and how long is it. `*storageapi.Client`
// satisfies it.
//
// The module never writes a file. The page uploads it through the core's own
// storage routes, which take write_storage for the person, and the module
// then records where it went.
type Files interface {
	Stat(ctx context.Context, params storageapi.StatParams) (storageapi.FileInfo, error)
}

// The keys the earlier releases wrote inside the module's namespace.
//
// An id is padded to idDigits and a version number to numberDigits, so the
// sorted order stateapi.List answers is the order of the numbers.
const (
	folderPrefix  = "folder/"
	partPrefix    = "part/"
	versionPrefix = "version/"
	commentPrefix = "comment/"
	counterKey    = stateapi.Key("counter")

	idDigits     = 10
	numberDigits = 5
)

// MaxID is the largest id this module hands out. It is the largest number
// the earlier releases could spell in a key.
const MaxID = 9_999_999_999

// numberOf reads one padded number back out of a key, after its prefix. It
// reports a segment that is not one.
func numberOf(segment string, digits int) (int, bool) {
	if len(segment) != digits {
		return 0, false
	}

	n, err := strconv.Atoi(segment)
	if err != nil || n < 1 {
		return 0, false
	}

	return n, true
}

// idOf reads the id out of a folder or a part key.
func idOf(key stateapi.Key, prefix string) (int, bool) {
	rest, found := strings.CutPrefix(string(key), prefix)
	if !found {
		return 0, false
	}

	return numberOf(rest, idDigits)
}

// pairOf reads the part and the second number out of a version or a comment
// key.
func pairOf(key stateapi.Key, prefix string, digits int) (part, second int, ok bool) {
	rest, found := strings.CutPrefix(string(key), prefix)
	if !found {
		return 0, 0, false
	}

	head, tail, found := strings.Cut(rest, "/")
	if !found {
		return 0, 0, false
	}

	part, ok = numberOf(head, idDigits)
	if !ok {
		return 0, 0, false
	}

	second, ok = numberOf(tail, digits)

	return part, second, ok
}

// counters is what the counter record held: the id the next folder, part and
// comment get.
type counters struct {
	V       int `json:"v"`
	Folder  int `json:"folder"`
	Part    int `json:"part"`
	Comment int `json:"comment"`
}

// legacy is what one read of the record store found.
type legacy struct {
	folders  map[int]held[Folder]
	parts    []Part
	versions []Version
	comments []Comment
	counter  counters
}

// rows is how many rows the move writes.
func (l legacy) rows() int {
	return len(l.folders) + len(l.parts) + len(l.versions) + len(l.comments)
}

// importRecords moves the records of an earlier release into the database,
// once. The caller holds the write lock.
func (c *Catalog) importRecords(ctx context.Context, db store) error {
	moved, err := imported(ctx, db)
	if err != nil {
		return err
	}

	if moved {
		return nil
	}

	found, err := c.readLegacy(ctx)
	if err != nil {
		return err
	}

	err = db.within(ctx, "move the records of an earlier release", func(tx *sql.Tx) error {
		return writeLegacy(ctx, tx, db, found, c.now())
	})
	if err != nil {
		return err
	}

	if found.rows() > 0 {
		c.log.InfoContext(ctx, "moved the catalogue out of the record store into the module's database",
			slog.Int("folders", len(found.folders)), slog.Int("parts", len(found.parts)),
			slog.Int("versions", len(found.versions)), slog.Int("comments", len(found.comments)))
	}

	return nil
}

// imported reports whether the database already holds the move.
func imported(ctx context.Context, db store) (bool, error) {
	var count int
	if err := db.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM record_import`).Scan(&count); err != nil {
		return false, storeFailed("read whether the records were moved", err)
	}

	return count > 0, nil
}

// readLegacy reads every record an earlier release wrote. A host that serves
// no record store has nothing to move. Any other failure refuses the move, so
// the next request tries again rather than starting an empty catalogue over a
// store it could not read.
func (c *Catalog) readLegacy(ctx context.Context) (legacy, error) {
	found := legacy{
		folders: map[int]held[Folder]{},
		counter: counters{V: Format, Folder: 1, Part: 1, Comment: 1},
	}

	if c.records == nil {
		return found, nil
	}

	keys, err := c.records.List(ctx, "")
	if err != nil {
		if coded, ok := errors.AsType[*wirerpc.Error](err); ok && coded.Code == wirerpc.CodeMethodNotFound {
			return found, nil
		}

		return legacy{}, storeFailed("list the records of an earlier release", err)
	}

	if len(keys) == 0 {
		return found, nil
	}

	return c.decodeLegacy(ctx, keys, found)
}

// decodeLegacy reads the records under keys into rows.
//
// A record that cannot be read, or that a later release wrote, is skipped and
// logged, because one such record would otherwise stop the whole catalogue. A
// version or a comment whose part is gone is skipped too. Only the counter is
// read or refused whole, because without it an id could be given out twice.
func (c *Catalog) decodeLegacy(ctx context.Context, keys []stateapi.Key, found legacy) (legacy, error) {
	present := map[int]bool{}

	for _, key := range keys {
		if err := c.decodeKey(ctx, key, &found, present); err != nil {
			return legacy{}, err
		}
	}

	untangle(found.folders, c.log)

	return keepOwned(found, present), nil
}

// decodeKey reads one record into found. present collects the id of every
// part key, also of a part whose record was skipped, because such a part
// still owns its versions and comments.
func (c *Catalog) decodeKey(ctx context.Context, key stateapi.Key, found *legacy, present map[int]bool) error {
	switch {
	case key == counterKey:
		counter, err := c.readCounter(ctx)
		if err != nil {
			return err
		}

		found.counter = counter
	case strings.HasPrefix(string(key), folderPrefix):
		var row Folder
		if id, ok := idOf(key, folderPrefix); ok && c.decode(ctx, key, &row) {
			row.ID = id
			found.folders[id] = held[Folder]{row: row}
		}
	case strings.HasPrefix(string(key), partPrefix):
		id, ok := idOf(key, partPrefix)
		if !ok {
			return nil
		}

		present[id] = true

		var row Part
		if c.decode(ctx, key, &row) {
			row.ID = id
			found.parts = append(found.parts, row)
		}
	case strings.HasPrefix(string(key), versionPrefix):
		var row Version
		if part, number, ok := pairOf(key, versionPrefix, numberDigits); ok && c.decode(ctx, key, &row) {
			row.Part, row.Number = part, number
			found.versions = append(found.versions, row)
		}
	case strings.HasPrefix(string(key), commentPrefix):
		var row Comment
		if part, id, ok := pairOf(key, commentPrefix, idDigits); ok && c.decode(ctx, key, &row) {
			row.Part, row.ID = part, id
			found.comments = append(found.comments, row)
		}
	}

	return nil
}

// keepOwned drops the versions and the comments of parts that are not
// moved, and raises each part's version mark to the versions it holds.
func keepOwned(found legacy, present map[int]bool) legacy {
	moved := map[int]*Part{}
	for at := range found.parts {
		moved[found.parts[at].ID] = &found.parts[at]
	}

	versions := found.versions[:0]

	for _, row := range found.versions {
		if part, ok := moved[row.Part]; ok {
			part.LastVersion = max(part.LastVersion, row.Number)
			versions = append(versions, row)
		}
	}

	comments := found.comments[:0]

	for _, row := range found.comments {
		if _, ok := moved[row.Part]; ok {
			comments = append(comments, row)
		}
	}

	found.versions, found.comments = versions, comments

	// A part kept by a later release is not moved, and its number is still
	// never given out again.
	for id := range present {
		found.counter.Part = max(found.counter.Part, id+1)
	}

	return found
}

// decode reads one record into row, and reports a record it skipped.
func (c *Catalog) decode(ctx context.Context, key stateapi.Key, row any) bool {
	value, err := c.records.Get(ctx, key)
	if err != nil {
		c.log.WarnContext(ctx, "skipped a record that could not be read",
			slog.String("key", string(key)), slog.String("error", err.Error()))

		return false
	}

	if err = json.Unmarshal(value.Bytes, row); err != nil {
		c.log.WarnContext(ctx, "skipped a record that does not parse",
			slog.String("key", string(key)), slog.String("error", err.Error()))

		return false
	}

	if format := formatOf(row); format != Format {
		c.log.WarnContext(ctx, "skipped a record of a format this build does not read",
			slog.String("key", string(key)), slog.Int("format", format), slog.Int("reads", Format))

		return false
	}

	return true
}

// formatOf answers the format field of any record the earlier releases wrote.
func formatOf(row any) int {
	switch typed := row.(type) {
	case *Folder:
		return typed.V
	case *Part:
		return typed.V
	case *Version:
		return typed.V
	case *Comment:
		return typed.V
	}

	return 0
}

// readCounter reads the counter record.
func (c *Catalog) readCounter(ctx context.Context) (counters, error) {
	value, err := c.records.Get(ctx, counterKey)
	if err != nil {
		return counters{}, storeFailed("read the counter of an earlier release", err)
	}

	var row counters
	if err = json.Unmarshal(value.Bytes, &row); err != nil {
		return counters{}, fmt.Errorf("%w: the counter is not a record this module wrote: %w", ErrStore, err)
	}

	if row.V != Format {
		return counters{}, fmt.Errorf("%w: the counter is format %d and this build reads %d",
			ErrStore, row.V, Format)
	}

	return row, nil
}

// writeLegacy writes what the record store held, raises the id counters past
// every id it gave out, and records the move. It runs in one transaction.
func writeLegacy(ctx context.Context, tx *sql.Tx, db store, found legacy, finished time.Time) error {
	next := map[string]int{
		countFolder:  found.counter.Folder,
		countPart:    found.counter.Part,
		countComment: found.counter.Comment,
	}

	for _, row := range found.folders {
		next[countFolder] = max(next[countFolder], row.row.ID+1)

		if err := insertFolder(ctx, tx, db, row.row); err != nil {
			return err
		}
	}

	for _, row := range found.parts {
		next[countPart] = max(next[countPart], row.ID+1)

		row.Tags = tagsOf(row.Tags)

		if err := insertPart(ctx, tx, db, row, 1); err != nil {
			return err
		}
	}

	for _, row := range found.versions {
		if row.Files == nil {
			row.Files = []File{}
		}

		if err := insertVersion(ctx, tx, db, row); err != nil {
			return err
		}
	}

	for _, row := range found.comments {
		next[countComment] = max(next[countComment], row.ID+1)

		if err := insertComment(ctx, tx, db, row); err != nil {
			return err
		}
	}

	for kind, value := range next {
		if _, err := tx.ExecContext(ctx, `UPDATE id_counter SET next = $1 WHERE name = $2 AND next < $1`,
			value, kind); err != nil {
			return fmt.Errorf("raise the %s counter: %w", kind, err)
		}
	}

	_, err := tx.ExecContext(ctx, `INSERT INTO record_import (id, finished, moved) VALUES (1, $1, $2)`,
		db.at(finished), found.rows())
	if err != nil {
		return fmt.Errorf("record the move: %w", err)
	}

	return nil
}

// tagsOf keeps each tag of a record once, so a record written by hand with a
// repeated tag still fits the table's key.
func tagsOf(written []string) []string {
	out := make([]string, 0, len(written))

	for _, tag := range written {
		if tag != "" && !slices.Contains(out, tag) {
			out = append(out, tag)
		}
	}

	return out
}
