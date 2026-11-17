// Package catalog holds the parts catalogue: the folders, the parts, the
// versions of the files behind each part and the comments on it, the rules
// each one is held to, and how each is kept in the module's own database.
//
// The database holds the rows and this program keeps a copy of them in
// memory, so a listing, a search and a walk of the folder tree read no
// statement. Load fills the copy at the first request. A write updates the
// copy only after the database committed it, so the copy never reports a row
// the database does not hold.
//
// Inside one process, every change takes one write lock for the whole of its
// check and its write. A check such as "the folder is empty" or "the move
// makes no loop" is only true while nothing else writes. Two requests that
// each passed their check alone could together leave a loop or an orphan.
// Reads take no part in that lock and stay concurrent.
//
// A change that touches several rows, such as deleting a part with its
// versions, is one transaction. A reader never sees half of it.
//
// An earlier release kept the catalogue in the server's record store. The
// first Load reads what that store holds into the database, once (legacy.go).
package catalog

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

// Now is how this package reads the clock. A test replaces it to write a
// record with a time it can compare against.
type Now func() time.Time

// held is one row in the copy this process keeps. version is the revision a
// part is at, which a change of the part names, and zero for every other row.
type held[T any] struct {
	row     T
	version uint64
}

// Place is where the catalogue keeps its files: the storage, and the folder
// inside it. It comes from the settings the host sent.
type Place struct {
	Storage string
	Root    string
}

// Catalog is every folder, part, version and comment this module keeps.
//
// The zero value is not usable: New binds the storage and the clock, Attach
// binds the database, and Load fills the copy.
type Catalog struct {
	records Records
	files   Files
	now     Now
	log     *slog.Logger

	// writing is held by every change from its first check to its last
	// write, so the checks of one change see no other change half done. It
	// is taken before mu and never while mu is held.
	writing sync.Mutex

	mu       sync.RWMutex
	db       *store
	place    Place
	folders  map[int]held[Folder]
	parts    map[int]held[Part]
	versions map[int][]held[Version]
	comments map[int][]held[Comment]
	// search is the lower-cased text a listing searches, one per part. It is
	// built when a part or its files change, so a search lower-cases nothing
	// but the query.
	search map[int]searchText
	ready  bool

	// loading serialises the first read, so several requests arriving at once
	// read the database once between them.
	loading sync.Mutex
}

// New builds a catalogue over one storage door. records is the server's
// record store, where an earlier release kept the catalogue, and nil where
// there is nothing to move.
func New(records Records, files Files, place Place, now Now) *Catalog {
	if now == nil {
		now = time.Now
	}

	return &Catalog{
		records:  records,
		files:    files,
		now:      now,
		log:      slog.New(slog.DiscardHandler),
		place:    place,
		folders:  map[int]held[Folder]{},
		parts:    map[int]held[Part]{},
		versions: map[int][]held[Version]{},
		comments: map[int][]held[Comment]{},
		search:   map[int]searchText{},
	}
}

// WithLog sets where the catalogue reports what it skipped or moved. Nothing
// a person wrote goes there: a line names a key, an id and a reason. Call it
// before the first request.
func (c *Catalog) WithLog(log *slog.Logger) *Catalog {
	if log != nil {
		c.log = log
	}

	return c
}

// Attach binds the database the operator granted, migrated, on its engine.
// The next request reads the catalogue out of it again.
func (c *Catalog) Attach(db *sql.DB, engine wirerpc.DatabaseEngine) {
	c.writing.Lock()
	defer c.writing.Unlock()

	c.mu.Lock()
	defer c.mu.Unlock()

	c.db = &store{db: db, engine: engine}
	c.ready = false
}

// Move takes a new place for the files from the settings the host sent. The
// files already recorded keep the storage and the path they were written
// under, so a changed setting moves where new files go and loses none.
func (c *Catalog) Move(place Place) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.place = place
}

// Place answers where new files go.
func (c *Catalog) Place() Place {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.place
}

// Ready reports whether Load has filled the copy.
func (c *Catalog) Ready() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.ready
}

