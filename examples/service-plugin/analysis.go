package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/prusa3d/prusa-connect/sdk/fileapi"
	"github.com/prusa3d/prusa-connect/sdk/storageapi"
)

// The module reads one file type for the core: .demo, a text file whose first
// line is a title and whose other lines are its parts.
//
// The manifest names the type under file_analysis. The core calls
// file.analyze with a read handle for that one file. The module needs no
// read_storage and can read no other file through the handle.

// maxDemoBytes is how much of a .demo file this module reads. A longer file
// is described by its head.
const maxDemoBytes = 64 << 10

// analyzeDemo describes one .demo file.
func analyzeDemo(_ context.Context, params fileapi.Params, file *storageapi.File) (fileapi.Analysis, error) {
	head := make([]byte, min(file.Size(), maxDemoBytes))

	read, err := file.ReadAt(head, 0)
	if err != nil && !errors.Is(err, io.EOF) {
		return fileapi.Analysis{}, fmt.Errorf("read %s: %w", params.Name, err)
	}

	lines := strings.Split(strings.TrimSpace(string(bytes.TrimSpace(head[:read]))), "\n")
	answer := fileapi.Analysis{
		Title: lines[0],
		Facts: []fileapi.Fact{
			{Key: "parts", Label: "Parts", Value: strconv.Itoa(len(lines) - 1)},
			{Key: "bytes", Label: "Size read", Value: strconv.Itoa(read)},
		},
	}

	for index, line := range lines[1:] {
		if index == fileapi.MaxObjects {
			break
		}

		answer.Objects = append(answer.Objects, fileapi.Object{ID: strconv.Itoa(index + 1), Name: line})
	}

	return answer, nil
}
