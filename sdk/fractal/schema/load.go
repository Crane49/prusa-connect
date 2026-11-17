package schema

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"strconv"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
)

// The reasons a definition file is invalid. A *LoadError wraps one of them.
var (
	// ErrEmptyFile reports that a file declares neither an interface nor an
	// enumeration.
	ErrEmptyFile = errors.New("file declares no interface and no enumeration")
	// ErrDuplicateName reports that the same name is declared twice: an
	// interface, an enumeration, an enumeration value, a property, a method or
	// a parameter.
	ErrDuplicateName = errors.New("name is declared twice")
	// ErrEmptyEnum reports that an enumeration lists no value.
	ErrEmptyEnum = errors.New("enumeration lists no value")
	// ErrEmptyEnumValue reports that an enumeration lists an empty string.
	ErrEmptyEnumValue = errors.New("enumeration value is empty")
	// ErrUnknownType reports that a type name is neither a built-in type nor a
	// declared enumeration.
	ErrUnknownType = errors.New("type is not a built-in type or a declared enumeration")
	// ErrUnknownAccess reports that an access mode is neither "ro" nor "rw".
	ErrUnknownAccess = errors.New(`access is not "ro" or "rw"`)
	// ErrRequiredAfterOptional reports that a required parameter follows an
	// optional one. Optional parameters must come last.
	ErrRequiredAfterOptional = errors.New("required parameter follows an optional one")
	// ErrDuplicateKey reports that a JSON object in the file has the same key
	// twice.
	ErrDuplicateKey = errors.New("json object holds the same key twice")
	// ErrMissingInterface reports that a file lists properties or methods, or
	// states an exposure, and does not name the interface they belong to.
	ErrMissingInterface = errors.New("properties, methods or an exposure without an interface name")
	// ErrUnknownExposure reports that an interface states no exposure, or one
	// that is not "upward", "downward" or "both".
	ErrUnknownExposure = errors.New(`exposure is not "upward", "downward" or "both"`)
)

// LoadError says which definition file is invalid, where in the file, and why.
type LoadError struct {
	// Source is the name the caller gave the file.
	Source string
	// Where is the place in the file, for example "properties[2].type". It is
	// empty when the error is about the whole file.
	Where string
	// Err is the reason.
	Err error
}

// Error returns the file name, the place in the file and the reason.
func (e *LoadError) Error() string {
	if e.Where == "" {
		return e.Source + ": " + e.Err.Error()
	}
	return e.Source + " " + e.Where + ": " + e.Err.Error()
}

// Unwrap returns the reason the file is invalid.
func (e *LoadError) Unwrap() error { return e.Err }

// fileJSON is the JSON structure of one definition file. The package comment
// shows an example.
type fileJSON struct {
	Interface  string       `json:"interface,omitempty"`
	Exposure   string       `json:"exposure,omitempty"`
	Enums      []enumJSON   `json:"enums,omitempty"`
	Properties []propJSON   `json:"properties,omitempty"`
	Methods    []methodJSON `json:"methods,omitempty"`
}

// enumJSON is one declared enumeration.
type enumJSON struct {
	Name   string   `json:"name"`
	Values []string `json:"values"`
}

// propJSON is one declared property.
type propJSON struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Access   string `json:"access"`
	Nullable bool   `json:"nullable,omitempty"`
}

// methodJSON is one declared method.
type methodJSON struct {
	Name    string      `json:"name"`
	Params  []paramJSON `json:"params,omitempty"`
	Returns string      `json:"returns,omitempty"`
}

// paramJSON is one declared parameter.
type paramJSON struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Optional bool   `json:"optional,omitempty"`
}

// loadedFile is one parsed file and its name.
type loadedFile struct {
	source string
	doc    fileJSON
}

// Builder collects definition files, and Build makes a Registry from them. A
// Builder is not safe for concurrent use. The Registry that Build returns is.
type Builder struct {
	files []loadedFile
}

// NewBuilder returns an empty Builder.
func NewBuilder() *Builder { return &Builder{} }

// Load reads every file in fsys that matches one of the patterns and builds a
// Registry from those files. With no pattern it reads "*.json" in the root of
// fsys. A pattern has the syntax of path.Match.
func Load(fsys fs.FS, patterns ...string) (*Registry, error) {
	b := NewBuilder()
	if err := b.AddFS(fsys, patterns...); err != nil {
		return nil, err
	}
	return b.Build()
}

// AddJSON parses one definition file and adds it to the Builder. source is the
// file name that a *LoadError reports. AddJSON checks everything except the
// type names. Build checks them, because a file added later may declare the
// enumeration.
func (b *Builder) AddJSON(source string, data []byte) error {
	if err := checkDuplicateKeys(source, data); err != nil {
		return err
	}
	var doc fileJSON
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&doc); err != nil {
		return &LoadError{Source: source, Err: err}
	}
	if err := checkTrailing(dec); err != nil {
		return &LoadError{Source: source, Err: err}
	}
	if err := checkFile(source, doc); err != nil {
		return err
	}
	b.files = append(b.files, loadedFile{source: source, doc: doc})
	return nil
}

// AddFS reads every file in fsys that matches one of the patterns and calls
// AddJSON on it. With no pattern it reads "*.json" in the root of fsys.
func (b *Builder) AddFS(fsys fs.FS, patterns ...string) error {
	if len(patterns) == 0 {
		patterns = []string{"*.json"}
	}
	for _, pattern := range patterns {
		names, err := fs.Glob(fsys, pattern)
		if err != nil {
			return fmt.Errorf("pattern %q: %w", pattern, err)
		}
		for _, name := range names {
			data, readErr := fs.ReadFile(fsys, name)
			if readErr != nil {
				return fmt.Errorf("read %q: %w", name, readErr)
			}
			if addErr := b.AddJSON(name, data); addErr != nil {
				return addErr
			}
		}
	}
	return nil
}

