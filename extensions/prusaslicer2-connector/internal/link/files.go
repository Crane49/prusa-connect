package link

import (
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// The folders of the data folder this package uses. The host writes a staged
// body into staged/ and removes it once the module answered.
const (
	// printFolder holds a file from the moment it is sent to a printer until
	// the sweep removes it, so the printer can read it.
	printFolder = "print"
	// incomingFolder holds a body that came on the request and the file
	// unpacked from a form, until the module answered.
	incomingFolder = "incoming"
)

// errEmpty refuses an upload with no bytes.
var errEmpty = errors.New("link: the file is empty")

// bodyOf is the body's path relative to the data folder. A staged body is
// already there. A body that came on the request, because the operator did
// not approve the staged route, is written to incoming/ first, and discard
// removes it.
func (s *Service) bodyOf(r *http.Request, staged string) (string, error) {
	if staged != "" {
		return staged, nil
	}

	file, err := s.create()
	if err != nil {
		return "", err
	}

	if r.Body != nil {
		_, err = io.Copy(file.out, r.Body)
	}

	if closeErr := file.out.Close(); err == nil {
		err = closeErr
	}

	if err != nil {
		s.discard(file.rel)

		return "", fmt.Errorf("read the upload: %w", err)
	}

	return file.rel, nil
}

// incoming is a new file in incoming/.
type incoming struct {
	out *os.File
	rel string
}

// create makes a new file in incoming/.
func (s *Service) create() (incoming, error) {
	dir := filepath.Join(s.data, incomingFolder)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return incoming{}, fmt.Errorf("make the incoming folder: %w", err)
	}

	out, err := os.CreateTemp(dir, "upload-*")
	if err != nil {
		return incoming{}, fmt.Errorf("make a file for the upload: %w", err)
	}

	return incoming{out: out, rel: incomingFolder + "/" + filepath.Base(out.Name())}, nil
}

// discard removes a file of incoming/. The host removes a staged body itself,
// and a file the print moved away is gone already.
func (s *Service) discard(rel string) {
	if rel == "" || !strings.HasPrefix(rel, incomingFolder+"/") {
		return
	}

	_ = os.Remove(filepath.Join(s.data, filepath.FromSlash(rel)))
}

// form is what a form upload carried.
type form struct {
	// file is the unpacked file, relative to the data folder.
	file   string
	name   string
	folder string
	print  bool
}

// unpack reads a multipart form from body: the part named file into
// incoming/, and the fields path and print.
func (s *Service) unpack(contentType, body string) (form, error) {
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil || mediaType != "multipart/form-data" || params["boundary"] == "" {
		return form{}, errNotAForm
	}

	in, err := os.Open(filepath.Join(s.data, filepath.FromSlash(body)))
	if err != nil {
		return form{}, fmt.Errorf("open the upload: %w", err)
	}
	defer in.Close()

	var (
		out    form
		reader = multipart.NewReader(in, params["boundary"])
	)

	for {
		part, nextErr := reader.NextPart()
		if errors.Is(nextErr, io.EOF) {
			break
		}

		if nextErr != nil {
			s.discard(out.file)

			return form{}, fmt.Errorf("%w: %w", errNotAForm, nextErr)
		}

		if err = s.readPart(part, &out); err != nil {
			s.discard(out.file)

			return form{}, err
		}
	}

	if out.file == "" {
		return form{}, errNoFilePart
	}

	return out, nil
}

// maxField bounds a form field that is not the file.
const maxField = 4 << 10

// readPart reads one part of the form into out.
func (s *Service) readPart(part *multipart.Part, out *form) error {
	defer part.Close()

	switch part.FormName() {
	case "file":
		if out.file != "" {
			return fmt.Errorf("%w: the form holds two files", errNotAForm)
		}

		file, err := s.create()
		if err != nil {
			return err
		}

		_, err = io.Copy(file.out, part)
		if closeErr := file.out.Close(); err == nil {
			err = closeErr
		}

		out.file = file.rel
		out.name = part.FileName()

		if err != nil {
			return fmt.Errorf("unpack the file: %w", err)
		}
	case "path":
		value, err := io.ReadAll(io.LimitReader(part, maxField))
		if err != nil {
			return fmt.Errorf("%w: %w", errNotAForm, err)
		}

		out.folder = strings.Trim(string(value), "/")
	case "print":
		value, err := io.ReadAll(io.LimitReader(part, maxField))
		if err != nil {
			return fmt.Errorf("%w: %w", errNotAForm, err)
		}

		out.print = truthy(string(value))
	}

	return nil
}

// The refusals of a form.
var (
	errNotAForm   = errors.New("link: the body is not a multipart form")
	errNoFilePart = errors.New("link: the form holds no file")
)

// How long the module keeps its files. A file sent to a printer stays in the
// print folder while the printer reads it, which takes over an hour for the
// largest file on a slow network, and nothing tells the module when the
// transfer ended. So it stays a day, and nothing removes it sooner. The
// host refuses an upload when the disk has no room for it.
const (
	// printKeep is how long a file sent to a printer stays.
	printKeep = 24 * time.Hour
	// incomingKeep is how long a file of incoming/ stays. An upload in
	// progress writes its file now, so one older than this was left behind
	// by a process that stopped.
	incomingKeep = 30 * time.Minute
	// SweepEvery is how often Sweeper sweeps when no print arrives.
	SweepEvery = time.Hour
)

// Sweep deletes every file sent to a printer more than printKeep ago, and the
// files of incoming/ older than incomingKeep. It runs when the module starts,
// before each print and every SweepEvery.
func (s *Service) Sweep() {
	now := s.now()
	removeOlder(filepath.Join(s.data, incomingFolder), now.Add(-incomingKeep))
	removeOlder(filepath.Join(s.data, printFolder), now.Add(-printKeep))
}

// removeOlder removes each entry of folder last changed before limit. An
// entry the sweep cannot remove is tried again at the next one.
func removeOlder(folder string, limit time.Time) {
	entries, err := os.ReadDir(folder)
	if err != nil {
		return
	}

	for _, entry := range entries {
		info, infoErr := entry.Info()
		if infoErr == nil && info.ModTime().Before(limit) {
			_ = os.RemoveAll(filepath.Join(folder, entry.Name()))
		}
	}
}
