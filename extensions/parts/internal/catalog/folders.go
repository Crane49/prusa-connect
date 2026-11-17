package catalog

import (
	"context"
	"fmt"
	"slices"
	"strings"
)

// FolderDraft is what a caller sends to make or to change a folder. A field
// left out keeps what is stored.
type FolderDraft struct {
	Name        *string `json:"name,omitempty"`
	Parent      *int    `json:"parent,omitempty"`
	Description *string `json:"description,omitempty"`
}

// FolderView is one folder as a client reads it: the record, and how much it
// holds directly.
type FolderView struct {
	Folder

	// Parts is how many parts are directly in this folder.
	Parts int `json:"parts"`
	// Folders is how many folders are directly in this folder.
	Folders int `json:"folders"`
}

// Folders answers every folder, ordered by parent and then by name, so a
// client builds the tree in one pass.
func (c *Catalog) Folders() []FolderView {
	c.mu.RLock()
	defer c.mu.RUnlock()

	parts := map[int]int{}
	for _, part := range c.parts {
		parts[part.row.Folder]++
	}

	children := map[int]int{}
	for _, folder := range c.folders {
		children[folder.row.Parent]++
	}

	out := make([]FolderView, 0, len(c.folders))
	for id, folder := range c.folders {
		out = append(out, FolderView{Folder: folder.row, Parts: parts[id], Folders: children[id]})
	}

	slices.SortFunc(out, func(a, b FolderView) int {
		if a.Parent != b.Parent {
			return a.Parent - b.Parent
		}

		if order := strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)); order != 0 {
			return order
		}

		return a.ID - b.ID
	})

	return out
}

// RootParts answers how many parts are at the top of the tree, in no folder.
func (c *Catalog) RootParts() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	count := 0

	for _, part := range c.parts {
		if part.row.Folder == Root {
			count++
		}
	}

	return count
}

// Folder answers one folder.
func (c *Catalog) Folder(id int) (FolderView, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	row, found := c.folders[id]
	if !found {
		return FolderView{}, fmt.Errorf("%w: %d", ErrFolderNotFound, id)
	}

	view := FolderView{Folder: row.row}

	for _, part := range c.parts {
		if part.row.Folder == id {
			view.Parts++
		}
	}

	for _, folder := range c.folders {
		if folder.row.Parent == id {
			view.Folders++
		}
	}

	return view, nil
}

// AddFolder writes one new folder.
func (c *Catalog) AddFolder(ctx context.Context, draft FolderDraft, by string) (Folder, error) {
	c.writing.Lock()
	defer c.writing.Unlock()

	folder := Folder{V: Format, Created: c.now().UTC()}
	if err := c.applyFolder(&folder, draft, true); err != nil {
		return Folder{}, err
	}

	db, err := c.store()
	if err != nil {
		return Folder{}, err
	}

	folder.Changed = folder.Created
	folder.ChangedBy = by

	id, err := db.addFolder(ctx, folder)
	if err != nil {
		return Folder{}, err
	}

	folder.ID = id

	c.mu.Lock()
	defer c.mu.Unlock()

	c.folders[id] = held[Folder]{row: folder}

	return folder, nil
}

