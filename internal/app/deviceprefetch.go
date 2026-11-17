package app

// The thumbnail walk: after a printer lists a directory, this server asks it
// for the picture of each print file there, one file at a time, while the
// printer is idle. A printer answers one command at a time and takes about a
// second per picture, so a person who opens the folder later finds the
// pictures already held instead of waiting for each row in turn.

import (
	"context"
	"path"
	"slices"
	"strings"

	"github.com/prusa3d/prusa-connect/internal/kernel/lane"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// printFileExtensions are the files a printer reads a picture out of. The web
// app asks for a thumbnail of the same set.
//
//nolint:gochecknoglobals // a fixed set, read-only after init.
var printFileExtensions = []string{".bgcode", ".gcode", ".gco", ".g"}

// prefetch starts the thumbnail walk over one directory's files, in place of
// any walk already running for this printer.
//
// The new walk waits for the old one to return before it asks for anything,
// so the printer has at most one prefetch in front of a person's own request.
func (d *devices) prefetch(entry *device, files []connectorapi.File) {
	wanted := printFiles(files)

	// A plane that is closing starts nothing Close would have to wait for.
	if len(wanted) == 0 || d.life.Err() != nil {
		return
	}

	ctx, cancel := context.WithCancel(d.life)
	done := make(chan struct{})

	entry.mu.Lock()
	previous, previousDone := entry.stopPrefetching, entry.prefetched
	entry.stopPrefetching, entry.prefetched = cancel, done
	entry.mu.Unlock()

	if previous != nil {
		previous()
	}

	d.walks.Go(func() {
		defer close(done)
		defer cancel()

		if previousDone != nil {
			select {
			case <-previousDone:
			case <-ctx.Done():
				return
			}
		}

		d.walk(ctx, entry, wanted)
	})
}

// walk asks the printer about each file whose picture is not held, and stops
// as soon as the printer is busy, gone, or the walk is cancelled.
func (d *devices) walk(ctx context.Context, entry *device, files []connectorapi.File) {
	for _, file := range files {
		if ctx.Err() != nil || !entry.session.Connected() || !entry.idle() {
			return
		}

		key := previewKey{device: entry.id, path: file.Path, size: file.Size, modified: file.Modified}
		if _, held := d.previews.get(key); held || d.thumbnails.has(key) {
			continue
		}

		// A file the printer will not describe is skipped. A person who opens
		// it asks again and is told why.
		_, _, _ = d.readThumbnail(lane.WithBackground(ctx, lane.NewBackground()), entry, file.Path, key)
	}
}

// printFiles are the files in a listing a printer reads a picture out of.
func printFiles(files []connectorapi.File) []connectorapi.File {
	out := make([]connectorapi.File, 0, len(files))

	for _, file := range files {
		if !file.Folder && file.Size > 0 &&
			slices.Contains(printFileExtensions, strings.ToLower(path.Ext(file.Path))) {
			out = append(out, file)
		}
	}

	return out
}

// stopPrefetch ends this printer's thumbnail walk, if one is running. A print
// start, a transfer, a lost connection and a retired printer call it.
func (e *device) stopPrefetch() {
	e.mu.Lock()
	stop := e.stopPrefetching
	e.mu.Unlock()

	if stop != nil {
		stop()
	}
}

// idle reports whether the printer says it is doing nothing a request for a
// picture could slow down: it has reported a state, and that state is neither
// a print nor a long move.
func (e *device) idle() bool {
	e.mu.Lock()
	state := jobText(e.state[printerStatePath])
	e.mu.Unlock()

	return state != "" && state != stateBusy && !slices.Contains(jobRunningStates, state)
}
