package connect

import (
	"context"
	"crypto/sha1" //nolint:gosec // the cloud's file hash is SHA-1; it names a file and protects nothing.
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/prusa3d/prusaslicer3-connector/internal/cloud"
)

// The bounds the cloud holds a file name to.
const (
	maxFileNameBytes   = 248
	forbiddenNameChars = "\x00\\/:*?\"<>|#%¯°ˇ"
)

// printExtensions are the file extensions the cloud accepts for a print.
//
//nolint:gochecknoglobals // a fixed set, read-only.
var printExtensions = map[string]bool{
	"bgcode": true, "bgc": true, "bg": true, "gcode": true, "gco": true, "g": true,
	"sl1": true, "sl1s": true, "m1": true, "sl2": true, "slx": true,
}

// uploadLife is how long a registered upload waits for its file.
const uploadLife = time.Hour

// maxRequest bounds the body of an upload request.
const maxRequest = 64 << 10

// maxPending bounds the uploads that wait for their file at once, and
// maxPendingPerPerson the share of one person, so a slicer that registers and
// never sends cannot lock everybody else out for an hour.
const (
	maxPending          = 64
	maxPendingPerPerson = 8
)

// Fleet is the part of the host's device door this package calls.
type Fleet interface {
	// Printers lists the printers principal may see.
	Printers(ctx context.Context, principal string) ([]Printer, error)
	// Printer reads one printer principal may see.
	Printer(ctx context.Context, principal, id string) (Printer, error)
	// Print sends a file of the module's print folder to a printer and starts
	// it, as principal. source is relative to the print folder, and target is
	// where the file lands on the printer. The host deletes source once the
	// print has settled. It answers the file the printer prints, which is a
	// numbered name when the printer already holds another file at target.
	Print(ctx context.Context, principal, id, source, target string) (string, error)
}

// Access answers whether a person may print on a printer, before the slicer
// sends the file (access.check, operate).
type Access interface {
	MayPrint(ctx context.Context, principal, printer string) (bool, error)
}

// The refusals Fleet and Access report.
var (
	ErrNoPrinter    = errors.New("connect: no such printer")
	ErrDenied       = errors.New("connect: the person may not do that")
	ErrNameTaken    = errors.New("connect: the printer holds a file of that name")
	ErrNotReady     = errors.New("connect: the printer is not ready to print")
	ErrPrintRefused = errors.New("connect: the printer refused to start the print")
	// ErrCannotAsk is an Access the operator did not let the module ask. The
	// print itself is then the check.
	ErrCannotAsk = errors.New("connect: the module may not ask whether a person may print")
)

// printableStates are the connect_state values of a printer that starts a
// print now. This server has no print queue, so a printer in any other state
// refuses the file, and the slicer is told before it sends it. The print
// dialog offers only these (ui/src/compatibility.ts, PRINTABLE_STATES).
//
//nolint:gochecknoglobals // a fixed set, read-only.
var printableStates = map[string]bool{"READY": true, "IDLE": true, "FINISHED": true, "STOPPED": true}

// upload is one registered upload that waits for its file.
type upload struct {
	ID        uint64
	TeamID    int64
	Principal string
	Printer   string
	Name      string
	Size      int64
	Hash      string
	Path      string
	SetReady  bool
	Position  *int
	expires   time.Time
}

// Service answers the printer list and the two upload steps.
type Service struct {
	fleet   Fleet
	access  Access
	data    string
	maxSize int64
	staged  bool
	now     func() time.Time

	mu      sync.Mutex
	next    uint64
	pending map[uint64]upload
}

// Options are what New needs.
type Options struct {
	Fleet Fleet
	// Access is asked whether a person may print before the file is sent.
	// Nil leaves the print itself as the check.
	Access Access
	// Data is the module's data folder, which holds the print folder.
	Data string
	// MaxSize bounds one file in bytes.
	MaxSize int64
	// Staged reports that the operator approved the upload route, so the
	// host writes the file into the data folder. Without it no print can
	// finish, and Register refuses at once.
	Staged bool
	// Now is the clock. Nil is time.Now.
	Now func() time.Time
}

