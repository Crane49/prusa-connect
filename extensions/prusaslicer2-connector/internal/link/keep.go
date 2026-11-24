package link

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
)

// keepFile holds, for each printer, where its uploads are also saved on this
// server. It sits in the data folder, beside the folders the sweep empties.
const keepFile = "keep.json"

// The bounds a storage name and a folder are held to, as the core holds a
// storage path.
const (
	maxStorageBytes = 64
	maxFolderBytes  = 1024
	// maxKeepBody is the largest body PutKeep reads: both fields at their
	// bound, with room for the JSON around them.
	maxKeepBody = maxFolderBytes + maxStorageBytes + 64
)

// Keep is where the uploads to one printer are also saved on this server, as
// a file dropped on the printer's page is with Save to storage. The zero value
// saves none: the file goes to the printer and is then deleted here.
type Keep struct {
	// Storage is the storage of this server. Empty saves nothing.
	Storage string `json:"storage"`
	// Folder is the folder inside the storage. Empty is its root.
	Folder string `json:"folder"`
}

// Files writes a file into a storage of this server as a person.
type Files interface {
	// Write replaces a file of that name, so deliverKept asks Exists first.
	Write(ctx context.Context, principal, storage, file string, size int64, body io.Reader) error
	// Exists reports whether the storage holds a file at file. A storage
	// this server does not have holds none.
	Exists(ctx context.Context, principal, storage, file string) (bool, error)
}

// freeNameTries is how many numbered names deliverKept tries before it
// refuses, as the core does for a printer.
const freeNameTries = 99

// The refusals Files reports.
var (
	// ErrNoShelf is a saved storage this server no longer has.
	ErrNoShelf = errors.New("link: the storage the printer saves into is gone")
	// ErrCannotKeep is a person who may not write into the storage.
	ErrCannotKeep = errors.New("link: the person may not write into the storage")
	// ErrNoFreeName is a folder that holds the name and every numbered one.
	ErrNoFreeName = errors.New("link: the folder holds every free name for the file")
	// ErrCannotCheckKeep is a module the operator did not let ask whether a
	// person may operate the printer, so nobody may change Save to storage.
	ErrCannotCheckKeep = errors.New("link: the module may not ask who may change Save to storage")
)

// errBadKeep is a Keep the person sent that names no storage path.
var errBadKeep = errors.New("link: the storage or the folder is not one this server takes")

// GetKeep answers GET /printers/{printer}/keep: where the printer's uploads
// are saved. Whoever sees the printer may read it.
func (s *Service) GetKeep(w http.ResponseWriter, r *http.Request, principal, printerID string) {
	if _, err := s.fleet.Printer(r.Context(), principal, printerID); err != nil {
		pageRefusal(printerID, err).Write(w)

		return
	}

	keep, err := s.keepOf(printerID)
	if err != nil {
		pageRefusal(printerID, err).Write(w)

		return
	}

	// The card shows the setting read-only to a person who may not change
	// it, rather than a switch that only fails when it is pressed. Such a
	// person learns whether it is on, and not which storage and folder,
	// which may be ones they may not read.
	state := keepState{Keep: keep, On: keep.Storage != "", MayChange: true}

	switch err = s.mayChangeKeep(r.Context(), principal, printerID); {
	case err == nil:
	case errors.Is(err, ErrCannotCheckKeep):
		state = keepState{On: state.On, Refused: refusedCheck}
	case errors.Is(err, ErrDenied):
		state = keepState{On: state.On, Refused: refusedOperate}
	default:
		// The check itself failed, which says nothing about the person.
		pageRefusal(printerID, err).Write(w)

		return
	}

	writeJSON(w, http.StatusOK, state)
}

// Why a person may not change Save to storage, as GET /keep says it.
const (
	// refusedOperate is a person who may not operate the printer.
	refusedOperate = "operate"
	// refusedCheck is a module the operator did not let ask, so nobody may.
	refusedCheck = "check"
)

// keepState is what GET /keep answers: the setting, whether it is on, and
// whether the person asking may change it, or why not.
type keepState struct {
	Keep

	On        bool   `json:"on"`
	MayChange bool   `json:"may_change"`
	Refused   string `json:"refused,omitempty"`
}

// PutKeep answers PUT /printers/{printer}/keep. Only a person who may operate
// the printer changes where its uploads go. The write itself is made later as
// the person who uploads, so the storage's own access holds then.
func (s *Service) PutKeep(w http.ResponseWriter, r *http.Request, principal, printerID string) {
	printer, err := s.fleet.Printer(r.Context(), principal, printerID)
	if err != nil {
		pageRefusal(printerID, err).Write(w)

		return
	}

	if err = s.mayChangeKeep(r.Context(), principal, printer.ID); err != nil {
		pageRefusal(printerID, err).Write(w)

		return
	}

	var keep Keep

	decoder := json.NewDecoder(io.LimitReader(r.Body, maxKeepBody))
	decoder.DisallowUnknownFields()

	if err = decoder.Decode(&keep); err != nil {
		badRequest("The body is not a storage and a folder.").Write(w)

		return
	}

	if keep, err = cleanKeep(keep); err != nil {
		badRequest("The storage or the folder is not one this server takes.").Write(w)

		return
	}

	if err = s.setKeep(printer.ID, keep); err != nil {
		pageRefusal(printerID, err).Write(w)

		return
	}

	writeJSON(w, http.StatusOK, keep)
}