// Ensure reads the database if nothing has read it yet, and answers what
// stopped it.
//
// The first request is what triggers the read, and not the host's configure
// call. The move out of the record store reads that store, and the host
// answers stateapi.ErrNotReady to every call made while it is still starting
// the module.
func (c *Catalog) Ensure(ctx context.Context) error {
	if c.Ready() {
		return nil
	}

	c.loading.Lock()
	defer c.loading.Unlock()

	if c.Ready() {
		return nil
	}

	return c.Load(ctx)
}

// store answers the database, and ErrStore before Attach bound one.
func (c *Catalog) store() (store, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.db == nil {
		return store{}, fmt.Errorf("%w: the module has opened no database yet", ErrStore)
	}

	return *c.db, nil
}

// Load reads every row out of the database into the copy this process keeps.
// It moves what an earlier release kept in the record store first, once.
func (c *Catalog) Load(ctx context.Context) error {
	c.writing.Lock()
	defer c.writing.Unlock()

	db, err := c.store()
	if err != nil {
		return err
	}

	if err = c.importRecords(ctx, db); err != nil {
		return err
	}

	rows, err := db.load(ctx)
	if err != nil {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	c.folders = map[int]held[Folder]{}
	for _, row := range rows.folders {
		c.folders[row.ID] = held[Folder]{row: row}
	}

	c.untangle()

	c.parts = map[int]held[Part]{}
	for _, row := range rows.parts {
		c.parts[row.row.ID] = row
	}

	c.versions = map[int][]held[Version]{}
	for _, row := range rows.versions {
		c.versions[row.Part] = append(c.versions[row.Part], held[Version]{row: row})
	}

	c.comments = map[int][]held[Comment]{}
	for _, row := range rows.comments {
		c.comments[row.Part] = append(c.comments[row.Part], held[Comment]{row: row})
	}

	c.search = map[int]searchText{}
	for id := range c.parts {
		sortedVersions(c.versions[id])
		sortedComments(c.comments[id])
		c.index(id)
	}

	c.ready = true

	return nil
}

// untangle puts a folder whose parent is gone, or that sits in a loop of
// folders, at the top of the tree. This module writes neither now, but the
// records of an earlier release may hold one. The copy is corrected, and the
// next change of that folder writes the correction. The caller holds the lock.
func (c *Catalog) untangle() {
	untangle(c.folders, c.log)
}

// untangle is the walk over one set of folders, which the move out of the
// record store runs too.
func untangle(folders map[int]held[Folder], log *slog.Logger) {
	for id := range folders {
		seen := map[int]bool{}

		for at := id; at != Root; {
			row, found := folders[at]
			if !found {
				break
			}

			parent := row.row.Parent
			_, known := folders[parent]

			if seen[at] || (parent != Root && !known) {
				log.Warn("shows a folder at the top of the tree, because its parent is gone or in a loop",
					slog.Int("folder", at))

				row.row.Parent = Root
				folders[at] = row

				break
			}

			seen[at] = true
			at = parent
		}
	}
}

// Usage is what the catalogue's database takes, and the limit it is held to.
// Limit is zero: the database is bounded by the disk or by the Postgres
// server, and not by the host.
type Usage struct {
	Bytes int64 `json:"bytes"`
	Limit int64 `json:"limit"`
}

// Usage answers how many bytes the catalogue's database takes.
func (c *Catalog) Usage(ctx context.Context) Usage {
	db, err := c.store()
	if err != nil {
		return Usage{}
	}

	return Usage{Bytes: db.size(ctx)}
}

// storeFailed wraps a database failure as ErrStore, keeping the driver's own
// sentence for the log.
func storeFailed(what string, err error) error {
	return fmt.Errorf("%w: %s: %w", ErrStore, what, err)
}

// sortedVersions keeps a part's versions in the order of their numbers.
func sortedVersions(rows []held[Version]) {
	slices.SortFunc(rows, func(a, b held[Version]) int { return a.row.Number - b.row.Number })
}

// sortedComments keeps a part's comments in the order they were written.
func sortedComments(rows []held[Comment]) {
	slices.SortFunc(rows, func(a, b held[Comment]) int { return a.row.ID - b.row.ID })
}
