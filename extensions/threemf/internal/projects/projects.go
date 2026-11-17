// Package projects reads 3MF projects out of the server's storages and keeps
// what it read.
//
// A project is a zip whose model must be read whole to place one instance, so
// a read is costly. The answer for one version of one file is kept in memory,
// keyed by the file's length and modification time, and one read is shared by
// every caller asking for the same file. At most two projects are read at once,
// because a read can hold a few hundred megabytes for the largest file the
// limits let through. Nothing is written to disk.
//
// A file is read through the storage wire as the person the host named, so
// the storage decides whether that person may read it. The cache is shared
// between people only after that check: every caller stats the file as
// themselves first, and only the stat's answer names the key.
package projects

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
	"time"
	"unsafe"

	"github.com/prusa3d/prusa-connect/sdk/storageapi"

	"github.com/prusa3d/threemf/internal/blockread"
	"github.com/prusa3d/threemf/threemf"
)

// The bounds of what is held in memory. A project keeps the outline of every
// instance and the pictures, so few are kept. A page opens one object at a
// time, and a person looks at a few objects of one project.
const (
	projectEntries = 8
	projectBytes   = 64 << 20
	meshEntries    = 16
	meshBytes      = 32 << 20
)

// readers is how many projects are read at once.
const readers = 2

// readTimeout bounds one read. The read is shared by every caller that asks
// for the same file, so it does not end when the first of them leaves.
const readTimeout = 2 * time.Minute

// MeshTriangles is the most triangles one mesh is sent with. It keeps the
// answer near half a megabyte of JSON, which a phone draws at once and which
// shows the shape of an object a slicer would print.
const MeshTriangles = 20_000

// maxThumbnailBytes is the largest picture kept, the bound the core holds a
// print file's picture to. A route answers at most 1 MiB, so it also fits.
const maxThumbnailBytes = 1 << 20

// maxThumbnails is the most pictures kept. An OrcaSlicer project carries one
// picture per plate, and a project of twenty plates is common.
const maxThumbnails = 64

// Limits are the bounds of one read. Every read of this module takes them, so
// a file the routes refuse is refused for the core too.
func Limits() threemf.Limits {
	return threemf.Limits{MaxThumbnails: maxThumbnails, MaxThumbnailBytes: maxThumbnailBytes}
}

// The refusals of this package. A storage refusal comes through as the
// storageapi sentinel.
var (
	// ErrNotAProject is a file whose name does not end in .3mf, a directory,
	// and a file the reader refuses as a format.
	ErrNotAProject = errors.New("projects: not a 3MF project")
	// ErrTooLarge is a file past the reader's limits. It wraps the
	// *threemf.LimitError that names the limit.
	ErrTooLarge = errors.New("projects: the project is larger than this module reads")
	// ErrNoObject is an object index the project does not have.
	ErrNoObject = errors.New("projects: no such object")
)

// File is one open file of a storage.
type File interface {
	io.ReaderAt
	Size() int64
}

// Files is the storage wire as this package reads it.
type Files interface {
	Stat(ctx context.Context, params storageapi.StatParams) (storageapi.FileInfo, error)
	Open(ctx context.Context, target storageapi.Target) (File, error)
}

// Wire adapts the module's storage client onto Files.
func Wire(client *storageapi.Client) Files { return wire{client: client} }

type wire struct{ client *storageapi.Client }

func (w wire) Stat(ctx context.Context, params storageapi.StatParams) (storageapi.FileInfo, error) {
	info, err := w.client.Stat(ctx, params)
	if err != nil {
		return storageapi.FileInfo{}, fmt.Errorf("stat: %w", err)
	}

	return info, nil
}

func (w wire) Open(ctx context.Context, target storageapi.Target) (File, error) {
	file, err := w.client.Open(ctx, target)
	if err != nil {
		return nil, fmt.Errorf("open: %w", err)
	}

	return file, nil
}

// Request names one file, read as one person.
type Request struct {
	Principal string
	Storage   string
	Path      string
}

