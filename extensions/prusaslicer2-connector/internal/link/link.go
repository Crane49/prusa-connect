// Package link answers the PrusaLink requests PrusaSlicer 2 sends to a
// printer: the version check, the storage list and the upload of a file.
//
// One address serves one printer of this server. The printer is the
// {printer} segment of the path, so a person adds one physical printer in
// PrusaSlicer for each printer here, each with its own address.
//
// An upload with Print-After-Upload goes to the printer and starts there,
// through the host's device door. An upload without it is kept in a storage of
// this server, and nothing is printed.
package link

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
	"unicode"
	"unicode/utf8"
)

// The storage PrusaSlicer uploads to. The slicer reads its path from the
// storage list and puts it into the upload address as /local.
const (
	storageName = "local"
	storagePath = "/" + storageName
)

// The bounds a file name is held to. The printer keeps the file on its USB
// drive, so a name holds no character a FAT file system refuses: none of
// \ / : * ? " < > | and no control character. A trailing dot or space, which
// FAT drops, leaves no print extension, so the extension check refuses such
// a name. A name also holds no invisible formatting character, such as U+202E
// that shows the rest of the name reversed, so the name on the printer's
// screen is the name that was sent.
const (
	maxFileNameBytes   = 248
	forbiddenNameChars = "\\/:*?\"<>|"
)

// printExtensions are the file extensions a Prusa printer prints.
//
//nolint:gochecknoglobals // a fixed set, read-only.
var printExtensions = map[string]bool{
	"bgcode": true, "bgc": true, "bg": true, "gcode": true, "gco": true, "g": true,
}

// printableStates are the states of a printer that starts a print now. This
// server has no print queue, so a printer in any other state refuses the file.
//
//nolint:gochecknoglobals // a fixed set, read-only.
var printableStates = map[string]bool{"READY": true, "IDLE": true, "FINISHED": true, "STOPPED": true}

// Printer is what the module reads of one printer.
type Printer struct {
	ID    string
	Name  string
	Model string
	State string
}

// Fleet is the part of the host's device door this package calls.
type Fleet interface {
	// Printer reads one printer principal may see.
	Printer(ctx context.Context, principal, id string) (Printer, error)
	// Print sends a file of the module's print folder to a printer and starts
	// it, as principal. source is relative to the print folder, and target is
	// where the file lands on the printer, or a free name beside it when the
	// printer holds a file there.
	Print(ctx context.Context, principal, id, source, target string) error
}

// Access answers whether a person may print on a printer.
type Access interface {
	MayPrint(ctx context.Context, principal, printer string) (bool, error)
}

// Files writes a file into a storage of this server as a person.
type Files interface {
	Write(ctx context.Context, principal, storage, file string, size int64, body io.Reader) error
}

// The refusals Fleet, Access and Files report.
var (
	ErrNoPrinter    = errors.New("link: no such printer")
	ErrDenied       = errors.New("link: the person may not do that")
	ErrNameTaken    = errors.New("link: the printer holds a file of that name")
	ErrNotReady     = errors.New("link: the printer is not ready to print")
	ErrPrintRefused = errors.New("link: the printer refused to start the print")
	ErrNoStorage    = errors.New("link: no such storage")
	// ErrNoShelf is a storage setting that names no storage of this server,
	// so an upload without a print has nowhere to go.
	ErrNoShelf = errors.New("link: the storage setting names no storage of this server")
	// ErrCannotKeep is a person who may not write into the storage an upload
	// without a print goes into.
	ErrCannotKeep = errors.New("link: the person may not write into the storage")
	// ErrCannotAsk is an Access the operator did not let the module ask. The
	// print itself is then the check.
	ErrCannotAsk = errors.New("link: the module may not ask whether a person may print")
)

// Options are what New needs.
type Options struct {
	Fleet  Fleet
	Access Access
	Files  Files
	// Data is the module's data folder. It holds the print folder and the
	// folder a form upload is unpacked into.
	Data string
	// Storage is the storage of this server an upload without a print goes
	// into.
	Storage string
	// Version is the module's own version, reported as the server version.
	Version string
	// Now is the clock. Nil is time.Now.
	Now func() time.Time
}

// Service answers the PrusaLink requests.
type Service struct {
	fleet   Fleet
	access  Access
	files   Files
	data    string
	storage string
	version string
	now     func() time.Time

	next atomic.Uint64
}