// New returns a Service.
func New(opts Options) *Service {
	now := opts.Now
	if now == nil {
		now = time.Now
	}

	return &Service{
		fleet: opts.Fleet, access: opts.Access, data: opts.Data, maxSize: opts.MaxSize, staged: opts.Staged,
		now: now, next: uint64(max(now().Unix(), 0)), pending: map[uint64]upload{},
	}
}

// PrinterList answers GET /slicer/v2/printers: {"printers": [...]}.
func (s *Service) PrinterList(w http.ResponseWriter, r *http.Request, principal string) {
	printers, err := s.fleet.Printers(r.Context(), principal)
	if err != nil {
		cloud.Unavailable().Write(w)

		return
	}

	out := make([]map[string]any, 0, len(printers))
	for _, p := range printers {
		out = append(out, PrinterJSON(p))
	}

	cloud.JSON(w, http.StatusOK, map[string]any{"printers": out})
}

// uploadRequest is the body PrusaSlicer posts: the data object of the PRINT
// message, with filename and size set by the slicer.
type uploadRequest struct {
	Filename    *string         `json:"filename"`
	Size        *int64          `json:"size"`
	PrinterUUID string          `json:"printer_uuid"`
	SetReady    bool            `json:"set_ready"`
	Position    *int            `json:"position"`
	WaitUntil   json.RawMessage `json:"wait_until"`
	Destination string          `json:"destination"`
}

// Register answers POST /app/users/teams/{team_id}/uploads. It checks the
// request in the order the cloud checks it, and answers the upload the cloud
// answers.
func (s *Service) Register(w http.ResponseWriter, r *http.Request, principal, team string) {
	teamID, ok := teamID(team)
	if !ok {
		cloud.NotFound().Write(w)

		return
	}

	var request uploadRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRequest)).Decode(&request); err != nil ||
		request.Filename == nil || request.Size == nil {
		cloud.BadRequest("").Write(w)

		return
	}

	if request.Position != nil && *request.Position != 0 && request.SetReady {
		cloud.BadRequest("Set ready allowed only for first position").Write(w)

		return
	}

	if request.Position == nil && request.SetReady {
		zero := 0
		request.Position = &zero
	}

	if hasValue(request.WaitUntil) && request.Position == nil {
		cloud.BadRequest("wait_until is only valid when adding to job queue").Write(w)

		return
	}

	name := *request.Filename
	if refusal, refused := checkName(name, *request.Size, s.maxSize); refused {
		refusal.Write(w)

		return
	}

	if request.PrinterUUID == "" {
		cloud.BadRequest("this server keeps no file without a printer").Write(w)

		return
	}

	// Without the staged route the file would come over the wire, where the
	// module never gets it as a file, and the print could never finish.
	if !s.staged {
		cloud.BadRequest("the operator of this server did not approve uploads for PrusaSlicer, " +
			"so it takes no print. Ask the operator to approve the upload route of the extension").Write(w)

		return
	}

	printer, err := s.fleet.Printer(r.Context(), principal, request.PrinterUUID)
	if err != nil {
		fleetRefusal(err).Write(w)

		return
	}

	if !printer.TakesFiles() {
		cloud.PrinterNoTransfer().Write(w)

		return
	}

	// This server has no print queue. A file is printed at once, so only the
	// first position with set_ready is accepted.
	if !request.SetReady || hasValue(request.WaitUntil) {
		cloud.BadRequest("this server has no print queue, so a file can only be printed at once").Write(w)

		return
	}

	// Asked before the slicer sends up to the largest file, so a person who
	// may not print, or a printer that is busy, is refused now.
	if refusal, refused := s.mayPrint(r.Context(), principal, printer); refused {
		refusal.Write(w)

		return
	}

	added, err := s.add(upload{
		TeamID: teamID, Principal: principal, Printer: request.PrinterUUID,
		Name: name, Size: *request.Size, Hash: fileHash(name, *request.Size),
		Path: downloadPath(name, request.Destination), SetReady: request.SetReady, Position: request.Position,
	})
	if err != nil {
		cloud.Unavailable().Write(w)

		return
	}

	cloud.JSON(w, http.StatusOK, uploadJSON(added))
}

