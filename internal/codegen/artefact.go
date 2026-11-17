// Package codegen holds the committed-artefact protocol the generator commands
// share: read what is committed, write what the code renders, and refuse the
// two when they differ.
//
// Every generator under cmd/ publishes a closed set as a file in the tree and
// gates that file under -check. The three steps are the same for every
// command, so they live here once and every command gives the same error
// message. Only the sentinel a command wraps and the make target it names
// differ, so both are arguments.
//
// The renderers stay in the commands. Each command renders a different shape,
// so the file protocol is shared here and the layout is not.
package codegen

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// DirPerms is the mode a directory holding a committed artefact is created
// with. An artefact is written by one command and read by everyone, so the
// group reads and nobody else sees it.
const DirPerms = 0o750

// FilePerms is the mode a committed artefact is written with.
const FilePerms = 0o600

// CompareFile is the committed-output gate for one artefact: it fails when the
// bytes on disk are not the bytes the code renders.
//
// stale is the caller's own sentinel, so a command that runs several gates can
// still say which one fired and errors.Is keeps working at the call site.
// target is the make target that fixes it, because the operator reading this
// wants the command to run and not a description of the problem.
func CompareFile(path string, want []byte, stale error, target string) error {
	got, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%w: %s does not exist. Run make %s and commit it", stale, path, target)
	}

	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}

	if !bytes.Equal(got, want) {
		return fmt.Errorf("%w: %s. Run make %s and commit the result", stale, path, target)
	}

	return nil
}

// ReadCatalogue reads the committed set into T, the artefact struct the calling
// command publishes.
//
// A file that does not exist yet answers nil, not an empty set. An artefact
// holding no rows is drift, and only a missing one is a first run. The caller
// tells them apart: one is a vocabulary somebody emptied, and the other is a
// vocabulary nobody has generated.
func ReadCatalogue[T any](path string) (*T, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil //nolint:nilnil // absent and empty are different states, and the caller distinguishes them.
	}

	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	var file T
	if err = json.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}

	return &file, nil
}

// WriteFile publishes an artefact and creates the directory it sits in. A new
// artefact has no directory on its first run.
func WriteFile(path string, content []byte) error {
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, DirPerms); err != nil {
			return fmt.Errorf("create %s: %w", dir, err)
		}
	}

	if err := os.WriteFile(path, content, FilePerms); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}

	return nil
}