// New returns a Service.
func New(opts Options) *Service {
	now := opts.Now
	if now == nil {
		now = time.Now
	}

	return &Service{
		fleet: opts.Fleet, access: opts.Access, files: opts.Files,
		data: opts.Data, storage: opts.Storage, version: opts.Version, now: now,
	}
}

// Version answers GET api/version. PrusaSlicer reads api and text, and from
// capabilities whether it may send the file with PUT. The printer is read
// first, so a wrong address or a credential that cannot see the printer fails
// the slicer's connection test.
func (s *Service) Version(w http.ResponseWriter, r *http.Request, principal, printerID string) {
	printer, err := s.fleet.Printer(r.Context(), principal, printerID)
	if err != nil {
		refusal(err).Write(w)

		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"api":      "2.0.0",
		"server":   s.version,
		"original": "PrusaLink " + printer.Model,
		"text":     "PrusaLink",
		"hostname": printer.Name,
		"capabilities": map[string]any{
			"upload-by-put": true,
		},
	})
}

// Storage answers GET api/v1/storage: one storage that is never full. The
// printer reports no free space through this server, and the printer itself
// refuses a file it has no room for.
func (s *Service) Storage(w http.ResponseWriter, r *http.Request, principal, printerID string) {
	printer, err := s.fleet.Printer(r.Context(), principal, printerID)
	if err != nil {
		refusal(err).Write(w)

		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"storage_list": []map[string]any{{
			"path":      storagePath,
			"name":      printer.Name,
			"type":      "LOCAL",
			"read_only": false,
			"available": true,
		}},
	})
}

// Upload is one file as it arrived, before the module acts on it.
type Upload struct {
	Principal string
	Printer   string
	Storage   string
	// Name is the file's name, with no folder.
	Name string
	// File is the body, relative to the data folder.
	File string
	// Print reports Print-After-Upload, or print=true in a form.
	Print bool
}

// Put answers PUT api/v1/files/{storage}/{name}. The host wrote the body to
// bodyFile, relative to the data folder. When the operator did not approve
// the staged route, bodyFile is empty and the body is on the request.
func (s *Service) Put(w http.ResponseWriter, r *http.Request, upload Upload) {
	file, err := s.bodyOf(r, upload.File)
	if err != nil {
		refusal(err).Write(w)

		return
	}
	defer s.discard(file)

	upload.File = file
	upload.Print = truthy(r.Header.Get("Print-After-Upload"))

	s.finish(r.Context(), w, upload)
}

// Post answers POST api/files/{storage}, the form PrusaSlicer 2.5 and older
// send: the file in the part named file, the folder in path and print=true to
// start it.
func (s *Service) Post(w http.ResponseWriter, r *http.Request, upload Upload) {
	body, err := s.bodyOf(r, upload.File)
	if err != nil {
		refusal(err).Write(w)

		return
	}
	defer s.discard(body)

	posted, err := s.unpack(r.Header.Get("Content-Type"), body)
	if err != nil {
		refusal(err).Write(w)

		return
	}
	defer s.discard(posted.file)

	if posted.folder != "" {
		badRequest("A folder is not supported. Leave the folder in the upload dialog empty.").Write(w)

		return
	}

	upload.File = posted.file
	upload.Name = posted.name
	upload.Print = posted.print

	s.finish(r.Context(), w, upload)
}

// finish checks the upload and prints it or keeps it.
func (s *Service) finish(ctx context.Context, w http.ResponseWriter, upload Upload) {
	printer, err := s.fleet.Printer(ctx, upload.Principal, upload.Printer)
	if err != nil {
		refusal(err).Write(w)

		return
	}

	if upload.Storage != storageName {
		refusal(ErrNoStorage).Write(w)

		return
	}

	if bad, refused := checkName(upload.Name); refused {
		bad.Write(w)

		return
	}

	if upload.Print {
		err = s.print(ctx, upload, printer)
	} else {
		err = s.keep(ctx, upload)
	}

	if err != nil {
		refusal(err).Write(w)

		return
	}

	w.WriteHeader(http.StatusCreated)
}

