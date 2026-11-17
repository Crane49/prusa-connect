package manifest

import (
	"fmt"
	"slices"
	"strings"
)

// The file types a module names: the files it reads for the core under
// file_analysis, and the files an entry of a file slot is shown for.

// FileAnalysis is a module's offer to read files of some types for the core:
// the picture, what a printer needs, and what the file page shows.
type FileAnalysis struct {
	// Types is the file types the module reads. Each passes ValidFileType.
	Types []string
}

// MaxFileTypes is how many file types one list may name.
const MaxFileTypes = 16

// maxFileTypeBytes bounds one file type, a dot and an extension.
const maxFileTypeBytes = 64

// readFileAnalysis reads the file_analysis block, and answers nil for a module
// that reads no file for the core.
func readFileAnalysis(tree map[string]any) (*FileAnalysis, error) {
	block, declared, err := node(tree, "", "file_analysis")
	if err != nil || !declared {
		return nil, err
	}

	if err = refuseUnknown(block, "file_analysis", []string{"types"}); err != nil {
		return nil, err
	}

	types, err := FileTypes(block, "file_analysis", "types")
	if err != nil {
		return nil, err
	}

	if len(types) == 0 {
		return nil, fmt.Errorf("%w: file_analysis.types is absent, expected the file types the module "+
			"reads, such as [.3mf]", ErrInvalid)
	}

	return &FileAnalysis{Types: types}, nil
}

// FileTypes reads an optional list of file types under key. Each entry must
// be a lowercase extension with its dot, such as .3mf. A storage reports no
// media type, so a file is known by its name.
func FileTypes(entry map[string]any, where, key string) ([]string, error) {
	types, err := textList(entry, where, key)
	if err != nil {
		return nil, err
	}

	place := at(where, key)

	if _, present := entry[key]; present && len(types) == 0 {
		return nil, fmt.Errorf("%w: %s is empty; drop the key for every file", ErrInvalid, place)
	}

	if len(types) > MaxFileTypes {
		return nil, fmt.Errorf("%w: %s names %d types, and the limit is %d",
			ErrInvalid, place, len(types), MaxFileTypes)
	}

	for index, fileType := range types {
		if !ValidFileType(fileType) {
			return nil, fmt.Errorf("%w: %s[%d] is %q, expected a lowercase extension with its dot, "+
				"such as .3mf", ErrInvalid, place, index, fileType)
		}

		if slices.Contains(types[:index], fileType) {
			return nil, fmt.Errorf("%w: %s names %s twice", ErrInvalid, place, fileType)
		}
	}

	return types, nil
}

// ValidFileType reports whether a file type is a lowercase extension with its
// dot.
func ValidFileType(fileType string) bool {
	if fileType == "" || len(fileType) > maxFileTypeBytes || fileType != strings.ToLower(fileType) {
		return false
	}

	extension, dotted := strings.CutPrefix(fileType, ".")

	return dotted && extension != "" && !strings.Contains(extension, ".") && fileTypeChars(extension)
}

// fileTypeChars reports whether text is lowercase letters, digits and the
// joiners an extension uses.
func fileTypeChars(text string) bool {
	for _, r := range text {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '+', r == '_':
		default:
			return false
		}
	}

	return true
}

// MatchesFileType reports whether a file of this name is of one of the types
// listed. An extension matches the end of the name without regard to case.
// An empty list matches every file.
func MatchesFileType(types []string, name string) bool {
	if len(types) == 0 {
		return true
	}

	lower := strings.ToLower(name)

	for _, fileType := range types {
		if len(lower) > len(fileType) && strings.HasSuffix(lower, fileType) {
			return true
		}
	}

	return false
}
