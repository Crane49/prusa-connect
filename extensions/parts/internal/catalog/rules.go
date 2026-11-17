package catalog

import (
	"fmt"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// The bounds every record is held to. A value a person cannot have meant is
// refused where it is written rather than found later on a page.
const (
	// MaxNameBytes bounds the name of a folder and of a part.
	MaxNameBytes = 120
	// MaxSummaryBytes bounds the one line under a part's name.
	MaxSummaryBytes = 280
	// MaxFolderDescriptionBytes bounds what a person writes about a folder.
	MaxFolderDescriptionBytes = 2 << 10
	// MaxDescriptionBytes bounds a part's long text. 16 KiB is several
	// printed pages.
	MaxDescriptionBytes = 16 << 10
	// MaxTags is how many tags one part carries.
	MaxTags = 16
	// MaxTagBytes bounds one tag.
	MaxTagBytes = 32
	// MaxNoteBytes bounds what a person writes about one version.
	MaxNoteBytes = 2 << 10
	// MaxFilesPerVersion is how many files one version holds.
	MaxFilesPerVersion = 32
	// MaxVersions is how many versions one part keeps.
	MaxVersions = 200
	// MaxCommentBytes bounds one comment.
	MaxCommentBytes = 4 << 10
	// MaxComments is how many comments one part keeps.
	MaxComments = 500
	// MaxDepth is how deep the folder tree goes. A tree deeper than this is
	// one nobody can read on a screen.
	MaxDepth = 12
	// MaxFileNameBytes bounds the last segment of a file's path.
	MaxFileNameBytes = 200
	// MaxPathBytes bounds the whole path of a file inside its storage.
	MaxPathBytes = 512
)

// tagPattern is what one tag may be: lower-case letters and digits of any
// script, joined by single hyphens.
var tagPattern = regexp.MustCompile(`^[\p{Ll}\p{Lo}\p{N}]+(?:-[\p{Ll}\p{Lo}\p{N}]+)*$`)

// uploadPattern is the segment the page names one upload by, between the
// part's own folder in the storage and the file name.
var uploadPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}$`)

// invalid is ErrInvalid with the sentence that says what to change.
func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, args...))
}

// text trims a value and holds it to a length and to printable characters.
// The field name goes into the refusal, so the person reads which field is
// too long.
func text(field, value string, limit int, required bool) (string, error) {
	held := strings.TrimSpace(value)

	switch {
	case !utf8.ValidString(held):
		return "", invalid("%s is not valid UTF-8", field)
	case required && held == "":
		return "", invalid("%s is empty, and a %s is needed", field, field)
	case len(held) > limit:
		return "", invalid("%s is %d bytes, and the limit is %d", field, len(held), limit)
	}

	return held, nil
}

// line is text that is one line: a name, a summary. A line break in a name
// would break the listing it is drawn in.
func line(field, value string, limit int, required bool) (string, error) {
	held, err := text(field, value, limit, required)
	if err != nil {
		return "", err
	}

	if strings.ContainsFunc(held, unicode.IsControl) {
		return "", invalid("%s is one line, and it holds a line break or a control character", field)
	}

	return held, nil
}

// block is text that may run over several lines: a description, a comment.
// A tab and a line break are kept and every other control character refused.
func block(field, value string, limit int, required bool) (string, error) {
	held, err := text(field, strings.ReplaceAll(value, "\r\n", "\n"), limit, required)
	if err != nil {
		return "", err
	}

	if strings.ContainsFunc(held, func(r rune) bool {
		return unicode.IsControl(r) && r != '\n' && r != '\t'
	}) {
		return "", invalid("%s holds a control character", field)
	}

	return held, nil
}

// tags lower-cases, trims and orders a part's tags, drops a repeat, and
// refuses one that is not a tag.
func tags(written []string) ([]string, error) {
	out := make([]string, 0, len(written))

	for _, raw := range written {
		tag := strings.ToLower(strings.TrimSpace(raw))
		if tag == "" {
			continue
		}

		if len(tag) > MaxTagBytes {
			return nil, invalid("the tag %q is %d bytes, and the limit is %d", tag, len(tag), MaxTagBytes)
		}

		if !tagPattern.MatchString(tag) {
			return nil, invalid("the tag %q is not letters and digits joined by hyphens", tag)
		}

		if !slices.Contains(out, tag) {
			out = append(out, tag)
		}
	}

	if len(out) > MaxTags {
		return nil, invalid("a part carries at most %d tags, and %d were sent", MaxTags, len(out))
	}

	slices.Sort(out)

	return out, nil
}

// PartDir is the folder inside the storage that holds one part's files, under
// the root the operator named.
func PartDir(root string, id int) string {
	return root + "/" + strconv.Itoa(id)
}

// filePath holds one path the page uploaded a file to against the place a
// file of this part has to be in: the part's own folder in the storage, one
// upload segment, and the file name.
//
// The path is checked here and not only by the storage, because a version
// that named a file outside its part's folder would make deleting the part
// delete somebody else's file.
func filePath(root string, part int, written string) (name string, err error) {
	if len(written) > MaxPathBytes {
		return "", invalid("the path is %d bytes, and the limit is %d", len(written), MaxPathBytes)
	}

	if strings.ContainsAny(written, "\\\x00") || path.Clean(written) != written {
		return "", invalid("the path %q is not a clean path", written)
	}

	rest, found := strings.CutPrefix(written, PartDir(root, part)+"/")
	if !found {
		return "", invalid("the path %q is not inside %s, the folder of part %d",
			written, PartDir(root, part), part)
	}

	upload, file, found := strings.Cut(rest, "/")
	if !found || strings.Contains(file, "/") {
		return "", invalid("the path %q is not <folder>/<upload>/<file name>", written)
	}

	if !uploadPattern.MatchString(upload) {
		return "", invalid("the upload segment %q is not lower-case letters, digits and hyphens", upload)
	}

	if file == "" || file == "." || file == ".." || len(file) > MaxFileNameBytes {
		return "", invalid("the file name %q is empty or longer than %d bytes", file, MaxFileNameBytes)
	}

	if strings.ContainsFunc(file, unicode.IsControl) {
		return "", invalid("the file name %q holds a control character", file)
	}

	return file, nil
}

// ValidRoot holds the folder the operator named for the catalogue's files to
// the shape filePath reads: a relative path of plain segments.
func ValidRoot(root string) error {
	if root == "" || len(root) > MaxNameBytes || path.Clean(root) != root ||
		strings.HasPrefix(root, "/") || strings.HasPrefix(root, "..") {
		return invalid("root %q is not a relative path inside the storage", root)
	}

	for segment := range strings.SplitSeq(root, "/") {
		if segment == "." || segment == ".." || strings.ContainsFunc(segment, unicode.IsControl) ||
			strings.ContainsAny(segment, "\\") {
			return invalid("root %q holds the segment %q", root, segment)
		}
	}

	return nil
}