// print moves the file into the print folder and starts it on the printer.
// A printer that already holds a file of that name gets the file under a free
// name the host picks, because PrusaSlicer always asks to overwrite and the
// printer keeps the file it has.
func (s *Service) print(ctx context.Context, upload Upload, printer Printer) error {
	if err := s.mayPrint(ctx, upload.Principal, printer); err != nil {
		return err
	}

	body := filepath.Join(s.data, filepath.FromSlash(upload.File))
	info, statErr := os.Stat(body)
	if statErr != nil {
		return fmt.Errorf("read the upload: %w", statErr)
	}

	if info.Size() == 0 {
		return errEmpty
	}

	s.Sweep()

	folder := strconv.FormatUint(s.next.Add(1), 10) + "-" + strconv.FormatInt(s.now().UnixNano(), 36)
	source := path.Join(folder, upload.Name)
	kept := filepath.Join(s.data, printFolder, filepath.FromSlash(source))

	if err := os.MkdirAll(filepath.Dir(kept), 0o750); err != nil {
		return fmt.Errorf("make the print folder: %w", err)
	}

	if err := os.Rename(body, kept); err != nil {
		return fmt.Errorf("move the file into the print folder: %w", err)
	}

	err := s.fleet.Print(ctx, upload.Principal, upload.Printer, source, "/usb/"+upload.Name)
	if refusedBeforeTransfer(err) {
		_ = os.RemoveAll(filepath.Dir(kept))
	}

	return err
}

// refusedBeforeTransfer reports a print the host refused before the printer
// could start reading the file, so the file can go at once. After any other
// failure, such as the slicer going away mid-call, the printer may be reading
// it, and the sweep removes it later.
func refusedBeforeTransfer(err error) bool {
	for _, refusal := range []error{ErrNoPrinter, ErrDenied, ErrNameTaken, ErrNotReady, ErrPrintRefused} {
		if errors.Is(err, refusal) {
			return true
		}
	}

	return false
}

// mayPrint refuses a person who may not operate the printer and a printer
// that would not start a print now.
func (s *Service) mayPrint(ctx context.Context, principal string, printer Printer) error {
	if s.access != nil {
		allowed, err := s.access.MayPrint(ctx, principal, printer.ID)

		switch {
		case errors.Is(err, ErrCannotAsk):
			// The print itself is the check.
		case err != nil:
			return err
		case !allowed:
			return ErrDenied
		}
	}

	if !printableStates[printer.State] {
		return fmt.Errorf("%w: it is %s", ErrNotReady, strings.ToLower(printer.State))
	}

	return nil
}

// keep writes the file into the storage of this server, replacing a file of
// the same name, as PrusaSlicer asks.
func (s *Service) keep(ctx context.Context, upload Upload) error {
	file, err := os.Open(filepath.Join(s.data, filepath.FromSlash(upload.File)))
	if err != nil {
		return fmt.Errorf("open the upload: %w", err)
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("read the upload: %w", err)
	}

	if info.Size() == 0 {
		return errEmpty
	}

	return s.files.Write(ctx, upload.Principal, s.storage, upload.Name, info.Size(), file)
}

// checkName holds a name to what a printer prints and what its USB drive
// keeps.
func checkName(name string) (Reply, bool) {
	switch {
	case name == "" || name == "." || name == "..":
		return badRequest("The upload names no file."), true
	case !printExtensions[strings.ToLower(strings.TrimPrefix(path.Ext(name), "."))]:
		return Reply{
			status:  http.StatusUnsupportedMediaType,
			title:   "Unsupported Media Type",
			message: "Only a .gcode or .bgcode file can be sent to a printer.",
		}, true
	case strings.ContainsAny(name, forbiddenNameChars) || hidesCharacters(name):
		return badRequest("The file name holds a character a printer does not accept."), true
	case len(name) > maxFileNameBytes:
		return badRequest("The file name is too long."), true
	}

	return Reply{}, false
}

// hidesCharacters reports a name that is not UTF-8 or holds a control or an
// invisible formatting character.
func hidesCharacters(name string) bool {
	if !utf8.ValidString(name) {
		return true
	}

	return strings.ContainsFunc(name, func(r rune) bool {
		return unicode.IsControl(r) || unicode.Is(unicode.Cf, r)
	})
}

// truthy reads a structured-field boolean, ?1, and the forms older releases
// of PrusaSlicer and other clients send.
func truthy(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "?1", "1", "true", "yes":
		return true
	}

	return false
}
