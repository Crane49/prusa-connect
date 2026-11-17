// Package diagnosticsapi is how a module adds files to the diagnostics
// bundle an admin downloads from Settings.
//
// A module names MethodCollect in its Info.Serves and answers it with Serve.
// The host asks page by page and stops at its own limit, so answer with what
// matters most first. For a log that is the newest lines.
//
// The host scrubs every line before it writes it: addresses, MACs, tokens and
// paths are replaced. A module still leaves out what it knows to be secret.
package diagnosticsapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

// MethodCollect asks a module for one page of its files. It takes a Request
// and answers a Page.
const MethodCollect wirerpc.Method = "diagnostics.collect"

// MaxNameBytes bounds a file name.
const MaxNameBytes = 128

// MaxPageBytes bounds the text of one page, so that the page fits one wire
// frame after JSON escaping.
const MaxPageBytes = 256 << 10

// MaxPageFiles bounds the entries of one page. The host also takes at most
// 1024 files from one module across all its pages.
const MaxPageFiles = 512

// ErrPage reports a page the host will not take.
var ErrPage = errors.New("diagnosticsapi: the module answered an unusable page")

// Request is what the host sends. Cursor is empty on the first call, and is
// the Cursor of the previous page after that.
type Request struct {
	Cursor string `json:"cursor,omitempty"`
}

// Page is one answer. An empty Cursor is the last page.
type Page struct {
	Files  []File `json:"files"`
	Cursor string `json:"cursor,omitempty"`
}

// File is text for one file in the bundle. Text with the same Name on a
// later page is appended to the same file. A file whose text is empty adds
// nothing, and the host writes no file for it.
type File struct {
	// Name is a relative path that passes ValidName, such as
	// "syslog/printer-1.log". The host replaces a part that names an address
	// or a secret with a hash. It refuses a name that differs from an earlier
	// one only in case, and one that uses an earlier file as a folder.
	Name string `json:"name"`
	// Text is UTF-8 text. A log is one line per entry.
	Text string `json:"text"`
}

// Validate reports what makes a page unusable. The host runs it on every
// answer before it reads a file.
func (p Page) Validate() error {
	if len(p.Files) > MaxPageFiles {
		return fmt.Errorf("%w: the page holds %d files, and a page holds at most %d",
			ErrPage, len(p.Files), MaxPageFiles)
	}

	total := 0

	for _, file := range p.Files {
		if !ValidName(file.Name) {
			return fmt.Errorf("%w: the name %q is not a relative path of letters, digits, '.', '_', '-' and '/'",
				ErrPage, file.Name)
		}

		if !utf8.ValidString(file.Text) {
			return fmt.Errorf("%w: %s is not UTF-8 text", ErrPage, file.Name)
		}

		total += len(file.Text)
	}

	if total > MaxPageBytes {
		return fmt.Errorf(
			"%w: the page holds %d bytes of text, and a page holds at most %d",
			ErrPage,
			total,
			MaxPageBytes,
		)
	}

	return nil
}

// ValidName reports whether name may name a file: a relative path of at most
// MaxNameBytes letters, digits, '.', '_', '-' and '/', with no empty, "." or
// ".." segment. A segment that is not valid on Windows is refused too: a
// device name such as "con" or "aux.log", and a segment that ends with '.'.
func ValidName(name string) bool {
	if name == "" || len(name) > MaxNameBytes {
		return false
	}

	for _, r := range name {
		if !nameRune(r) {
			return false
		}
	}

	for segment := range strings.SplitSeq(name, "/") {
		if segment == "" || strings.HasSuffix(segment, ".") || windowsDevice(segment) {
			return false
		}
	}

	return true
}

// windowsDevice reports whether a segment names a device on Windows, with or
// without an extension, in any case.
func windowsDevice(segment string) bool {
	base, _, _ := strings.Cut(strings.ToLower(segment), ".")

	switch base {
	case "con", "prn", "aux", "nul":
		return true
	}

	if len(base) == len("com1") && (strings.HasPrefix(base, "com") || strings.HasPrefix(base, "lpt")) {
		return base[3] >= '1' && base[3] <= '9'
	}

	return false
}

func nameRune(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') ||
		r == '.' || r == '_' || r == '-' || r == '/'
}

// Serve builds the handler a module answers MethodCollect with. collect is
// called once per page with the cursor the host sent back. Each page is
// validated before it is sent.
func Serve(collect func(ctx context.Context, cursor string) (Page, error)) wirerpc.Handler {
	return func(ctx context.Context, call wirerpc.Call) (any, error) {
		var request Request

		if len(call.Params) != 0 && string(call.Params) != "null" {
			if err := json.Unmarshal(call.Params, &request); err != nil {
				return nil, fmt.Errorf("diagnosticsapi: read the request: %w", err)
			}
		}

		page, err := collect(ctx, request.Cursor)
		if err != nil {
			return nil, err
		}

		if err = page.Validate(); err != nil {
			return nil, err
		}

		if page.Files == nil {
			page.Files = []File{}
		}

		return page, nil
	}
}
