// Package fileapi is how the core asks a module about one file.
//
// A module names the file types it reads under file_analysis in its manifest.
// When the core needs such a file described, it calls MethodAnalyze on that
// module with a read handle for that one file. The module reads the file with
// storageapi.Client.Open, which needs no read_storage permission, and answers
// an Analysis.
//
// When the module is not serving, fails, answers out of bounds or takes too
// long, the core reads the file itself.
package fileapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/prusa3d/prusa-connect/sdk/storageapi"
	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

// MethodAnalyze is the call the core makes on a module to describe one file.
const MethodAnalyze wirerpc.Method = "file.analyze"

// The bounds of an Analysis, checked on both sides. An answer over one is a
// failure, and the core reads the file itself.
const (
	// MaxThumbnails is how many pictures one answer may have.
	MaxThumbnails = 4
	// MaxThumbnailBytes is how many encoded picture bytes one answer may
	// have, every picture together. It keeps the answer inside one control
	// frame.
	MaxThumbnailBytes = 512 << 10
	// MaxFacts is how many facts one answer lists.
	MaxFacts = 64
	// MaxObjects is how many objects one answer lists.
	MaxObjects = 256
	// MaxTextBytes bounds a title, a label, a value and a name.
	MaxTextBytes = 256
	// MaxKeyBytes bounds the key of a fact and the id of an object.
	MaxKeyBytes = 63
	// MaxNameBytes bounds the name of the file a call describes.
	MaxNameBytes = 1024
	// MaxFilaments is how many filaments, nozzle checks and firmware
	// features the print part of one answer lists, each list on its own.
	MaxFilaments = 64
	// MaxOutlinePoints is how many points the outline of one object in the
	// print part has.
	MaxOutlinePoints = 1024
)

// The media types a picture may be. The core decodes every picture again
// before it serves it.
const (
	MediaPNG  = "image/png"
	MediaJPEG = "image/jpeg"
)

// ErrOutOfBounds reports an answer or a call outside the bounds of this
// package.
var ErrOutOfBounds = errors.New("fileapi: outside the published bounds")

// Params is the params of MethodAnalyze.
type Params struct {
	// Handle is the read handle for the file, valid while this call is
	// open. It is what storageapi.Target.Handle takes.
	Handle string `json:"handle"`
	// Name is the file's name, its last path segment.
	Name string `json:"name"`
	// Size is the file's length in bytes.
	Size int64 `json:"size"`
	// Modified is when the file last changed, in Unix seconds.
	Modified int64 `json:"modified"`
}

// Validate checks a call against the bounds of this package.
func (p Params) Validate() error {
	switch {
	case p.Handle == "" || len(p.Handle) > storageapi.MaxHandleBytes:
		return fmt.Errorf("%w: the handle is %d bytes", ErrOutOfBounds, len(p.Handle))
	case p.Name == "" || len(p.Name) > MaxNameBytes:
		return fmt.Errorf("%w: the name is %d bytes", ErrOutOfBounds, len(p.Name))
	case p.Size < 0:
		return fmt.Errorf("%w: the size is %d", ErrOutOfBounds, p.Size)
	}

	return nil
}

// Analysis is what a module answers about one file. Every member is optional.
type Analysis struct {
	// Title is what a person calls the file, such as the project name the
	// file stores. Empty is the file's own name.
	Title string `json:"title,omitempty"`
	// Thumbnails is the pictures of the file. The core serves the largest
	// one that is no longer than 1024 pixels on its long edge.
	Thumbnails []Thumbnail `json:"thumbnails,omitempty"`
	// Print is what a printer needs to print the file. Nil is a file that
	// says nothing about a print.
	Print *storageapi.PrintInfo `json:"print,omitempty"`
	// Facts is what the file page lists about the file, in order.
	Facts []Fact `json:"facts,omitempty"`
	// Objects is the parts of the file, such as the models of a project.
	Objects []Object `json:"objects,omitempty"`
}

// Thumbnail is one picture of the file.
type Thumbnail struct {
	// MediaType is MediaPNG or MediaJPEG.
	MediaType string `json:"media_type"`
	// Width and Height are the size in pixels the module read.
	Width  int `json:"width"`
	Height int `json:"height"`
	// Data is the encoded picture.
	Data []byte `json:"data"`
}

// Fact is one line the file page lists: a key a program reads, a label a
// person reads and the value.
type Fact struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Value string `json:"value"`
}

// Object is one part of the file.
type Object struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Validate checks an answer against the bounds of this package.
func (a Analysis) Validate() error {
	if len(a.Title) > MaxTextBytes {
		return fmt.Errorf("%w: the title is %d bytes", ErrOutOfBounds, len(a.Title))
	}

	if err := validThumbnails(a.Thumbnails); err != nil {
		return err
	}

	if len(a.Facts) > MaxFacts {
		return fmt.Errorf("%w: %d facts, and the limit is %d", ErrOutOfBounds, len(a.Facts), MaxFacts)
	}

	for index, fact := range a.Facts {
		if fact.Key == "" || len(fact.Key) > MaxKeyBytes || len(fact.Label) > MaxTextBytes ||
			len(fact.Value) > MaxTextBytes {
			return fmt.Errorf("%w: facts[%d] has an empty key or a text over %d bytes",
				ErrOutOfBounds, index, MaxTextBytes)
		}
	}

	if err := validPrint(a.Print); err != nil {
		return err
	}

	if len(a.Objects) > MaxObjects {
		return fmt.Errorf("%w: %d objects, and the limit is %d", ErrOutOfBounds, len(a.Objects), MaxObjects)
	}

	for index, object := range a.Objects {
		if object.ID == "" || len(object.ID) > MaxKeyBytes || len(object.Name) > MaxTextBytes {
			return fmt.Errorf("%w: objects[%d] has an empty id or a text over %d bytes",
				ErrOutOfBounds, index, MaxTextBytes)
		}
	}

	return nil
}

