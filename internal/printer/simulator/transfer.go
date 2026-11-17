package simulator

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"time"

	"github.com/gorilla/websocket"
)

// The transfer wire, from the printer's side. The server tells the printer to
// fetch a file and the printer asks for the ranges it wants, so the printer is
// what decides the chunking and what decides when the file is complete.
const (
	// transferChunk is how many bytes the printer asks for in one T frame. It
	// is what shipping firmware asks for.
	transferChunk = 4096
	// transferBlock is how much of the file one block request covers. Real
	// firmware asks for the whole file in one range, so a request covers the
	// rest of the file up to this span.
	transferBlock = 1 << 18

	// maxFramePayload is the largest chunk this printer will take in one frame.
	//
	// It is the chunk size the firmware asks for. A frame that carries more of
	// the file than the printer asked for ends the connection here. Real
	// hardware takes at least what it asks for, and the host's default ceiling
	// is the requested chunk (internal/connector/xbuddy/transfer.go). No board
	// was seen to fail on a larger frame. The simulator hangs up because a host
	// that writes more than the printer asked for is out of contract, and a
	// test should catch that.
	maxFramePayload = transferChunk

	// teamZero is the tenant every transfer this server serves belongs to.
	teamZero = 0
)

// transfer is the file this printer is fetching, or nil when it is fetching
// none. A printer runs one at a time.
//
// The bytes are not kept. A simulator that held the file could not be sent a
// file larger than the process, and a transfer must be tested with one. A test
// compares the digest and the count, and both cover the whole file.
//
// Every field is read and written with Printer.mu held, the digest's own state
// included. The block request and the chunk that answers it are written by two
// goroutines, the session's resume and the read loop, and nothing else orders
// the two.
//
// A holder of this pointer does not own the transfer. Anything that ends a
// transfer clears Printer.transfer, so every step that advances one checks
// that it is still the one the printer is running. See claimBlock.
type transfer struct {
	fileID uint32
	// session is the connection this transfer was accepted on, so a resume can
	// tell a reconnection from the connection that started it.
	session uint64
	// hash is the token the server named this transfer by, kept so that a
	// transfer resumed on a later connection names itself the same way.
	hash string
	// transferID is what this printer calls the transfer in its events. It is
	// not the file id. Real firmware uses two different numbers for one
	// transfer, and a server that matched the completion on the file id would
	// never see the transfer end.
	transferID int64

	path     string
	size     int64
	received int64
	// blockEnd is the inclusive last byte of the range asked for, so a short
	// block is told from a complete one.
	blockEnd int64
	digest   hash.Hash
	// resetOwed is the connection drop this printer still owes for having
	// accepted the transfer. See ErrTransferReset.
	resetOwed bool
}

// Transfer is what a printer made of a file it was sent, for a test to compare
// against what was offered.
type Transfer struct {
	// Path is where the file landed, in the printer's own absolute spelling.
	Path string
	// Size is the length the server declared.
	Size int64
	// Received is how many bytes arrived. It equals Size on a transfer that
	// finished.
	Received int64
	// Digest is the hex SHA-256 of every byte received, in arrival order. It
	// proves the file arrived intact. It is a digest and not the bytes, so a
	// file larger than the heap can be sent.
	Digest string
	// Finished is whether the printer reported TRANSFER_FINISHED.
	Finished bool
	// Landed is whether the file is on the storage under the path it was sent
	// to. A transfer can finish without its file landing (see plant), so a
	// test reads the two apart here.
	Landed bool
}

// LastTransfer is what the printer last fetched, and false when it has fetched
// nothing.
func (p *Printer) LastTransfer() (Transfer, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.lastTransfer, p.lastTransfer.Path != ""
}

