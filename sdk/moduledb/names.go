package moduledb

import (
	"cmp"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// StampLayout is the time layout of a dated migration's name: the UTC date and
// time the file was written, to the second, such as 20261027T093015.
const StampLayout = "20060102T150405"

// MigrationName is a migration's name read into the parts that order it. A
// name is dated, such as 20261027T093015_note_tags.sql, or numbered, such as
// 0003_note_words.sql. A Go step is named the same way and ends in .go. Give
// a new migration a dated name. A numbered migration that has shipped keeps
// its name.
type MigrationName struct {
	// Name is the whole name, such as 0003_note_words.sql.
	Name string
	// Number is the version of a numbered name, such as 3, and zero for a
	// dated one.
	Number int
	// Stamp is the date and time of a dated name, in StampLayout, and empty
	// for a numbered one.
	Stamp string
	// Step is whether the name is a Go step's (.go) and not a file's (.sql).
	Step bool
}

// Dated reports whether the name has a date and not a number.
func (n MigrationName) Dated() bool { return n.Stamp != "" }

// Compare orders two migrations the way they are applied. Every numbered
// migration comes before every dated one, numbered ones by their number and
// dated ones by their date. At one number or one date, the file comes before
// the step, and then the names are compared.
func (n MigrationName) Compare(other MigrationName) int {
	if n.Dated() != other.Dated() {
		if n.Dated() {
			return 1
		}

		return -1
	}

	if order := cmp.Or(cmp.Compare(n.Number, other.Number), strings.Compare(n.Stamp, other.Stamp)); order != 0 {
		return order
	}

	if n.Step != other.Step {
		if n.Step {
			return 1
		}

		return -1
	}

	return strings.Compare(n.Name, other.Name)
}

//nolint:gochecknoglobals // compiled patterns read by ParseMigrationName, never written.
var (
	// numberedName is a numbered name. Anything may follow the number.
	numberedName = regexp.MustCompile(`^([0-9]+)_[^/]*\.(sql|go)$`)
	// datedName is a stamp, an underscore and lower case words joined by
	// underscores.
	datedName = regexp.MustCompile(`^([0-9]{8}T[0-9]{6})_[a-z0-9]+(?:_[a-z0-9]+)*\.(sql|go)$`)
)

// ParseMigrationName reads a file's or a step's name. It returns
// ErrMigrationName for a name that is neither dated nor numbered, and for a
// stamp that is not a real date and time.
func ParseMigrationName(name string) (MigrationName, error) {
	if match := datedName.FindStringSubmatch(name); match != nil {
		if _, err := time.Parse(StampLayout, match[1]); err != nil {
			return MigrationName{}, fmt.Errorf("%w: %s. The date and time %s is not one", ErrMigrationName,
				name, match[1])
		}

		return MigrationName{Name: name, Stamp: match[1], Step: match[2] == "go"}, nil
	}

	if match := numberedName.FindStringSubmatch(name); match != nil {
		// Eight digits are a date written without its time, which would sort
		// among the numbers.
		if len(match[1]) >= len("20060102") {
			return MigrationName{}, fmt.Errorf("%w: %s. A date is followed by T and the time, such as "+
				"20261027T093015_note_tags.sql", ErrMigrationName, name)
		}

		number, err := strconv.Atoi(match[1])
		if err != nil || number < 1 {
			return MigrationName{}, fmt.Errorf("%w: %s. The version is a number from 1", ErrMigrationName, name)
		}

		return MigrationName{Name: name, Number: number, Step: match[2] == "go"}, nil
	}

	return MigrationName{}, fmt.Errorf("%w: %s. A new migration is named <date>T<time>_<words>.sql in UTC, "+
		"such as 20261027T093015_note_tags.sql", ErrMigrationName, name)
}