// Reader reads projects and meshes, and keeps them.
type Reader struct {
	files    Files
	projects *cache[projectRead]
	meshes   *cache[meshRead]
	slots    chan struct{}
}

type projectRead struct {
	project *threemf.Project
	err     error
}

type meshRead struct {
	mesh *threemf.Mesh
	err  error
}

// New is a Reader over files.
func New(files Files) *Reader {
	return &Reader{
		files:    files,
		projects: newCache(projectEntries, projectBytes, sizeOfProject, keptProject),
		meshes:   newCache(meshEntries, meshBytes, sizeOfMesh, keptMesh),
		slots:    make(chan struct{}, readers),
	}
}

// A read stopped by its deadline or by the caller says nothing about the file,
// so it is not kept and the next caller reads again. Nor is a refusal: it is
// the answer for the person the read ran as, and the read runs past their
// request, so the host refuses it once that request has ended.
func keptProject(read projectRead) bool { return kept(read.err) }
func keptMesh(read meshRead) bool       { return kept(read.err) }

func kept(err error) bool {
	return !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) &&
		!errors.Is(err, storageapi.ErrDenied)
}

// Stat describes the file as the person, and refuses a file that cannot be a
// project before anything is read. A G-code file of a gigabyte is not read
// to learn it is not a zip.
func (r *Reader) Stat(ctx context.Context, req Request) (storageapi.FileInfo, error) {
	info, err := r.files.Stat(ctx, storageapi.StatParams{
		Principal: req.Principal, Storage: req.Storage, Path: req.Path,
	})
	if err != nil {
		return storageapi.FileInfo{}, err
	}

	if info.IsDir || !strings.EqualFold(path.Ext(req.Path), ".3mf") {
		return storageapi.FileInfo{}, fmt.Errorf("%w: %s", ErrNotAProject, req.Path)
	}

	return info, nil
}

// Project reads one project, or answers the one already read for this version
// of the file. It answers the stat too, which names the version.
func (r *Reader) Project(ctx context.Context, req Request) (*threemf.Project, storageapi.FileInfo, error) {
	info, err := r.Stat(ctx, req)
	if err != nil {
		return nil, storageapi.FileInfo{}, err
	}

	k := key{storage: req.Storage, path: info.Path, size: info.Size, modified: info.MTime, object: -1}

	ran := false
	own := func() projectRead {
		ran = true

		var project *threemf.Project

		readErr := r.whole(ctx, req, info, func(readCtx context.Context, at io.ReaderAt) error {
			var failed error

			project, failed = threemf.Read(readCtx, at, info.Size, Limits())

			return failed
		})

		return projectRead{project: project, err: readErr}
	}

	read := r.projects.once(ctx, k, own)

	// Another caller's read was refused as that caller. This caller's own
	// stat passed, so it reads as itself.
	if !ran && errors.Is(read.err, storageapi.ErrDenied) {
		read = own()
	}

	if read.err == nil && read.project == nil {
		// The caller left while another caller's read was running.
		return nil, storageapi.FileInfo{}, ctx.Err()
	}

	if read.err != nil {
		return nil, storageapi.FileInfo{}, classify(read.err)
	}

	return read.project, info, nil
}

// Mesh reads the model parts of one object as at most MeshTriangles
// triangles. A mesh read is a whole read of the file, so it takes a slot of
// the project reads.
func (r *Reader) Mesh(ctx context.Context, req Request, object int) (*threemf.Mesh, error) {
	info, err := r.Stat(ctx, req)
	if err != nil {
		return nil, err
	}

	k := key{storage: req.Storage, path: info.Path, size: info.Size, modified: info.MTime, object: object}

	ran := false
	own := func() meshRead {
		ran = true

		var mesh *threemf.Mesh

		readErr := r.whole(ctx, req, info, func(readCtx context.Context, at io.ReaderAt) error {
			var failed error

			mesh, failed = threemf.ReadMesh(readCtx, at, info.Size, Limits(), object, MeshTriangles)

			return failed
		})

		return meshRead{mesh: mesh, err: readErr}
	}

	read := r.meshes.once(ctx, k, own)

	// As in Project: another caller's refused read is not this caller's.
	if !ran && errors.Is(read.err, storageapi.ErrDenied) {
		read = own()
	}

	if read.err == nil && read.mesh == nil {
		return nil, ctx.Err()
	}

	if read.err != nil {
		return nil, classify(read.err)
	}

	return read.mesh, nil
}