// checkTrailing returns an error when the file has anything after its one JSON
// object. Decoder.More misses a stray bracket, so it reads the next token too.
func checkTrailing(dec *json.Decoder) error {
	if dec.More() {
		return errors.New("more than one json value in the file")
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return errors.New("the file has more text after its json value")
	}
	return nil
}

// checkFile applies every rule that one file can be checked against without
// the other files. That excludes the type names.
func checkFile(source string, doc fileJSON) error {
	if doc.Interface == "" && len(doc.Enums) == 0 {
		return &LoadError{Source: source, Err: ErrEmptyFile}
	}
	if err := checkEnums(source, doc.Enums); err != nil {
		return err
	}
	if doc.Interface == "" {
		if len(doc.Properties) > 0 || len(doc.Methods) > 0 || doc.Exposure != "" {
			return &LoadError{Source: source, Where: "interface", Err: ErrMissingInterface}
		}
		return nil
	}
	if _, err := fractal.ParseInterfaceName(doc.Interface); err != nil {
		return &LoadError{Source: source, Where: "interface", Err: err}
	}
	if err := checkProps(source, doc.Properties); err != nil {
		return err
	}
	if err := checkMethods(source, doc.Methods); err != nil {
		return err
	}
	if _, ok := parseExposure(doc.Exposure); !ok {
		return &LoadError{Source: source, Where: "exposure", Err: ErrUnknownExposure}
	}
	return nil
}

// checkEnums checks every declared enumeration: a valid name, no name twice in
// the file, at least one value, and valid values.
func checkEnums(source string, enums []enumJSON) error {
	seen := make(map[string]struct{}, len(enums))
	for i, e := range enums {
		where := "enums[" + strconv.Itoa(i) + "]"
		if _, err := fractal.ParseIdentifier(e.Name); err != nil {
			return &LoadError{Source: source, Where: where + ".name", Err: err}
		}
		if _, dup := seen[e.Name]; dup {
			return &LoadError{Source: source, Where: where + ".name", Err: ErrDuplicateName}
		}
		seen[e.Name] = struct{}{}
		if len(e.Values) == 0 {
			return &LoadError{Source: source, Where: where + ".values", Err: ErrEmptyEnum}
		}
		if err := checkEnumValues(source, where, e.Values); err != nil {
			return err
		}
	}
	return nil
}

// checkEnumValues returns an error for an empty value and for a value listed
// twice.
func checkEnumValues(source, where string, values []string) error {
	seen := make(map[string]struct{}, len(values))
	for i, v := range values {
		at := where + ".values[" + strconv.Itoa(i) + "]"
		if v == "" {
			return &LoadError{Source: source, Where: at, Err: ErrEmptyEnumValue}
		}
		if _, dup := seen[v]; dup {
			return &LoadError{Source: source, Where: at, Err: ErrDuplicateName}
		}
		seen[v] = struct{}{}
	}
	return nil
}

// checkProps checks every declared property: a valid name, no name twice, and
// an access mode of "ro" or "rw".
func checkProps(source string, props []propJSON) error {
	seen := make(map[string]struct{}, len(props))
	for i, p := range props {
		where := "properties[" + strconv.Itoa(i) + "]"
		if _, err := fractal.ParsePropertyName(p.Name); err != nil {
			return &LoadError{Source: source, Where: where + ".name", Err: err}
		}
		if _, dup := seen[p.Name]; dup {
			return &LoadError{Source: source, Where: where + ".name", Err: ErrDuplicateName}
		}
		seen[p.Name] = struct{}{}
		if _, ok := ParseAccess(p.Access); !ok {
			return &LoadError{Source: source, Where: where + ".access", Err: ErrUnknownAccess}
		}
	}
	return nil
}

// checkMethods checks every declared method: a valid name, no name twice, and
// valid parameters.
func checkMethods(source string, methods []methodJSON) error {
	seen := make(map[string]struct{}, len(methods))
	for i, m := range methods {
		where := "methods[" + strconv.Itoa(i) + "]"
		if _, err := fractal.ParseMethodName(m.Name); err != nil {
			return &LoadError{Source: source, Where: where + ".name", Err: err}
		}
		if _, dup := seen[m.Name]; dup {
			return &LoadError{Source: source, Where: where + ".name", Err: ErrDuplicateName}
		}
		seen[m.Name] = struct{}{}
		if err := checkParams(source, where, m.Params); err != nil {
			return err
		}
	}
	return nil
}

// checkParams checks the parameters of one method: a valid name, no name twice,
// and no required parameter after an optional one.
func checkParams(source, where string, params []paramJSON) error {
	seen := make(map[string]struct{}, len(params))
	optional := false
	for i, p := range params {
		at := where + ".params[" + strconv.Itoa(i) + "]"
		if _, err := fractal.ParseIdentifier(p.Name); err != nil {
			return &LoadError{Source: source, Where: at + ".name", Err: err}
		}
		if _, dup := seen[p.Name]; dup {
			return &LoadError{Source: source, Where: at + ".name", Err: ErrDuplicateName}
		}
		seen[p.Name] = struct{}{}
		if p.Optional {
			optional = true
			continue
		}
		if optional {
			return &LoadError{Source: source, Where: at, Err: ErrRequiredAfterOptional}
		}
	}
	return nil
}
