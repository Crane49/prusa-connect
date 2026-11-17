// Package blockread reads a file over the wire in large blocks.
//
// A storageapi.File makes one fetch for each ReadAt. archive/zip reads an
// entry through a small buffer, so a model entry of 50 MB would cost thousands
// of fetches of a few kilobytes each. Reader turns those small reads into
// reads of whole blocks and keeps the newest blocks, so a zip read costs about
// one fetch per block.
package blockread

import (
	"errors"
	"fmt"
	"io"
	"slices"
	"sync"
)

// The defaults New takes for a zero block size or count.
const (
	// DefaultBlockBytes is the size of one block. It is far below the 16 MiB
	// one fetch sends, so a read near the end of a file costs little more
	// than it reads.
	DefaultBlockBytes = 1 << 20
	// DefaultBlocks is how many blocks are kept. A zip read jumps between
	// the central directory at the end and the entry it inflates, so a few
	// blocks are enough.
	DefaultBlocks = 8
)

// ErrNegativeOffset refuses a read before the start of the file.
var ErrNegativeOffset = errors.New("blockread: negative offset")

// Reader is an io.ReaderAt over src that reads whole blocks. It is safe for
// concurrent use.
type Reader struct {
	src        io.ReaderAt
	size       int64
	blockBytes int64
	maxBlocks  int

	mu     sync.Mutex
	blocks map[int64][]byte
	// order is the start of each kept block, the least recently used first.
	order []int64
}

// New reads src, which is size bytes long, in blocks of blockBytes and keeps
// at most blocks of them. A zero for either takes the default.
func New(src io.ReaderAt, size int64, blockBytes, blocks int) *Reader {
	if blockBytes <= 0 {
		blockBytes = DefaultBlockBytes
	}

	if blocks <= 0 {
		blocks = DefaultBlocks
	}

	return &Reader{
		src: src, size: size, blockBytes: int64(blockBytes), maxBlocks: blocks,
		blocks: make(map[int64][]byte, blocks),
	}
}

// Size is the length of the file.
func (r *Reader) Size() int64 { return r.size }

// ReadAt reads len(p) bytes from off, as io.ReaderAt says.
func (r *Reader) ReadAt(p []byte, off int64) (int, error) {
	if off < 0 {
		return 0, fmt.Errorf("%w: %d", ErrNegativeOffset, off)
	}

	read := 0

	for read < len(p) {
		at := off + int64(read)
		if at >= r.size {
			return read, io.EOF
		}

		start := at - at%r.blockBytes

		block, err := r.block(start)
		if err != nil {
			return read, err
		}

		read += copy(p[read:], block[at-start:])
	}

	return read, nil
}

// block answers the block that starts at start, read now or kept from before.
func (r *Reader) block(start int64) ([]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if kept, ok := r.blocks[start]; ok {
		r.touch(start)

		return kept, nil
	}

	length := min(r.blockBytes, r.size-start)
	block := make([]byte, length)

	n, err := r.src.ReadAt(block, start)
	if n < len(block) {
		if err == nil || errors.Is(err, io.EOF) {
			err = io.ErrUnexpectedEOF
		}

		return nil, fmt.Errorf("blockread: read %d bytes at %d: %w", length, start, err)
	}

	r.blocks[start] = block
	r.order = append(r.order, start)

	if len(r.order) > r.maxBlocks {
		delete(r.blocks, r.order[0])
		r.order = r.order[1:]
	}

	return block, nil
}

// touch moves a kept block to the end of the order.
func (r *Reader) touch(start int64) {
	at := slices.Index(r.order, start)
	if at < 0 || at == len(r.order)-1 {
		return
	}

	r.order = append(slices.Delete(r.order, at, at+1), start)
}
