package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/fault"
	"github.com/prusa3d/prusa-connect/sdk/storageapi"
)

// A temporary copy is a file a person sent to a printer without keeping it.
// The browser uploads it into a storage, the server relays it to the printer,
// and then the server deletes it. The rules for when it is deleted are on
// incomingCopies.
const (
	// incomingDir is the folder at a storage's root that holds the temporary
	// copies, one folder per copy below it. The storage listing leaves it out,
	// and an ordinary upload, folder or move into it is refused, so a file a
	// person keeps can never land where this server deletes files.
	incomingDir = ".pcl-incoming"

	// incomingKeep is how long a copy whose send did not finish is kept. A
	// printer that already holds the name, a printer that went away and a
	// transfer that failed can each be sent again from the same copy for this
	// long.
	incomingKeep = time.Hour

	// incomingSweep is how often the copies left behind are looked for.
	incomingSweep = 10 * time.Minute

	// incomingWatch is how often the sweep looks for a storage that started
	// serving since the last look. A storage starts after this server does,
	// and its first sweep is the one that removes what a restart left.
	incomingWatch = 2 * time.Second

	// incomingDeleteBudget bounds one deletion, which runs after the caller
	// that caused it may already have gone.
	incomingDeleteBudget = 30 * time.Second
)

// isIncoming reports whether a path inside a storage names the temporary
// area or something under it.
func isIncoming(name string) bool {
	clean := storageapi.CleanRelPath(name)

	return clean == incomingDir || strings.HasPrefix(clean, incomingDir+"/")
}

// copyFolder is the folder of one temporary copy, such as
// .pcl-incoming/1761300000-<uuid>, and the empty string for a path that is not
// inside one.
func copyFolder(name string) string {
	inside, found := strings.CutPrefix(storageapi.CleanRelPath(name), incomingDir+"/")
	if !found {
		return ""
	}

	own, _, _ := strings.Cut(inside, "/")
	if own == "" {
		return ""
	}

	return incomingDir + "/" + own
}

// copyBorn reads when a copy's folder was made from its name. A name this
// server did not write reads as the zero time, so it counts as old.
func copyBorn(folder string) time.Time {
	stamp, _, found := strings.Cut(path.Base(folder), "-")
	if !found {
		return time.Time{}
	}

	seconds, err := strconv.ParseInt(stamp, 10, 64)
	if err != nil {
		return time.Time{}
	}

	return time.Unix(seconds, 0)
}

// refuseIncoming is the answer to an ordinary write that names the temporary
// area.
func refuseIncoming(instance, name string) error {
	if !isIncoming(name) {
		return nil
	}

	return fault.StorageInvalidPath.
		New("that folder holds the files on their way to a printer").
		With(fault.StringParam("storage_id", instance), fault.StringParam("path", name)).
		Wrap(storageapi.ErrInvalidPath)
}

// copyKey names one temporary copy: its storage and its folder.
type copyKey struct {
	storage string
	folder  string
}

// incomingCopies holds the temporary copies and deletes them.
//
// A copy is deleted:
//   - at once, when the file landed on the printer, when the printer already
//     held the same file, or when a person stopped the transfer;
//   - incomingKeep after it was made, when its send failed or the printer
//     holds the name and the page asks the person, so the same copy can be
//     sent again;
//   - on the first sweep after the server starts, when it was made before
//     the start, because no upload and no relay outlives the process that
//     ran it.
//
// A copy an upload or a relay is using is never deleted. Nothing outside the
// temporary area is ever deleted here.
type incomingCopies struct {
	stores stores
	log    *slog.Logger
	now    func() time.Time
	// started is when this process began. A copy whose name says it was made
	// before then is a leftover. The name carries whole seconds, so a copy
	// made in the second the process started counts as a leftover too, and no
	// storage serves an upload that early.
	started time.Time

	mu sync.Mutex
	// held counts the uploads and relays using each copy.
	held map[copyKey]int
	// swept names the storages swept once since this server started.
	swept map[string]bool
}

func newIncomingCopies(s stores, log *slog.Logger) *incomingCopies {
	return &incomingCopies{
		stores:  s,
		log:     log,
		now:     time.Now,
		started: time.Now(),
		held:    map[copyKey]int{},
		swept:   map[string]bool{},
	}
}

// hold marks a copy in use until the returned function is called.
func (c *incomingCopies) hold(storage, folder string) func() {
	key := copyKey{storage: storage, folder: folder}

	c.mu.Lock()
	c.held[key]++
	c.mu.Unlock()

	return func() {
		c.mu.Lock()
		defer c.mu.Unlock()

		c.held[key]--
		if c.held[key] <= 0 {
			delete(c.held, key)
		}
	}
}

func (c *incomingCopies) inUse(storage, folder string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.held[copyKey{storage: storage, folder: folder}] > 0
}

