package files

import (
	"fmt"
	"slices"
	"strings"
)

// Kind says what an entry of a listing names.
type Kind uint8

// The kinds of entry.
const (
	// KindFile names the blob that holds the file's content.
	KindFile Kind = 1
	// KindDirectory names the listing of the directory's entries.
	KindDirectory Kind = 2
)

// listingVersion is the version of the listing this package reads and
// writes.
const listingVersion = 1

// The number of values in a listing and in an entry.
const (
	listingFields = 2
	entryFields   = 4
)

// Entry is one entry of a listing.
type Entry struct {
	// Name is the name of the file or the directory, under the rule for
	// names.
	Name string
	// Kind says whether Hash names a blob or a listing.
	Kind Kind
	// Size is the length in bytes of the object Hash names. For a directory
	// that is the length of its listing, not of what it holds.
	Size uint64
	// Hash is the identity of the object.
	Hash Identity
}

// Listing is the content of one directory.
type Listing struct {
	// Entries are the entries in any order. Encode sorts them, and
	// DecodeListing returns them sorted.
	Entries []Entry
}

// EmptyListing is the identity of the listing of an empty directory, the
// bytes 82 01 80.
var EmptyListing = Sum([]byte{0x82, 0x01, 0x80})

// Encode returns the listing in deterministic CBOR, version 1, with its
// entries sorted by the UTF-8 bytes of their names. The identity of the
// listing is Sum of the result. It refuses a name that breaks the rule for
// names or that two entries share, a kind other than file and directory, and
// a zero hash. It does not change l.
func (l Listing) Encode() ([]byte, error) {
	entries := slices.Clone(l.Entries)
	slices.SortFunc(entries, func(a, b Entry) int { return strings.Compare(a.Name, b.Name) })
	b := appendHead(nil, majorArray, listingFields)
	b = appendHead(b, majorUint, listingVersion)
	b = appendHead(b, majorArray, uint64(len(entries)))
	for i, e := range entries {
		if err := CheckName(e.Name); err != nil {
			return nil, err
		}
		if i > 0 && entries[i-1].Name == e.Name {
			return nil, fmt.Errorf("%w: two entries are named %q", ErrBadName, e.Name)
		}
		if e.Kind != KindFile && e.Kind != KindDirectory {
			return nil, fmt.Errorf("%w: entry %q has kind %d", ErrMalformed, e.Name, e.Kind)
		}
		if e.Hash.IsZero() {
			return nil, fmt.Errorf("%w: entry %q has no hash", ErrMalformed, e.Name)
		}
		b = appendHead(b, majorArray, entryFields)
		b = appendHead(b, majorText, uint64(len(e.Name)))
		b = append(b, e.Name...)
		b = appendHead(b, majorUint, uint64(e.Kind))
		b = appendHead(b, majorUint, e.Size)
		hash := e.Hash.Bytes()
		b = appendHead(b, majorBytes, uint64(len(hash)))
		b = append(b, hash...)
	}
	return b, nil
}

// DecodeListing reads a listing. It reads the version first and refuses one
// other than 1 with ErrUnsupportedVersion. It refuses bytes that are not the
// deterministic CBOR Encode writes, and entries that are not sorted and
// unique, have a kind other than file and directory, or an identity it
// cannot read. It checks every name with CheckName.
func DecodeListing(b []byte) (Listing, error) {
	d := decoder{b: b}
	fields, err := d.head(majorArray)
	if err != nil {
		return Listing{}, err
	}
	if fields == 0 {
		return Listing{}, fmt.Errorf("%w: the listing has no version", ErrMalformed)
	}
	version, err := d.head(majorUint)
	if err != nil {
		return Listing{}, err
	}
	if version != listingVersion {
		return Listing{}, fmt.Errorf("%w: listing version %d", ErrUnsupportedVersion, version)
	}
	if fields != listingFields {
		return Listing{}, fmt.Errorf("%w: a listing has %d values, not %d", ErrMalformed, fields, listingFields)
	}
	count, err := d.head(majorArray)
	if err != nil {
		return Listing{}, err
	}
	var l Listing
	for range count {
		e, entryErr := d.entry()
		if entryErr != nil {
			return Listing{}, entryErr
		}
		if n := len(l.Entries); n > 0 && l.Entries[n-1].Name >= e.Name {
			return Listing{}, fmt.Errorf("%w: entry %q is not after %q", ErrMalformed, e.Name, l.Entries[n-1].Name)
		}
		l.Entries = append(l.Entries, e)
	}
	if len(d.b) != 0 {
		return Listing{}, fmt.Errorf("%w: %d bytes follow the listing", ErrMalformed, len(d.b))
	}
	return l, nil
}

// entry reads one entry of a listing.
func (d *decoder) entry() (Entry, error) {
	fields, err := d.head(majorArray)
	if err != nil {
		return Entry{}, err
	}
	if fields != entryFields {
		return Entry{}, fmt.Errorf("%w: an entry has %d values, not %d", ErrMalformed, fields, entryFields)
	}
	name, err := d.bytes(majorText)
	if err != nil {
		return Entry{}, err
	}
	if err = CheckName(string(name)); err != nil {
		return Entry{}, err
	}
	rawKind, err := d.head(majorUint)
	if err != nil {
		return Entry{}, err
	}
	var kind Kind
	switch rawKind {
	case uint64(KindFile):
		kind = KindFile
	case uint64(KindDirectory):
		kind = KindDirectory
	default:
		return Entry{}, fmt.Errorf("%w: entry %q has kind %d", ErrMalformed, name, rawKind)
	}
	size, err := d.head(majorUint)
	if err != nil {
		return Entry{}, err
	}
	raw, err := d.bytes(majorBytes)
	if err != nil {
		return Entry{}, err
	}
	hash, err := ParseBytes(raw)
	if err != nil {
		return Entry{}, err
	}
	return Entry{Name: string(name), Kind: kind, Size: size, Hash: hash}, nil
}