// Analyze reads a file the host opened for a question of the core. It takes
// a slot like any other read, and is not kept: the host keeps the answer.
func (r *Reader) Analyze(ctx context.Context, file File) (*threemf.Project, error) {
	if err := r.take(ctx); err != nil {
		return nil, err
	}
	defer r.give()

	project, err := threemf.Read(ctx, blockread.New(file, file.Size(), 0, 0), file.Size(), Limits())
	if err != nil {
		return nil, classify(err)
	}

	return project, nil
}

// whole opens the file as the person and runs read over it, under its own
// deadline and in a slot. The read runs under its own deadline and not under
// the caller's, because other callers asking for the same file wait on it.
func (r *Reader) whole(
	ctx context.Context, req Request, info storageapi.FileInfo,
	read func(context.Context, io.ReaderAt) error,
) error {
	readCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), readTimeout)
	defer cancel()

	if err := r.take(ctx); err != nil {
		return err
	}
	defer r.give()

	file, err := r.files.Open(readCtx, storageapi.Target{
		Principal: req.Principal, Storage: req.Storage, Path: info.Path,
	})
	if err != nil {
		return err
	}

	return read(readCtx, blockread.New(file, file.Size(), 0, 0))
}

func (r *Reader) take(ctx context.Context) error {
	select {
	case r.slots <- struct{}{}:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("wait for a read slot: %w", ctx.Err())
	}
}

func (r *Reader) give() { <-r.slots }

// classify maps what stopped a read onto this package's refusals.
func classify(err error) error {
	switch {
	case errors.Is(err, threemf.ErrNoObject):
		return fmt.Errorf("%w: %w", ErrNoObject, err)
	case errors.Is(err, threemf.ErrLimit):
		return fmt.Errorf("%w: %w", ErrTooLarge, err)
	case errors.Is(err, threemf.ErrFormat):
		return fmt.Errorf("%w: %w", ErrNotAProject, err)
	}

	return err
}

// sizeOfProject is how much memory one kept project holds, near enough to
// bound the cache: the outlines, the names, the settings and the pictures.
func sizeOfProject(k key, read projectRead) int {
	held := k.bytes() + int(unsafe.Sizeof(read))

	project := read.project
	if project == nil {
		return held
	}

	const point = int(unsafe.Sizeof(threemf.Point{}))

	for _, object := range project.Objects {
		held += int(unsafe.Sizeof(object)) + len(object.Name)

		for _, part := range object.Parts {
			held += int(unsafe.Sizeof(part)) + len(part.Name)
		}

		for _, instance := range object.Instances {
			held += int(unsafe.Sizeof(instance)) + len(instance.Outline)*point
		}
	}

	for _, setup := range project.Setups {
		held += int(unsafe.Sizeof(setup)) + len(setup.Beds)*int(unsafe.Sizeof(threemf.Bed{}))

		for _, group := range setup.Settings {
			for name, value := range group.Values {
				held += len(name) + len(value)
			}
		}
	}

	for _, picture := range project.Thumbnails {
		held += int(unsafe.Sizeof(picture)) + len(picture.Data)
	}

	for _, entry := range project.Metadata {
		held += len(entry.Name) + len(entry.Value)
	}

	return held
}

// sizeOfMesh is how much memory one kept mesh holds.
func sizeOfMesh(k key, read meshRead) int {
	held := k.bytes() + int(unsafe.Sizeof(read))
	if read.mesh == nil {
		return held
	}

	const word = 4

	held += len(read.mesh.Vertices)*word + len(read.mesh.Triangles)*word

	for _, part := range read.mesh.Parts {
		held += int(unsafe.Sizeof(part)) + len(part.Name)
	}

	return held
}
