package projects

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"sync"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/storageapi"
)

// sizedBytes is a file held in memory.
type sizedBytes struct{ *bytes.Reader }

func (b sizedBytes) Size() int64 { return b.Reader.Size() }

// refusingFiles serves one project and refuses to open it as gone, the way
// the host refuses a person whose request has ended. The open as gone waits
// until release, so a second caller can join that read.
type refusingFiles struct {
	body    []byte
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (f *refusingFiles) Stat(context.Context, storageapi.StatParams) (storageapi.FileInfo, error) {
	return storageapi.FileInfo{Path: "box.3mf", Size: int64(len(f.body)), MTime: 1}, nil
}

func (f *refusingFiles) Open(_ context.Context, target storageapi.Target) (File, error) {
	if target.Principal == "user:gone" {
		f.once.Do(func() { close(f.started) })
		<-f.release

		return nil, fmt.Errorf("open: %w", storageapi.ErrDenied)
	}

	return sizedBytes{bytes.NewReader(f.body)}, nil
}

// A read that runs past its caller's request is refused as that caller. A
// caller who waited on it reads as itself, and the refusal is not kept for
// the next caller.
func TestARefusalOfAnotherCallerIsNotThisCallersAnswer(t *testing.T) {
	t.Parallel()

	body, err := os.ReadFile("../../threemf/testdata/prusaslicer-2.9.6-multibed.3mf")
	if err != nil {
		t.Fatal(err)
	}

	files := &refusingFiles{body: body, started: make(chan struct{}), release: make(chan struct{})}
	reader := New(files)
	request := func(who string) Request { return Request{Principal: who, Storage: "farm", Path: "box.3mf"} }

	gone := make(chan error, 1)

	go func() {
		_, _, readErr := reader.Project(t.Context(), request("user:gone"))
		gone <- readErr
	}()

	<-files.started

	here := make(chan error, 1)

	go func() {
		_, _, readErr := reader.Project(t.Context(), request("user:here"))
		here <- readErr
	}()

	close(files.release)

	if readErr := <-gone; readErr == nil {
		t.Error("the read as a person whose request ended was answered")
	}

	if readErr := <-here; readErr != nil {
		t.Fatalf("a person who may read the file was refused: %v", readErr)
	}

	if _, _, readErr := reader.Project(t.Context(), request("user:later")); readErr != nil {
		t.Errorf("the refusal was kept for the next person: %v", readErr)
	}
}
