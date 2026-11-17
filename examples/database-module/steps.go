package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/prusa3d/prusa-connect/sdk/moduledb"
	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

// wordsVersion is the schema version that adds the word count:
// 0003_note_words.sql adds the column, and the step fills it.
const wordsVersion = 3

// steps are the migrations SQL cannot say. 0003_note_words.sql adds the
// column, and this step counts the words of every note kept before it, in
// the same transaction: a note is never seen with the column and no count.
//
//nolint:gochecknoglobals // a fixed list the schema reads, never written.
var steps = []moduledb.Step{
	{Version: wordsVersion, Name: "count_words", Up: countWords},
}

// countWords fills the word count of every note. Both engines read $1, so the
// statements are the same on each.
func countWords(ctx context.Context, tx moduledb.Tx, _ wirerpc.DatabaseEngine) error {
	counted, err := wordsOfEveryNote(ctx, tx)
	if err != nil {
		return err
	}

	for id, words := range counted {
		if _, err = tx.ExecContext(ctx, `UPDATE note SET words = $1 WHERE id = $2`, words, id); err != nil {
			return fmt.Errorf("count the words of note %d: %w", id, err)
		}
	}

	return nil
}

// wordsOfEveryNote reads every note and counts its words. The rows are closed
// before the step writes: SQLite runs a migration on one connection, which a
// reader still open would hold.
func wordsOfEveryNote(ctx context.Context, tx moduledb.Tx) (map[int64]int, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id, text FROM note`)
	if err != nil {
		return nil, fmt.Errorf("read the notes: %w", err)
	}

	defer func() { _ = rows.Close() }() // rows.Err below reports what this would

	counted := map[int64]int{}

	for rows.Next() {
		var (
			id   int64
			text string
		)

		if err = rows.Scan(&id, &text); err != nil {
			return nil, fmt.Errorf("read a note: %w", err)
		}

		counted[id] = len(strings.Fields(text))
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("read the notes: %w", err)
	}

	return counted, nil
}