// RequestStrayBlock asks the server for a block of a transfer nobody started.
//
// It is a fault and not something a printer does on purpose. A printer that
// reconnected in the middle of a transfer, or one whose server forgot the
// transfer, puts this on the wire. The server must answer with silence. A
// server that answers with a chunk ends the connection here. See
// takeTransferChunk.
func (p *Printer) RequestStrayBlock(fileID uint32) error {
	p.mu.Lock()
	conn := p.conn
	p.mu.Unlock()

	if conn == nil {
		return errNotConnected
	}

	return p.sendJSON(conn, map[string]any{
		"transfer": transferInline,
		"file_id":  fileID,
		"chunk":    transferChunk,
		"start":    0,
		"end":      transferChunk - 1,
	})
}

// maxChunkBytes is the largest chunk this board takes in one frame:
// maxFramePayload unless Config.MaxChunkBytes names another size.
func (p *Printer) maxChunkBytes() int {
	if p.cfg.MaxChunkBytes > 0 {
		return p.cfg.MaxChunkBytes
	}

	return maxFramePayload
}

// errNotConnected is a fault asked for while the printer has no socket.
var errNotConnected = errors.New("simulator: the printer is not connected")

// ErrChunkTooLarge is a transfer chunk larger than this board can take in one
// frame. See maxFramePayload.
var ErrChunkTooLarge = errors.New("simulator: transfer chunk too large for one frame")

// ErrUnknownTransferChunk is a chunk addressed to a transfer this printer is not
// fetching. See takeTransferChunk.
var ErrUnknownTransferChunk = errors.New("simulator: transfer chunk for a transfer this printer is not fetching")

// ErrTransferReset is the connection this printer drops of its own accord
// after it accepted a transfer. The drop ends the session and starts the
// resume.
//
// Real firmware does this: an MK4S at 6.5.7+12836 resets about 100 ms after
// accepting and asks again on the next connection under a new file id. The
// drop is owed once per transfer, and any lost connection pays it.
//
// It lands on the first chunk because a synctest bubble advances no virtual
// time while bytes are moving, so a 100 ms timer would fire only after the
// transfer had finished.
var ErrTransferReset = errors.New("simulator: the printer reset its connection after accepting a transfer")

// startTransfer takes a START_INLINE_DOWNLOAD and begins fetching.
//
// The command's own terminal event says only that the printer accepted the
// transfer. The file arriving is TRANSFER_FINISHED, later. Because of that
// order a transfer is not a command.
func (p *Printer) startTransfer(conn *websocket.Conn, id uint32, command jsonCommand) error {
	path := normalisePath(pathArgument(command.Kwargs))
	size, ok := command.Kwargs["orig_size"].(float64)

	// A negative length is refused. As an int64 it would make a transfer that
	// is complete before a byte arrives, and this printer would plant a file
	// in its own tree that nothing sent.
	if !ok || path == "" || size < 0 {
		return p.reject(conn, id, ReasonInvalidArgument)
	}

	p.mu.Lock()

	if p.transfer != nil {
		p.mu.Unlock()

		return p.reject(conn, id, ReasonTransferInProgress)
	}

	// A name the storage already holds is refused, and the file there stays.
	// The firmware stats the destination before it begins and answers "File
	// already exists" (Prusa-Firmware-Buddy src/transfers/transfer.cpp,
	// Transfer::begin, and src/connect/planner.cpp, handle_transfer_result).
	if p.holdsLocked(path) {
		p.mu.Unlock()

		return p.reject(conn, id, ReasonFileExists)
	}

	// A fresh file id every time, as the firmware does: it does not reuse the
	// one from the attempt before, so a server that bound it once cannot serve
	// the retry.
	p.nextFileID += 7919
	p.nextTransferID++

	hashValue, _ := command.Kwargs["hash"].(string)

	current := &transfer{
		fileID:     p.nextFileID,
		session:    p.sessions,
		transferID: p.nextTransferID,
		hash:       hashValue,
		path:       path,
		size:       int64(size),
		digest:     sha256.New(),
		resetOwed:  true,
	}
	p.transfer = current
	p.lastTransfer = Transfer{Path: path, Size: current.size}
	answer := int(current.transferID)
	p.mu.Unlock()

	// The command is answered with TRANSFER_INFO carrying the transfer id, which
	// is the only place the printer says what its TRANSFER_ events will be
	// keyed by.
	if err := p.send(conn, event{
		Event: eventTransferInfo, CommandID: &id, TransferID: &answer, State: p.State(),
	}); err != nil {
		return err
	}

	team, _ := command.Kwargs["team_id"].(float64)

	return p.requestBlock(conn, current, hashValue, int(team))
}

