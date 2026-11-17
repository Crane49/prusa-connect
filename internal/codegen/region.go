package codegen

import (
	"bytes"
	"errors"
	"strings"
)

// Region is the marker pair a generated region sits between on a page a
// person owns. The opening marker names what the region is generated for and
// says "do not edit", so a reader of the source knows which lines a program
// wrote. Everything outside the two markers is the person's and is copied
// through untouched.
//
// Two commands write such a page. The protocol is the part they must agree
// on, so it lives here, and each command keeps only the subject and the fix
// its marker names.
type Region struct {
	// Start opens the region. The subject follows it, then the rest of the
	// sentence, so a page's marker reads as a whole line.
	Start string
	// End closes the region, on a line of its own.
	End string
}

// ErrNoRegion is a page that exists and has no region to write: somebody
// removed a marker, or wrote the file by hand without one.
var ErrNoRegion = errors.New("the page has no generated region: both markers are needed, opening first")

// Wrap puts a body between the two markers. subject is what the opening marker
// names; fix is what a person does instead of editing.
func (r Region) Wrap(subject, fix, body string) []byte {
	var region strings.Builder

	region.WriteString(r.Start)
	region.WriteString(subject)
	region.WriteString(". Do not edit: ")
	region.WriteString(fix)
	region.WriteString(" -->\n\n")
	region.WriteString(strings.TrimSpace(body))
	region.WriteString("\n\n")
	region.WriteString(r.End)

	return []byte(region.String())
}

// Splice replaces the region of a committed page with region, from the start
// of the opening marker's line to the end of the closing marker's line.
func (r Region) Splice(committed, region []byte) ([]byte, error) {
	start, end, err := r.bounds(committed)
	if err != nil {
		return nil, err
	}

	out := make([]byte, 0, len(committed)-(end-start)+len(region))
	out = append(out, committed[:start]...)
	out = append(out, region...)
	out = append(out, committed[end:]...)

	return out, nil
}

// HandWritten is the part of a page a person wrote: everything outside the
// markers.
func (r Region) HandWritten(committed []byte) (string, error) {
	start, end, err := r.bounds(committed)
	if err != nil {
		return "", err
	}

	return string(committed[:start]) + string(committed[end:]), nil
}

// bounds is where the region starts and where the text after it resumes.
func (r Region) bounds(committed []byte) (int, int, error) {
	start := bytes.Index(committed, []byte(r.Start))
	end := bytes.Index(committed, []byte(r.End))

	if start < 0 || end < start {
		return 0, 0, ErrNoRegion
	}

	return start, end + len(r.End), nil
}
