package files

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
)

// ExtensionName is the key of the extension's entry in the extension join
// data of a grant.
const ExtensionName fractal.Identifier = "files"

// joinVersion is the version of the files entry and of its http part that
// this package reads and writes.
const joinVersion = 1

// JoinEntry is the files entry a parent puts in every grant it issues.
type JoinEntry struct {
	// Version is the version of the entry, 1.
	Version uint `json:"version"`
	// HTTP is the join data of the HTTP binding. A parent always sets it.
	HTTP *HTTPEntry `json:"http,omitempty"`
}

// HTTPEntry is the http part of a files entry.
type HTTPEntry struct {
	// Version is the version of the part, 1.
	Version uint `json:"version"`
	// BaseURL is where the parent serves its objects, under BaseURL +
	// "/objects/". CheckBaseURL says what it may be.
	BaseURL string `json:"baseUrl"`
}

// ParseJoinEntry reads the files entry of a parent's grant. It returns
// ErrUnsupportedVersion for an unsupported version and for an entry with no
// http part, and refuses a base URL CheckBaseURL refuses. allowCleartext
// lets the base URL use http. On any refusal, a node must treat the grant as
// one of a protocol version it does not support.
func ParseJoinEntry(raw json.RawMessage, allowCleartext bool) (JoinEntry, error) {
	var e JoinEntry
	if err := json.Unmarshal(raw, &e); err != nil {
		return JoinEntry{}, fmt.Errorf("%w: the %s entry: %w", ErrMalformed, ExtensionName, err)
	}
	if e.Version != joinVersion {
		return JoinEntry{}, fmt.Errorf("%w: %s entry version %d", ErrUnsupportedVersion, ExtensionName, e.Version)
	}
	if e.HTTP == nil {
		return JoinEntry{}, fmt.Errorf("%w: the %s entry has no http part", ErrUnsupportedVersion, ExtensionName)
	}
	if e.HTTP.Version != joinVersion {
		return JoinEntry{}, fmt.Errorf("%w: http part version %d", ErrUnsupportedVersion, e.HTTP.Version)
	}
	if err := CheckBaseURL(e.HTTP.BaseURL, allowCleartext); err != nil {
		return JoinEntry{}, err
	}
	return e, nil
}

// Marshal returns the entry as JSON for the extension join data of a grant.
// It refuses an entry ParseJoinEntry would refuse with cleartext allowed, so
// a parent never issues a grant no child can read.
func (e JoinEntry) Marshal() (json.RawMessage, error) {
	raw, err := json.Marshal(e)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrMalformed, err)
	}
	if _, err = ParseJoinEntry(raw, true); err != nil {
		return nil, err
	}
	return raw, nil
}

// CheckBaseURL returns nil when raw is an absolute URL with a host name, no
// user name, no query and no fragment, whose scheme is https, or http when
// allowCleartext is true. The error wraps ErrMalformed.
func CheckBaseURL(raw string, allowCleartext bool) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("%w: base URL %q: %w", ErrMalformed, raw, err)
	}
	switch {
	case u.Scheme != "https" && (u.Scheme != "http" || !allowCleartext):
		return fmt.Errorf("%w: base URL %q does not use https", ErrMalformed, raw)
	case u.Hostname() == "":
		return fmt.Errorf("%w: base URL %q has no host", ErrMalformed, raw)
	case u.User != nil:
		return fmt.Errorf("%w: base URL %q carries a user name", ErrMalformed, raw)
	// url.Parse drops an empty query or fragment, so the text is checked.
	case strings.ContainsAny(raw, "?#"):
		return fmt.Errorf("%w: base URL %q has a query or a fragment", ErrMalformed, raw)
	}
	return nil
}