// requestBlock asks for the next range the printer does not have.
//
// The first request carries the token the server named the transfer by. Every
// later one carries only the id this printer chose. The server has to answer
// both.
func (p *Printer) requestBlock(conn *websocket.Conn, current *transfer, hashValue string, teamID int) error {
	claim, state := p.claimBlock(current)

	switch state {
	case blockComplete:
		return p.finishTransfer(conn, current)
	case blockAbandoned:
		return nil
	case blockReady:
	}

	request := map[string]any{
		"transfer": transferInline,
		"file_id":  claim.fileID,
		"chunk":    transferChunk,
		"start":    claim.start,
		"end":      claim.end,
	}

	if hashValue != "" {
		request["hash"] = hashValue
		request["team_id"] = teamID
	}

	return p.sendJSON(conn, request)
}

// blockRequest is one range of the file, as the printer asks for it.
type blockRequest struct {
	fileID uint32
	start  int64
	end    int64
}

// The three answers claimBlock gives.
const (
	// blockReady is a range to ask the server for.
	blockReady = iota
	// blockComplete is a file with no byte left to fetch.
	blockComplete
	// blockAbandoned is a transfer that ended while the caller was holding it,
	// which a stop, an abort and a finish all do. Asking for a range would make
	// the server serve a transfer the printer has already declared over, and
	// the answer would arrive under a file id nothing is fetching.
	blockAbandoned
)

// claimBlock takes the next range this printer does not have.
func (p *Printer) claimBlock(current *transfer) (blockRequest, int) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.transfer != current {
		return blockRequest{}, blockAbandoned
	}

	if current.received >= current.size {
		return blockRequest{}, blockComplete
	}

	start := current.received
	end := min(start+transferBlock, current.size) - 1
	current.blockEnd = end

	return blockRequest{fileID: current.fileID, start: start, end: end}, blockReady
}

// resumeTransfer asks again for the file this printer was fetching when its
// last connection ended.
//
// A transfer outlives a connection here because it does on the machine, where
// the printer drops one of its own accord after accepting. See
// ErrTransferReset. A server that bound the first file id and refused the rest
// would serve a transfer that can never finish.
func (p *Printer) resumeTransfer(conn *websocket.Conn) error {
	current, hash, fetching := p.beginResume()
	if !fetching {
		return nil
	}

	if p.park(conn, current, hash) {
		return nil
	}

	return p.requestBlock(conn, current, hash, teamZero)
}

// beginResume mints the file id the next connection asks under and spends the
// drop the printer owed, whatever ended the connection before it. fetching is
// false when there is no transfer to resume.
//
// A transfer accepted on this same connection is not one to resume. The
// session starts its resume and its read loop together. Without this check, a
// transfer that arrives while the resume is being scheduled would get a new
// file id after its block request had gone out, and would spend a drop that
// never happened.
func (p *Printer) beginResume() (current *transfer, hash string, fetching bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.transfer == nil || p.transfer.session == p.sessions {
		return nil, "", false
	}

	p.nextFileID += 7919
	p.transfer.fileID = p.nextFileID
	p.transfer.session = p.sessions
	p.transfer.resetOwed = false

	if p.cfg.RestartsTransfers {
		p.transfer.received = 0
		p.transfer.digest.Reset()
	}

	return p.transfer, p.transfer.hash, true
}