// mayPrint is the refusal of a print the person may not start on printer, or
// that printer cannot start now.
func (s *Service) mayPrint(ctx context.Context, principal string, printer Printer) (cloud.Error, bool) {
	if s.access != nil {
		allowed, err := s.access.MayPrint(ctx, principal, printer.ID)

		switch {
		case errors.Is(err, ErrCannotAsk):
		case err != nil:
			return cloud.Unavailable(), true
		case !allowed:
			return cloud.Forbidden(), true
		}
	}

	if state := connectState(printer); !printableStates[state] {
		return cloud.BadRequest(fmt.Sprintf("the printer is %s. This server has no print queue, "+
			"so it prints only on a printer that is ready", state)), true
	}

	return cloud.Error{}, false
}

// Receive answers PUT /app/teams/{team_id}/files/raw?upload_id=<id>. The host
// wrote the body to file, relative to the data folder.
func (s *Service) Receive(w http.ResponseWriter, r *http.Request, principal, team, file string) {
	teamID, ok := teamID(team)
	if !ok {
		cloud.NotFound().Write(w)

		return
	}

	id, err := strconv.ParseUint(r.URL.Query().Get("upload_id"), 10, 64)
	if err != nil {
		cloud.NotFound().Write(w)

		return
	}

	staged := filepath.Join(s.data, filepath.FromSlash(file))

	info, err := os.Stat(staged)
	if file == "" || err != nil {
		cloud.BadRequest("MissingPayload").Write(w)

		return
	}

	pending, ok := s.take(id, teamID, principal, info.Size())
	if !ok {
		cloud.NotFound().Write(w)

		return
	}

	// The host deletes the file once the print has settled (ADR-20261123-vvb).
	source := strconv.FormatUint(pending.ID, 10) + "-" + pending.Name
	kept := filepath.Join(s.data, "print", source)

	if err = os.MkdirAll(filepath.Dir(kept), 0o750); err == nil {
		err = os.Rename(staged, kept)
	}

	// An upload ends at its first file, as in the cloud. PrusaSlicer never
	// sends a file for the same upload twice: a retry registers a new one.
	if err != nil {
		cloud.Unavailable().Write(w)

		return
	}

	printed, err := s.fleet.Print(r.Context(), principal, pending.Printer, source, pending.Path)
	if err != nil {
		_ = os.Remove(kept)

		fleetRefusal(err).Write(w)

		return
	}

	if printed == "" {
		printed = pending.Path
	}

	cloud.JSON(w, http.StatusOK, map[string]any{
		"name":         path.Base(printed),
		"display_name": path.Base(printed),
		"path":         printed,
		"size":         pending.Size,
		"hash":         pending.Hash,
		"team_id":      pending.TeamID,
		"upload_id":    pending.ID,
		"type":         "PRINT_FILE",
	})
}

// printKeep is how long a file stays in the print folder at most. The host
// deletes a printed file once its print has settled, so only a file whose
// send failed is left, and the module deletes it (ADR-20261123-vvb rule 3).
const printKeep = 24 * time.Hour

// SweepEvery is how often Sweeper sweeps.
const SweepEvery = time.Hour

// Sweep deletes every file of the print folder older than printKeep. It runs
// when the module starts and every SweepEvery.
func (s *Service) Sweep() {
	folder := filepath.Join(s.data, "print")

	entries, err := os.ReadDir(folder)
	if err != nil {
		return
	}

	expired := s.now().Add(-printKeep)

	for _, entry := range entries {
		// A file the sweep cannot read or remove is tried again at the next one.
		if info, infoErr := entry.Info(); infoErr == nil && info.ModTime().Before(expired) {
			_ = os.RemoveAll(filepath.Join(folder, entry.Name()))
		}
	}
}

