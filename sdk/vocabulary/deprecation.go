// Package vocabulary defines the deprecation marker that every published
// vocabulary uses.
//
// A vocabulary is a closed set of names that a client depends on, such as the
// error codes, the event types, the resource kinds and the authorization
// actions. One release marks an entry as deprecated, and a later release
// removes it.
package vocabulary

import (
	"fmt"
	"strings"
)

// Window is the number of minor releases of the application that a deprecated
// entry stays published, counted from the release that deprecated it.
const Window = 3

// Deprecation marks an entry that is still published and that a later release
// removes. An entry with no Deprecation is not deprecated. An entry is removed
// only after it had this marker for Window minor releases.
type Deprecation struct {
	// Since is the application release that first published this marker,
	// written as three numbers such as 0.2.0. Window is counted from it.
	Since string `json:"since" doc:"The application release this entry was deprecated in."`
	// ReplacedBy is the name of the entry to use instead, in the same
	// vocabulary. It is empty when no entry replaces this one.
	ReplacedBy string `json:"replaced_by,omitempty" doc:"The entry to move to, when one takes over."`
}

// Deprecated reports whether d marks a deprecation. It is false for a nil
// marker and for a marker with an empty Since.
func (d *Deprecation) Deprecated() bool { return d != nil && strings.TrimSpace(d.Since) != "" }

// Sentence returns the sentence a generated reference page prints under a
// deprecated entry. It returns an empty string when d marks no deprecation.
func (d *Deprecation) Sentence() string {
	if !d.Deprecated() {
		return ""
	}

	if d.ReplacedBy == "" {
		return fmt.Sprintf("Deprecated in %s. It is removed in a later release and nothing replaces it.", d.Since)
	}

	return fmt.Sprintf("Deprecated in %s. Use `%s` instead.", d.Since, d.ReplacedBy)
}

// Deprecate returns a marker with the given release and replacement.
func Deprecate(since, replacedBy string) *Deprecation {
	return &Deprecation{Since: since, ReplacedBy: replacedBy}
}