// save writes one temporary copy under a folder of its own and reports where
// it landed. name is the file's own name. Only its last part is used, because
// the server chooses the folder.
func (c *incomingCopies) save(
	ctx context.Context, storage, name string, body io.Reader,
) (httpapi.SaveResult, error) {
	base := path.Base(storageapi.CleanRelPath(name))
	if base == "" || base == "." || base == "/" {
		return httpapi.SaveResult{}, fault.StorageInvalidPath.
			New("a temporary upload needs a file name").
			With(fault.StringParam("storage_id", storage), fault.StringParam("path", name)).
			Wrap(storageapi.ErrInvalidPath)
	}

	folder := incomingDir + "/" + strconv.FormatInt(c.now().Unix(), 10) + "-" + uuid.NewString()
	target := folder + "/" + base

	release := c.hold(storage, folder)
	defer release()

	written, err := c.stores.Save(ctx, storage, target, body)
	if err != nil {
		// The storage published nothing, but it may have made the folder.
		c.remove(ctx, storage, folder)

		return httpapi.SaveResult{}, err
	}

	return httpapi.SaveResult{
		Path: target, Size: written.Size, SHA256: written.SHA256, Temporary: true,
	}, nil
}

// relayed is what a send from a temporary copy calls once it has ended. The
// copy is deleted when the send finished or a person stopped it, and kept for
// the sweep otherwise.
func (c *incomingCopies) relayed(ctx context.Context, storage, folder string, err error) {
	if !sendSettled(err) {
		return
	}

	if c.inUse(storage, folder) {
		return
	}

	c.remove(ctx, storage, folder)
}

// sendSettled reports whether a send leaves nothing to send again: the file
// landed, the printer already held the same file, or a person stopped the
// transfer.
func sendSettled(err error) bool {
	if err == nil || errors.Is(err, errHeldSame) {
		return true
	}

	carried, ok := errors.AsType[*fault.Fault](err)
	if !ok || carried.Code() != connectorapi.TransferFailed.Code() {
		return false
	}

	for _, param := range carried.Params() {
		if param.Key == "event" && param.Value.String() == "stopped" {
			return true
		}
	}

	return false
}

// remove deletes one copy's folder. It runs on a context of its own, because
// the request that caused it may be gone, and a failure is left to the sweep.
func (c *incomingCopies) remove(ctx context.Context, storage, folder string) {
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), incomingDeleteBudget)
	defer cancel()

	if err := c.stores.Delete(cleanup, storage, folder); err != nil && !errors.Is(err, os.ErrNotExist) {
		c.log.WarnContext(ctx, "a temporary copy could not be deleted, the next sweep tries again",
			"storage_id", storage, "path", folder, "error", err.Error())
	}
}

// Run sweeps at once and then every incomingSweep until ctx ends. Between
// the sweeps it gives every storage that started serving its first sweep.
func (c *incomingCopies) Run(ctx context.Context) {
	full := time.NewTicker(incomingSweep)
	defer full.Stop()

	watch := time.NewTicker(incomingWatch)
	defer watch.Stop()

	c.sweep(ctx, false)

	for {
		select {
		case <-ctx.Done():
			return
		case <-watch.C:
			c.sweep(ctx, true)
		case <-full.C:
			c.sweep(ctx, false)
		}
	}
}

// sweep deletes the copies nothing uses any more in every storage that is
// serving, or only in those not swept yet when fresh is set. The first sweep
// of a storage deletes every copy made before this process started. Every
// sweep deletes the copies older than incomingKeep.
func (c *incomingCopies) sweep(ctx context.Context, fresh bool) {
	for _, storage := range c.stores.Serving() {
		c.mu.Lock()
		first := !c.swept[storage]
		c.mu.Unlock()

		if fresh && !first {
			continue
		}

		if err := c.sweepOne(ctx, storage, first); err != nil {
			c.log.WarnContext(ctx, "the temporary copies of a storage could not be listed",
				"storage_id", storage, "error", err.Error())

			continue
		}

		c.mu.Lock()
		c.swept[storage] = true
		c.mu.Unlock()
	}
}

func (c *incomingCopies) sweepOne(ctx context.Context, storage string, first bool) error {
	entries, err := c.stores.List(ctx, storage, incomingDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, storageapi.ErrInvalidPath) {
			return nil
		}

		return fmt.Errorf("list %s: %w", incomingDir, err)
	}

	cutoff := c.now().Add(-incomingKeep)

	for _, entry := range entries {
		folder := incomingDir + "/" + entry.Name
		if c.inUse(storage, folder) {
			continue
		}

		born := copyBorn(folder)
		leftover := first && born.Before(c.started)

		if !leftover && !born.Before(cutoff) {
			continue
		}

		c.remove(ctx, storage, folder)
	}

	return nil
}