// takeTransferChunk reads one T frame.
//
// An empty payload is the server saying it cannot serve the block. It is the
// one failure signal the wire has, and the printer then abandons the transfer.
//
// A chunk addressed to a transfer this printer is not fetching ends the
// connection. What a board does with one is not known. A board does reset
// within ten milliseconds of an empty chunk for a transfer it is opening, so
// this frame is treated the same way. The server must answer a request it has
// nothing for with silence, and a server that answers with a chunk fails a
// test here.
func (p *Printer) takeTransferChunk(conn *websocket.Conn, f frame) error {
	current, fetching := p.transferFor(f.id)
	if !fetching {
		return fmt.Errorf("%w: file_id %d", ErrUnknownTransferChunk, f.id)
	}

	if len(f.payload) == 0 {
		return p.abortTransfer(conn, current)
	}

	if len(f.payload) > p.maxChunkBytes() {
		// A host that writes more than the printer asked for is out of
		// contract. The simulator hangs up, as a board that resets would.
		return fmt.Errorf("%w: %d bytes in one chunk, and this board takes %d",
			ErrChunkTooLarge, len(f.payload), p.maxChunkBytes())
	}

	received, blockDone, resetOwed := p.accountChunk(current, f.payload)

	// The bytes are counted before the drop, because they arrived: the resume
	// asks from here and the file is whole across the two connections.
	if resetOwed {
		return fmt.Errorf("%w: %d bytes in", ErrTransferReset, received)
	}

	if blockDone {
		return p.nextBlock(conn, current)
	}

	return nil
}

// parkedBlock is a block request a held transfer owes, with the connection it
// is owed on.
type parkedBlock struct {
	conn    *websocket.Conn
	current *transfer
	hash    string
}

// HoldTransfer stops the transfer at the end of the block it is fetching. The
// printer asks for no other block until ReleaseTransfer, and a resume on a new
// connection waits too. The connection stays up, so the server sees a transfer
// that is on its way and not moving.
//
// A test calls it to end the server's side of a transfer part way, by a caller
// that leaves or a person that stops it, at a point the test chose.
func (p *Printer) HoldTransfer() {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.transferHeld = true
}

// ReleaseTransfer lets a held transfer ask for its next block again.
func (p *Printer) ReleaseTransfer() error {
	p.mu.Lock()
	p.transferHeld = false
	parked := p.parked
	p.parked = nil
	p.mu.Unlock()

	if parked == nil {
		return nil
	}

	return p.requestBlock(parked.conn, parked.current, parked.hash, teamZero)
}

// TransferWaiting reports whether a held transfer has reached the end of its
// block and is waiting for ReleaseTransfer.
func (p *Printer) TransferWaiting() bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.parked != nil
}

// nextBlock asks for the block after the one that just arrived: at once, after
// the wait TransferRate asks for, or once a held transfer is released.
func (p *Printer) nextBlock(conn *websocket.Conn, current *transfer) error {
	if p.park(conn, current, "") {
		return nil
	}

	wait := p.blockWait(current)
	if wait <= 0 {
		return p.requestBlock(conn, current, "", 0)
	}

	// A timer and not a sleep, because this runs on the read loop and the
	// printer must go on answering commands, a stop among them, while it waits.
	time.AfterFunc(wait, func() {
		if p.park(conn, current, "") {
			return
		}

		// A connection that went away in the wait is not an error: the resume
		// on the next one asks for the same block.
		_ = p.requestBlock(conn, current, "", 0)
	})

	return nil
}

// park records the block request a held transfer owes, and reports whether
// the transfer is held.
func (p *Printer) park(conn *websocket.Conn, current *transfer, hashValue string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.transferHeld {
		return false
	}

	p.parked = &parkedBlock{conn: conn, current: current, hash: hashValue}

	return true
}

// blockWait is how long the block that just arrived would have taken at
// TransferRate.
func (p *Printer) blockWait(current *transfer) time.Duration {
	if p.cfg.TransferRate <= 0 {
		return 0
	}

	p.mu.Lock()
	block := min(int64(transferBlock), current.size)
	p.mu.Unlock()

	return time.Duration(block * int64(time.Second) / p.cfg.TransferRate)
}

// transferFor finds the transfer a T frame belongs to. fetching is false for a
// frame naming any other transfer, and takeTransferChunk ends the connection
// over one of those.
func (p *Printer) transferFor(fileID uint32) (current *transfer, fetching bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.transfer == nil || p.transfer.fileID != fileID {
		return nil, false
	}

	return p.transfer, true
}