// validPrint checks the print part against the same bounds, because every
// text in it comes from the file.
func validPrint(info *storageapi.PrintInfo) error {
	if info == nil {
		return nil
	}

	texts := []string{
		info.Material, info.PrinterModel, info.PrintProfile, info.PrinterProfile, info.FilamentProfile,
	}

	if len(info.Filaments) > MaxFilaments {
		return fmt.Errorf("%w: %d filaments, and the limit is %d", ErrOutOfBounds, len(info.Filaments), MaxFilaments)
	}

	for _, filament := range info.Filaments {
		texts = append(texts, filament.Material, filament.Colour)
	}

	if checks := info.Checks; checks != nil {
		if len(checks.Nozzles) > MaxFilaments || len(checks.Features) > MaxFilaments {
			return fmt.Errorf("%w: the checks list over %d nozzles or features", ErrOutOfBounds, MaxFilaments)
		}

		texts = append(texts, checks.PrinterModel, checks.MinFirmware, checks.Firmware)
		texts = append(texts, checks.Features...)
	}

	if len(info.Objects) > MaxObjects {
		return fmt.Errorf("%w: the print part lists %d objects, and the limit is %d",
			ErrOutOfBounds, len(info.Objects), MaxObjects)
	}

	for _, object := range info.Objects {
		if len(object.Outline) > MaxOutlinePoints {
			return fmt.Errorf("%w: an object's outline has %d points, and the limit is %d",
				ErrOutOfBounds, len(object.Outline), MaxOutlinePoints)
		}

		texts = append(texts, object.Name)
	}

	for _, text := range texts {
		if len(text) > MaxTextBytes {
			return fmt.Errorf("%w: the print part has a text over %d bytes", ErrOutOfBounds, MaxTextBytes)
		}
	}

	return nil
}

func validThumbnails(pictures []Thumbnail) error {
	if len(pictures) > MaxThumbnails {
		return fmt.Errorf("%w: %d pictures, and the limit is %d", ErrOutOfBounds, len(pictures), MaxThumbnails)
	}

	total := 0

	for index, picture := range pictures {
		if picture.MediaType != MediaPNG && picture.MediaType != MediaJPEG {
			return fmt.Errorf("%w: thumbnails[%d] is %q, expected %s or %s",
				ErrOutOfBounds, index, picture.MediaType, MediaPNG, MediaJPEG)
		}

		if len(picture.Data) == 0 || picture.Width <= 0 || picture.Height <= 0 {
			return fmt.Errorf("%w: thumbnails[%d] is empty or has no size", ErrOutOfBounds, index)
		}

		total += len(picture.Data)
	}

	if total > MaxThumbnailBytes {
		return fmt.Errorf(
			"%w: the pictures are %d bytes, and the limit is %d",
			ErrOutOfBounds,
			total,
			MaxThumbnailBytes,
		)
	}

	return nil
}

// Analyze is the function a module writes to describe one file. file is
// already open and reads that one file.
type Analyze func(ctx context.Context, params Params, file *storageapi.File) (Analysis, error)

// Serve adapts an Analyze onto MethodAnalyze. files is the module's storage
// client over the same session. An answer over a bound is refused here.
//
//	wirerpc.Methods{fileapi.MethodAnalyze: fileapi.Serve(files, analyze)}
func Serve(files *storageapi.Client, analyze Analyze) wirerpc.Handler {
	return func(ctx context.Context, call wirerpc.Call) (any, error) {
		var params Params
		if err := json.Unmarshal(call.Params, &params); err != nil {
			return nil, &wirerpc.Error{
				Code:    wirerpc.CodeInvalidParams,
				Message: string(MethodAnalyze) + " carries a params object",
			}
		}

		if err := params.Validate(); err != nil {
			return nil, &wirerpc.Error{Code: wirerpc.CodeInvalidParams, Message: err.Error()}
		}

		file, err := files.Open(ctx, storageapi.Target{Handle: params.Handle})
		if err != nil {
			return nil, fmt.Errorf("fileapi: open the file: %w", err)
		}

		answered, err := analyze(ctx, params, file)
		if err != nil {
			return nil, err
		}

		if err = answered.Validate(); err != nil {
			return nil, err
		}

		return answered, nil
	}
}

// Caller is the part of a session the core needs to ask a module.
type Caller interface {
	Call(ctx context.Context, method wirerpc.Method, params any) (json.RawMessage, error)
}

// Ask makes the call from the core's side and validates the answer.
func Ask(ctx context.Context, module Caller, params Params) (Analysis, error) {
	if err := params.Validate(); err != nil {
		return Analysis{}, err
	}

	raw, err := module.Call(ctx, MethodAnalyze, params)
	if err != nil {
		return Analysis{}, fmt.Errorf("fileapi: %s: %w", MethodAnalyze, err)
	}

	var answered Analysis
	if err = json.Unmarshal(raw, &answered); err != nil {
		return Analysis{}, fmt.Errorf("fileapi: %s: read the answer: %w", MethodAnalyze, err)
	}

	if err = answered.Validate(); err != nil {
		return Analysis{}, err
	}

	return answered, nil
}