// Sweeper sweeps every SweepEvery until ctx ends.
func (s *Service) Sweeper(ctx context.Context) {
	ticker := time.NewTicker(SweepEvery)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.Sweep()
		}
	}
}

func (s *Service) add(u upload) (upload, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	for id, held := range s.pending {
		if now.After(held.expires) {
			delete(s.pending, id)
		}
	}

	mine := 0

	for _, held := range s.pending {
		if held.Principal == u.Principal {
			mine++
		}
	}

	if len(s.pending) >= maxPending || mine >= maxPendingPerPerson {
		return upload{}, errors.New("connect: too many uploads wait for their file")
	}

	s.next++
	u.ID = s.next
	u.expires = now.Add(uploadLife)
	s.pending[u.ID] = u

	return u, nil
}

// take returns the upload the file is for and forgets it. It must be the same
// team and person, and the file must have the size the upload named, as
// Upload.get in the cloud requires.
func (s *Service) take(id uint64, team int64, principal string, size int64) (upload, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	u, ok := s.pending[id]
	if !ok || u.TeamID != team || u.Principal != principal || u.Size != size || s.now().After(u.expires) {
		return upload{}, false
	}

	delete(s.pending, id)

	return u, true
}

// checkName holds a file to validate_file of the cloud.
func checkName(name string, size, maxSize int64) (cloud.Error, bool) {
	extension := strings.ToLower(strings.TrimPrefix(path.Ext(name), "."))
	if !printExtensions[extension] {
		return cloud.InvalidExtension(), true
	}

	if strings.ContainsAny(name, forbiddenNameChars) {
		return cloud.InvalidFileNameChars(), true
	}

	if len(name) > maxFileNameBytes {
		return cloud.InvalidFileNameLength(), true
	}

	if size <= 0 {
		return cloud.BadRequest("the file is empty"), true
	}

	if size > maxSize {
		return cloud.PayloadTooLarge(), true
	}

	return cloud.Error{}, false
}

// downloadPath is where the file lands on the printer: the USB drive, or the
// storage the request named.
func downloadPath(name, destination string) string {
	storage := strings.Trim(destination, "/")
	if storage == "" {
		storage = "usb"
	}

	return "/" + storage + "/" + name
}

// fileHash is the hash the cloud answers for an upload: SHA-1 of the name and
// the size, in URL-safe base64 with '.' for padding.
func fileHash(name string, size int64) string {
	sum := sha1.Sum(fmt.Appendf(nil, "%s:%d", name, size)) //nolint:gosec // see the import.

	return strings.ReplaceAll(base64.URLEncoding.EncodeToString(sum[:]), "=", ".")
}

// uploadJSON is the upload as the cloud answers it, with empty values left out.
func uploadJSON(u upload) map[string]any {
	out := map[string]any{
		"id":      u.ID,
		"team_id": u.TeamID,
		"name":    u.Name,
		"size":    u.Size,
		"hash":    u.Hash,
		"state":   "INITIATED",
		"source":  "CONNECT_USER",
		"path":    u.Path,
	}

	if u.SetReady {
		out["set_ready"] = true
	}

	return out
}

// fleetRefusal is the cloud's answer to what the host reported. A failure
// that is no refusal, such as a host that did not answer, is 503, so the
// slicer may try again.
func fleetRefusal(err error) cloud.Error {
	switch {
	case errors.Is(err, ErrNoPrinter):
		return cloud.NotFound()
	case errors.Is(err, ErrDenied):
		return cloud.Forbidden()
	case errors.Is(err, ErrNameTaken):
		return cloud.BadRequest("the printer already holds a file of that name")
	case errors.Is(err, ErrNotReady):
		return cloud.BadRequest("the printer is not ready to print")
	case errors.Is(err, ErrPrintRefused):
		return cloud.BadRequest("the printer refused to start the print")
	default:
		return cloud.Unavailable()
	}
}

// hasValue reports a JSON value that is there and not null, false or 0, which
// is what Python reads as true.
func hasValue(raw json.RawMessage) bool {
	text := strings.TrimSpace(string(raw))

	return text != "" && text != "null" && text != "false" && text != "0"
}