// ChangeFolder writes the fields a draft carries onto a folder. A new parent
// moves the folder with everything in it.
func (c *Catalog) ChangeFolder(ctx context.Context, id int, draft FolderDraft, by string) (Folder, error) {
	c.writing.Lock()
	defer c.writing.Unlock()

	c.mu.RLock()
	row, found := c.folders[id]
	c.mu.RUnlock()

	if !found {
		return Folder{}, fmt.Errorf("%w: %d", ErrFolderNotFound, id)
	}

	folder := row.row
	if err := c.applyFolder(&folder, draft, false); err != nil {
		return Folder{}, err
	}

	folder.V = Format
	folder.Changed = c.now().UTC()
	folder.ChangedBy = by

	db, err := c.store()
	if err != nil {
		return Folder{}, err
	}

	if err = db.saveFolder(ctx, folder); err != nil {
		return Folder{}, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	c.folders[id] = held[Folder]{row: folder}

	return folder, nil
}

// DeleteFolder removes a folder that holds nothing. A folder with something
// in it is refused rather than emptied, because deleting a part deletes its
// files and that is a choice a person makes one part at a time.
func (c *Catalog) DeleteFolder(ctx context.Context, id int) error {
	c.writing.Lock()
	defer c.writing.Unlock()

	c.mu.RLock()
	_, found := c.folders[id]
	busy := slices.ContainsFunc(mapValues(c.folders), func(f held[Folder]) bool { return f.row.Parent == id }) ||
		slices.ContainsFunc(mapValues(c.parts), func(p held[Part]) bool { return p.row.Folder == id })
	c.mu.RUnlock()

	if !found {
		return fmt.Errorf("%w: %d", ErrFolderNotFound, id)
	}

	if busy {
		return fmt.Errorf(
			"%w: folder %d holds a folder or a part, and an empty folder is deleted",
			ErrFolderNotEmpty,
			id,
		)
	}

	db, err := c.store()
	if err != nil {
		return err
	}

	if err = db.deleteFolder(ctx, id); err != nil {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.folders, id)

	return nil
}

// applyFolder writes a draft onto a folder and holds the result to the rules:
// a name, a parent that exists, no loop, no folder deeper than MaxDepth, and
// no second folder of the same name in one parent.
func (c *Catalog) applyFolder(folder *Folder, draft FolderDraft, creating bool) error {
	if draft.Name != nil || creating {
		written := ""
		if draft.Name != nil {
			written = *draft.Name
		}

		name, err := line("name", written, MaxNameBytes, true)
		if err != nil {
			return err
		}

		if strings.Contains(name, "/") {
			return invalid("name is one folder, and it holds a slash")
		}

		folder.Name = name
	}

	if draft.Description != nil {
		description, err := block("description", *draft.Description, MaxFolderDescriptionBytes, false)
		if err != nil {
			return err
		}

		folder.Description = description
	}

	if draft.Parent != nil {
		folder.Parent = *draft.Parent
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	if err := c.reachable(folder.ID, folder.Parent); err != nil {
		return err
	}

	for _, sibling := range c.folders {
		if sibling.row.ID != folder.ID && sibling.row.Parent == folder.Parent &&
			strings.EqualFold(sibling.row.Name, folder.Name) {
			return fmt.Errorf("%w: %q", ErrNameTaken, folder.Name)
		}
	}

	return nil
}

// reachable holds a parent to the tree: it exists, it is not the folder
// itself or one inside it, and the folder does not end up deeper than
// MaxDepth. id is zero for a folder not yet written.
//
// The walk up is bounded by MaxDepth, so a loop already in the copy ends it
// with a refusal rather than a hang. The caller holds the read lock.
func (c *Catalog) reachable(id, parent int) error {
	depth := 1

	for at := parent; at != Root; {
		if at == id && id != 0 {
			return invalid("folder %d cannot be moved into itself or into a folder inside it", id)
		}

		row, found := c.folders[at]
		if !found {
			return fmt.Errorf("%w: %d, the parent named", ErrFolderNotFound, at)
		}

		depth++
		if depth > MaxDepth {
			return invalid("the tree is at most %d folders deep", MaxDepth)
		}

		at = row.row.Parent
	}

	// A folder moved somewhere keeps its own subtree, so the deepest folder
	// inside it moves down as far.
	if id != 0 && depth+c.below(id) > MaxDepth {
		return invalid("the tree is at most %d folders deep, and the move takes it past that", MaxDepth)
	}

	return nil
}

// below answers how many levels of folders sit under one folder. It indexes
// the children once, so it reads each folder once and not once per level.
// A folder it has already seen is not entered again, so a loop in the store
// cannot make it recurse without end. The caller holds the read lock.
func (c *Catalog) below(id int) int {
	children := map[int][]int{}
	for _, row := range c.folders {
		children[row.row.Parent] = append(children[row.row.Parent], row.row.ID)
	}

	seen := map[int]bool{id: true}
	level := []int{id}
	deepest := 0

	for len(level) > 0 {
		var next []int

		for _, at := range level {
			for _, child := range children[at] {
				if !seen[child] {
					seen[child] = true
					next = append(next, child)
				}
			}
		}

		if len(next) > 0 {
			deepest++
		}

		level = next
	}

	return deepest
}

// within reports whether folder at is root itself or sits somewhere under
// it. The walk is bounded by MaxDepth, which also ends it in a loop. The
// caller holds the read lock.
func (c *Catalog) within(at, root int) bool {
	for steps := 0; steps <= MaxDepth; steps++ {
		if at == root {
			return true
		}

		if at == Root {
			return false
		}

		row, found := c.folders[at]
		if !found {
			return false
		}

		at = row.row.Parent
	}

	return false
}

// mapValues answers the values of a map, for a scan that needs a slice.
func mapValues[K comparable, V any](rows map[K]V) []V {
	out := make([]V, 0, len(rows))
	for _, row := range rows {
		out = append(out, row)
	}

	return out
}
