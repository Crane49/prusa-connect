package store

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"time"
)

// Purged is what one purge took from one stream.
type Purged struct {
	Records  int64 `json:"records"`
	Bytes    int64 `json:"bytes"`
	Segments int   `json:"segments"`
}

// Purge removes the records of the streams named that arrived before cutoff,
// and every record when cutoff is zero. A segment wholly before cutoff is
// removed, and the segment that holds it is written again with the newer
// records only, under a temporary name renamed into place, so a failure
// leaves the old file whole.
//
// The open segment is closed first, and the next record opens a new one
// beside it. The files are then rewritten without the store's lock, so the
// receiver keeps appending while a purge reads and writes megabytes. A purge
// changes the generation, which refuses every cursor written before it.
func (s *Store) Purge(streams []Stream, cutoff time.Time) (map[Stream]Purged, error) {
	s.purging.Lock()
	defer s.purging.Unlock()

	work, errs, err := s.startPurge(streams)
	if err != nil {
		return nil, err
	}

	out := map[Stream]Purged{}

	for _, stream := range streams {
		held := work[stream]

		purged, purgeErr := purgeStream(held, cutoff)
		out[stream] = purged

		if purgeErr != nil {
			errs = append(errs, fmt.Errorf("purge %s: %w", stream, purgeErr))
		}
	}

	s.endPurge(work)

	return out, errors.Join(errs...)
}

// segments is a copy of one stream's closed segments that a purge works on
// without the store's lock.
type segments struct {
	dir string
	// names is every segment the purge was given.
	names []string
	// sizes is what is left of each: a removed segment is not in it.
	sizes map[string]int64
}

// startPurge closes the open segment of each stream and copies the sizes of
// its segments. Every segment in the copy is closed, and an append opens a
// segment that is not in it.
func (s *Store) startPurge(streams []Stream) (map[Stream]segments, []error, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil, nil, ErrClosed
	}

	var errs []error

	work := map[Stream]segments{}

	for _, stream := range streams {
		w, ok := s.streams[stream]
		if !ok {
			return nil, nil, fmt.Errorf("store: no stream %q", stream)
		}

		if err := w.close(); err != nil {
			errs = append(errs, err)
		}

		w.name, w.hour, w.size = "", "", 0
		w.purging = true
		work[stream] = segments{dir: w.dir, names: sortedSegments(w.sizes), sizes: maps.Clone(w.sizes)}
	}

	s.generation.Add(1)

	return work, errs, nil
}

// endPurge writes what the purge left back into the sizes. A segment the purge
// removed goes, and one it rewrote takes its new size.
func (s *Store) endPurge(work map[Stream]segments) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for stream, held := range work {
		w := s.streams[stream]
		w.purging = false

		for _, name := range held.names {
			if size, kept := held.sizes[name]; kept {
				w.sizes[name] = size
			} else {
				delete(w.sizes, name)
			}
		}
	}

	s.generation.Add(1)
}

// purgeStream purges one stream's segments, oldest first, and leaves in
// w.sizes what is left of each.
func purgeStream(w segments, cutoff time.Time) (Purged, error) {
	var out Purged

	for _, name := range w.names {
		start, ok := segmentHour(name)
		whole := cutoff.IsZero() || (ok && !start.Add(time.Hour).After(cutoff))

		// A segment that starts at the cutoff or later holds nothing to
		// remove. A clock that stepped back can put an older hour after it,
		// so the loop goes on.
		if !whole && ok && !start.Before(cutoff) {
			continue
		}

		var (
			took Purged
			err  error
		)

		if whole {
			took, err = removeSegment(w, name)
		} else {
			took, err = cutSegment(w, name, cutoff)
		}

		out.Records += took.Records
		out.Bytes += took.Bytes
		out.Segments += took.Segments

		if err != nil {
			return out, err
		}
	}

	return out, nil
}

// removeSegment removes one segment whole.
func removeSegment(w segments, name string) (Purged, error) {
	path := filepath.Join(w.dir, name)

	lines, err := countLines(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return Purged{}, err
	}

	if err = os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return Purged{}, fmt.Errorf("remove %s: %w", name, err)
	}

	took := Purged{Records: lines, Bytes: w.sizes[name], Segments: 1}
	delete(w.sizes, name)

	return took, nil
}

// cutSegment keeps the records of one segment from cutoff on, and removes
// the segment when none is left.
func cutSegment(w segments, name string, cutoff time.Time) (Purged, error) {
	kept, removed, freed, err := rewriteFrom(filepath.Join(w.dir, name), cutoff)
	if err != nil {
		return Purged{}, err
	}

	if kept > 0 {
		w.sizes[name] -= freed

		return Purged{Records: removed, Bytes: freed}, nil
	}

	took, err := removeSegment(w, name)
	took.Records = removed

	return took, err
}

// countLines counts the records of one segment.
func countLines(path string) (int64, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return 0, fmt.Errorf("read %s: %w", filepath.Base(path), err)
	}

	return int64(bytes.Count(content, []byte{'\n'})), nil
}

// rewriteFrom keeps the records of one segment that arrived at cutoff or
// later, and answers how many it kept, how many it removed and the bytes it
// freed. A line it cannot read is kept, because it is evidence.
func rewriteFrom(path string, cutoff time.Time) (int64, int64, int64, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("read %s: %w", filepath.Base(path), err)
	}

	var (
		kept    bytes.Buffer
		lines   int64
		removed int64
	)

	scanner := bufio.NewScanner(bytes.NewReader(content))
	scanner.Buffer(make([]byte, readBuffer), MaxSegmentBytes)

	for scanner.Scan() {
		line := scanner.Bytes()

		var at struct {
			At time.Time `json:"t"`
		}

		if json.Unmarshal(line, &at) == nil && at.At.Before(cutoff) {
			removed++

			continue
		}

		kept.Write(line)
		kept.WriteByte('\n')
		lines++
	}

	if err = scanner.Err(); err != nil {
		return 0, 0, 0, fmt.Errorf("read %s: %w", filepath.Base(path), err)
	}

	if removed == 0 {
		return lines, 0, 0, nil
	}

	if lines > 0 {
		if err = replaceFile(path, kept.Bytes()); err != nil {
			return 0, 0, 0, err
		}
	}

	return lines, removed, int64(len(content) - kept.Len()), nil
}

// replaceFile writes content beside path and renames it over path, so a
// failure partway leaves the old file whole. Close is checked: on FAT and USB
// a full disk is reported there and nowhere else.
func replaceFile(path string, content []byte) error {
	temp, err := os.CreateTemp(filepath.Dir(path), ".purge-*")
	if err != nil {
		return fmt.Errorf("write %s: %w", filepath.Base(path), err)
	}

	name := temp.Name()
	_, writeErr := temp.Write(content)
	syncErr := temp.Sync()
	closeErr := temp.Close()

	if err = errors.Join(writeErr, syncErr, closeErr); err != nil {
		_ = os.Remove(name) // the original is untouched; the partial copy goes

		return fmt.Errorf("write %s: %w", filepath.Base(path), err)
	}

	if err = os.Rename(name, path); err != nil {
		_ = os.Remove(name) // the original is untouched; the copy goes

		return fmt.Errorf("replace %s: %w", filepath.Base(path), err)
	}

	return nil
}
