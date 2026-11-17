package catalog

import (
	"context"
	"fmt"
	"slices"
)

// AddComment writes one comment on a part, as the person the host named.
func (c *Catalog) AddComment(ctx context.Context, part int, body, by string) (Comment, error) {
	written, err := block("body", body, MaxCommentBytes, true)
	if err != nil {
		return Comment{}, err
	}

	c.writing.Lock()
	defer c.writing.Unlock()

	c.mu.RLock()
	_, found := c.parts[part]
	count := len(c.comments[part])
	c.mu.RUnlock()

	if !found {
		return Comment{}, fmt.Errorf("%w: %d", ErrPartNotFound, part)
	}

	if count >= MaxComments {
		return Comment{}, fmt.Errorf("%w: a part keeps at most %d comments", ErrFull, MaxComments)
	}

	db, err := c.store()
	if err != nil {
		return Comment{}, err
	}

	comment := Comment{V: Format, Part: part, Body: written, By: by, Created: c.now().UTC()}

	if comment.ID, err = db.addComment(ctx, comment); err != nil {
		return Comment{}, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	c.comments[part] = append(c.comments[part], held[Comment]{row: comment})
	sortedComments(c.comments[part])

	return comment, nil
}

// ChangeComment rewrites the body of one comment. Only the person who wrote
// it may, because a comment says what that person thought.
func (c *Catalog) ChangeComment(ctx context.Context, part, id int, body, by string) (Comment, error) {
	written, err := block("body", body, MaxCommentBytes, true)
	if err != nil {
		return Comment{}, err
	}

	c.writing.Lock()
	defer c.writing.Unlock()

	c.mu.RLock()
	row, err := c.commentAt(part, id)
	c.mu.RUnlock()

	if err != nil {
		return Comment{}, err
	}

	if row.row.By != by {
		return Comment{}, fmt.Errorf("%w: comment %d was written by %s", ErrNotAuthor, id, row.row.By)
	}

	comment := row.row
	comment.V = Format
	comment.Body = written
	edited := c.now().UTC()
	comment.Edited = &edited

	db, err := c.store()
	if err != nil {
		return Comment{}, err
	}

	if err = db.saveComment(ctx, comment); err != nil {
		return Comment{}, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	for at, kept := range c.comments[part] {
		if kept.row.ID == id {
			c.comments[part][at] = held[Comment]{row: comment}
		}
	}

	return comment, nil
}

// DeleteComment removes one comment. Only the person who wrote it may.
func (c *Catalog) DeleteComment(ctx context.Context, part, id int, by string) error {
	c.writing.Lock()
	defer c.writing.Unlock()

	c.mu.RLock()
	row, err := c.commentAt(part, id)
	c.mu.RUnlock()

	if err != nil {
		return err
	}

	if row.row.By != by {
		return fmt.Errorf("%w: comment %d was written by %s", ErrNotAuthor, id, row.row.By)
	}

	db, err := c.store()
	if err != nil {
		return err
	}

	if err = db.deleteComment(ctx, id); err != nil {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	c.comments[part] = slices.DeleteFunc(c.comments[part], func(kept held[Comment]) bool {
		return kept.row.ID == id
	})

	return nil
}

// commentAt finds one comment of a part. The caller holds the read lock.
func (c *Catalog) commentAt(part, id int) (held[Comment], error) {
	if _, found := c.parts[part]; !found {
		return held[Comment]{}, fmt.Errorf("%w: %d", ErrPartNotFound, part)
	}

	for _, row := range c.comments[part] {
		if row.row.ID == id {
			return row, nil
		}
	}

	return held[Comment]{}, fmt.Errorf("%w: part %d has no comment %d", ErrCommentNotFound, part, id)
}
