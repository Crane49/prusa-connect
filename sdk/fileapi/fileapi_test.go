package fileapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/fileapi"
	"github.com/prusa3d/prusa-connect/sdk/storageapi"
	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

func picture(size int) fileapi.Thumbnail {
	return fileapi.Thumbnail{MediaType: fileapi.MediaPNG, Width: 1, Height: 1, Data: make([]byte, size)}
}

func TestAnAnalysisIsHeldToItsBounds(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("x", fileapi.MaxTextBytes+1)

	cases := map[string]fileapi.Analysis{
		"a long title": {Title: long},
		"five pictures": {
			Thumbnails: []fileapi.Thumbnail{picture(1), picture(1), picture(1), picture(1), picture(1)},
		},
		"too many bytes":       {Thumbnails: []fileapi.Thumbnail{picture(fileapi.MaxThumbnailBytes), picture(1)}},
		"a picture of no size": {Thumbnails: []fileapi.Thumbnail{{MediaType: fileapi.MediaPNG, Data: []byte{1}}}},
		"a gif": {
			Thumbnails: []fileapi.Thumbnail{{MediaType: "image/gif", Width: 1, Height: 1, Data: []byte{1}}},
		},
		"a fact with no key":   {Facts: []fileapi.Fact{{Label: "Plates", Value: "3"}}},
		"a long value":         {Facts: []fileapi.Fact{{Key: "plates", Value: long}}},
		"an object with no id": {Objects: []fileapi.Object{{Name: "Cube"}}},
		"too many objects":     {Objects: make([]fileapi.Object, fileapi.MaxObjects+1)},
		"a long printer model": {Print: &storageapi.PrintInfo{PrinterModel: long}},
		"a long colour": {
			Print: &storageapi.PrintInfo{Filaments: []storageapi.PrintFilament{{Colour: long}}},
		},
		"too many filaments": {
			Print: &storageapi.PrintInfo{Filaments: make([]storageapi.PrintFilament, fileapi.MaxFilaments+1)},
		},
		"an object outline of too many points": {
			Print: &storageapi.PrintInfo{Objects: []storageapi.PrintObject{
				{ID: 0, Name: "Cube", Outline: make([]storageapi.PrintPoint, fileapi.MaxOutlinePoints+1)},
			}},
		},
		"too many printed objects": {
			Print: &storageapi.PrintInfo{Objects: make([]storageapi.PrintObject, fileapi.MaxObjects+1)},
		},
		"a long feature": {
			Print: &storageapi.PrintInfo{Checks: &storageapi.PrintChecks{Features: []string{long}}},
		},
	}

	for name, answer := range cases {
		if err := answer.Validate(); !errors.Is(err, fileapi.ErrOutOfBounds) {
			t.Errorf("%s held as %v", name, err)
		}
	}

	whole := fileapi.Analysis{
		Title:      "Bracket",
		Thumbnails: []fileapi.Thumbnail{picture(fileapi.MaxThumbnailBytes)},
		Print:      &storageapi.PrintInfo{PrinterModel: "MK4S"},
		Facts:      []fileapi.Fact{{Key: "plates", Label: "Plates", Value: "3"}},
		Objects:    []fileapi.Object{{ID: "1", Name: "Cube"}},
	}

	if err := whole.Validate(); err != nil {
		t.Fatalf("an answer inside every bound was refused: %v", err)
	}
}

// moduleSide answers a call the way a session would, through the handler
// Serve builds, and refuses a read the way a host with no file would.
type moduleSide struct{ handler wirerpc.Handler }

func (m moduleSide) Call(ctx context.Context, _ wirerpc.Method, params any) (json.RawMessage, error) {
	encoded, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}

	answered, err := m.handler(ctx, wirerpc.Call{Method: fileapi.MethodAnalyze, Params: encoded})
	if err != nil {
		return nil, err
	}

	return json.Marshal(answered)
}

// statOnly is a host that knows one handle and describes its file.
type statOnly struct{}

func (statOnly) Call(_ context.Context, method wirerpc.Method, params any) (json.RawMessage, error) {
	if method != storageapi.MethodStat {
		return nil, errors.New("only stat")
	}

	if held, ok := params.(storageapi.StatParams); !ok || held.Handle != "h1" {
		return nil, &wirerpc.Error{Code: storageapi.CodeNoFile, Message: "no file"}
	}

	return json.Marshal(storageapi.FileInfo{Name: "a.demo", Size: 4})
}

func TestTheCoreAsksAndTheModuleAnswersThroughTheHandle(t *testing.T) {
	t.Parallel()

	var saw int64

	module := moduleSide{handler: fileapi.Serve(storageapi.NewClient(statOnly{}),
		func(_ context.Context, params fileapi.Params, file *storageapi.File) (fileapi.Analysis, error) {
			saw = file.Size()

			return fileapi.Analysis{Title: "from " + params.Name}, nil
		})}

	answered, err := fileapi.Ask(t.Context(), module, fileapi.Params{Handle: "h1", Name: "a.demo", Size: 4})
	if err != nil || answered.Title != "from a.demo" || saw != 4 {
		t.Fatalf("the answer was %+v, %v, and the module saw a file of %d bytes", answered, err, saw)
	}

	if _, err = fileapi.Ask(
		t.Context(),
		module,
		fileapi.Params{Handle: "h2", Name: "a.demo"},
	); !errors.Is(
		err,
		storageapi.ErrNoFile,
	) {
		t.Fatalf("a handle the host did not give answered %v", err)
	}

	if _, err = fileapi.Ask(
		t.Context(),
		module,
		fileapi.Params{Name: "a.demo"},
	); !errors.Is(
		err,
		fileapi.ErrOutOfBounds,
	) {
		t.Fatalf("a call with no handle answered %v", err)
	}
}

func TestAModuleAnswerOverABoundNeverLeavesTheModule(t *testing.T) {
	t.Parallel()

	module := moduleSide{handler: fileapi.Serve(storageapi.NewClient(statOnly{}),
		func(context.Context, fileapi.Params, *storageapi.File) (fileapi.Analysis, error) {
			return fileapi.Analysis{Title: strings.Repeat("x", fileapi.MaxTextBytes+1)}, nil
		})}

	if _, err := fileapi.Ask(
		t.Context(),
		module,
		fileapi.Params{Handle: "h1", Name: "a.demo"},
	); !errors.Is(
		err,
		fileapi.ErrOutOfBounds,
	) {
		t.Fatalf("an answer over the bound crossed as %v", err)
	}
}