// accountChunk counts one chunk against the transfer. blockDone is whether the
// range asked for is covered, and resetOwed is whether the drop of
// ErrTransferReset is still to come. beginResume spends it, on the connection
// after the drop.
func (p *Printer) accountChunk(current *transfer, payload []byte) (received int64, blockDone, resetOwed bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	current.digest.Write(payload)
	current.received += int64(len(payload))
	p.lastTransfer.Received = current.received

	return current.received, current.received > current.blockEnd, current.resetOwed
}

// finishTransfer writes the file into the tree and says so. A transfer that
// ended while the caller was holding it plants nothing and says nothing: one
// transfer reports its end once, and the printer has already reported this one.
func (p *Printer) finishTransfer(conn *websocket.Conn, current *transfer) error {
	p.mu.Lock()

	if p.transfer != current {
		p.mu.Unlock()

		return nil
	}

	transferID := int(current.transferID)
	p.transfer = nil
	p.lastTransfer.Received = current.received
	p.lastTransfer.Digest = hex.EncodeToString(current.digest.Sum(nil))
	p.lastTransfer.Finished = true
	p.lastTransfer.Landed = p.plant(File{
		Path:     current.path,
		Size:     current.received,
		Modified: time.Now().Unix(),
	})
	p.mu.Unlock()

	return p.send(conn, event{
		Event: eventTransferFinished, TransferID: &transferID, State: p.State(),
	})
}

// plant writes one entry into the tree, replacing the entry at that path, and
// reports whether the file is on the storage under that name. A file sent twice
// is one file on a storage device, and two entries at one path is a listing no
// real board reports. The caller holds the lock.
//
// A name whose 8.3 short name a sibling already holds is written nowhere, the
// sibling is left undeletable, and the transfer still reports itself finished.
// A CORE One L at 6.8.1+16182 behaves this way: the second of two long names
// that shorten alike never appears in the board's own listing, its transfer is
// reported done with every byte taken, and the first cannot be deleted
// afterwards. See shortName and Printer.lockEntry.
func (p *Printer) plant(entry File) bool {
	for at, existing := range p.files {
		if existing.Path == entry.Path {
			p.files[at] = entry

			return true
		}
	}

	if holder, taken := p.shortNameHolder(entry.Path); taken {
		p.lockEntry(holder)

		return false
	}

	p.files = append(p.files, entry)

	return true
}

// abortTransfer gives up on a file the server would not serve. Nothing is
// written: a partial file in the tree would be a printer offering a print that
// stops in the middle.
func (p *Printer) abortTransfer(conn *websocket.Conn, current *transfer) error {
	p.mu.Lock()

	if p.transfer != current {
		p.mu.Unlock()

		return nil
	}

	transferID := int(current.transferID)
	p.transfer = nil
	p.lastTransfer.Digest = hex.EncodeToString(current.digest.Sum(nil))
	p.mu.Unlock()

	return p.send(conn, event{
		Event: eventTransferAborted, TransferID: &transferID, State: p.State(),
	})
}

// stopTransfer answers STOP_TRANSFER. A printer with nothing to stop still
// completes the command. The shipping command table lets it run from every
// state.
func (p *Printer) stopTransfer(conn *websocket.Conn) error {
	p.mu.Lock()
	current := p.transfer
	p.transfer = nil

	if current == nil {
		p.mu.Unlock()

		return nil
	}

	transferID := int(current.transferID)
	p.mu.Unlock()

	return p.send(conn, event{
		Event: eventTransferStopped, TransferID: &transferID, State: p.State(),
	})
}

// transferInfoData is the SEND_TRANSFER_INFO payload: what the printer makes of
// the transfer it is running, and an empty object when it is running none.
func (p *Printer) transferInfoData() map[string]any {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.transfer == nil {
		return map[string]any{}
	}

	return map[string]any{
		"transfer_id": p.transfer.transferID,
		"path":        p.transfer.path,
		"size":        p.transfer.size,
		"transferred": p.transfer.received,
	}
}