// mayChangeKeep refuses a person who may not operate the printer. Unlike an
// upload, no later call checks the person again, so a module that may not
// ask is refused and not let through.
func (s *Service) mayChangeKeep(ctx context.Context, principal, printer string) error {
	if s.access == nil {
		return ErrCannotCheckKeep
	}

	allowed, err := s.access.MayPrint(ctx, principal, printer)

	switch {
	case errors.Is(err, ErrCannotAsk):
		return fmt.Errorf("%w: %w", ErrCannotCheckKeep, err)
	case err != nil:
		return err
	case !allowed:
		return ErrDenied
	}

	return nil
}

// cleanKeep holds a Keep to a storage name and a folder relative to its root.
func cleanKeep(keep Keep) (Keep, error) {
	if keep.Storage == "" {
		return Keep{}, nil
	}

	if len(keep.Storage) > maxStorageBytes || strings.ContainsAny(keep.Storage, "/\\") ||
		len(keep.Folder) > maxFolderBytes || hasControl(keep.Storage+keep.Folder) {
		return Keep{}, errBadKeep
	}

	folder := strings.Trim(keep.Folder, "/")
	if folder != "" && (path.Clean(folder) != folder || folder == ".." || strings.HasPrefix(folder, "../")) {
		return Keep{}, errBadKeep
	}

	return Keep{Storage: keep.Storage, Folder: folder}, nil
}

func hasControl(text string) bool {
	return strings.IndexFunc(text, func(r rune) bool { return r < 0x20 || r == 0x7f }) >= 0
}

// keepOf is where one printer's uploads are saved.
func (s *Service) keepOf(printerID string) (Keep, error) {
	s.keepMu.Lock()
	defer s.keepMu.Unlock()

	keeps, err := s.readKeeps()

	return keeps[printerID], err
}

// setKeep records where one printer's uploads are saved. The file is replaced
// whole, so a crash leaves the old one or the new one.
func (s *Service) setKeep(printerID string, keep Keep) error {
	s.keepMu.Lock()
	defer s.keepMu.Unlock()

	keeps, err := s.readKeeps()
	if err != nil {
		return err
	}

	if keep.Storage == "" {
		delete(keeps, printerID)
	} else {
		keeps[printerID] = keep
	}

	body, err := json.MarshalIndent(keeps, "", "  ")
	if err != nil {
		return fmt.Errorf("encode the saved storages: %w", err)
	}

	tmp := filepath.Join(s.data, keepFile+".tmp")
	if err = os.WriteFile(tmp, body, 0o600); err != nil {
		return fmt.Errorf("write the saved storages: %w", err)
	}

	if err = os.Rename(tmp, filepath.Join(s.data, keepFile)); err != nil {
		return fmt.Errorf("replace the saved storages: %w", err)
	}

	return nil
}

func (s *Service) readKeeps() (map[string]Keep, error) {
	keeps := map[string]Keep{}

	body, err := os.ReadFile(filepath.Join(s.data, keepFile))
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return keeps, nil
	case err != nil:
		return nil, fmt.Errorf("read the saved storages: %w", err)
	}

	if err = json.Unmarshal(body, &keeps); err != nil {
		return nil, fmt.Errorf("read the saved storages: %w", err)
	}

	return keeps, nil
}

// freeTarget is where an upload is saved: its own name in the folder, or the
// first numbered one free beside it, so one person's upload never replaces
// another's. Two uploads of one name in the same moment may still meet.
func (s *Service) freeTarget(ctx context.Context, principal string, keep Keep, name string) (string, error) {
	target := path.Join(keep.Folder, name)

	for n := 0; n <= freeNameTries; n++ {
		candidate := target
		if n > 0 {
			candidate = numberedName(target, n)
		}

		held, err := s.files.Exists(ctx, principal, keep.Storage, candidate)
		if err != nil {
			return "", err
		}

		if !held {
			return candidate, nil
		}
	}

	return "", ErrNoFreeName
}

// numberedName puts [n] before the file's extension, as the core names a
// second file on a printer: part.bgcode becomes part[1].bgcode.
func numberedName(at string, n int) string {
	dir, name := path.Split(at)
	extension := path.Ext(name)

	return dir + strings.TrimSuffix(name, extension) + "[" + strconv.Itoa(n) + "]" + extension
}

// deliverKept saves the upload in the printer's storage as the person who
// sent it and sends it to the printer from there. The copy stays on this
// server as a file uploaded on the Storage page does.
func (s *Service) deliverKept(ctx context.Context, upload Upload, keep Keep, body string, size int64) error {
	file, err := os.Open(body)
	if err != nil {
		return fmt.Errorf("open the upload: %w", err)
	}

	target, err := s.freeTarget(ctx, upload.Principal, keep, upload.Name)
	if err != nil {
		_ = file.Close()

		return err
	}

	err = s.files.Write(ctx, upload.Principal, keep.Storage, target, size, file)
	_ = file.Close()
	_ = os.Remove(body)

	if err != nil {
		return err
	}

	send := s.fleet.Send
	if upload.Print {
		send = s.fleet.Print
	}

	return send(ctx, upload.Principal, upload.Printer, keep.Storage, target, "/usb/"+upload.Name)
}
