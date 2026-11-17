package moduledb

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// RequireTransactional returns ErrOutsideTransaction for a migration with a
// statement that cannot run inside the upgrade's transaction, or that would
// end the transaction early. Such a statement would escape the rollback of a
// failed upgrade.
func RequireTransactional(content []byte) error {
	if statement := outsideTransaction(content); statement != "" {
		return fmt.Errorf("%w: it holds %s. Every migration of an upgrade runs in one transaction, so the "+
			"upgrade is kept whole or not at all", ErrOutsideTransaction, statement)
	}

	return nil
}

// keywords are the statements that cannot run inside the upgrade's
// transaction wherever they appear.
//
//nolint:gochecknoglobals // a closed table read at run time, never written.
var keywords = []struct {
	pattern *regexp.Regexp
	name    string
}{
	{regexp.MustCompile(`(?i)\bCONCURRENTLY\b`), "CONCURRENTLY"},
	{regexp.MustCompile(`(?i)\bVACUUM\b`), "VACUUM"},
	{regexp.MustCompile(`(?i)\b(CREATE|DROP|ALTER)\s+DATABASE\b`), "a statement on a whole database"},
	{regexp.MustCompile(`(?i)\bALTER\s+SYSTEM\b`), "ALTER SYSTEM"},
	{regexp.MustCompile(`(?i)\bPRAGMA\s+(foreign_keys|journal_mode)\b`), "a PRAGMA SQLite ignores in a transaction"},
}

// transactionWords start a statement that begins, ends or prepares a
// transaction. END is SQLite's other spelling of COMMIT, and ABORT is
// Postgres's of ROLLBACK.
//
//nolint:gochecknoglobals // a closed table read at run time, never written.
var transactionWords = []string{"BEGIN", "COMMIT", "END", "ROLLBACK", "ABORT"}

// blockWords make a CREATE statement whose body has statements of its own up
// to an END: a SQLite trigger, or a Postgres function with BEGIN ATOMIC.
//
//nolint:gochecknoglobals // a closed table read at run time, never written.
var blockWords = []string{"TRIGGER", "FUNCTION", "PROCEDURE"}

// outsideTransaction is the first statement in content that cannot run in the
// upgrade's transaction, and empty for none.
func outsideTransaction(content []byte) string {
	code := codeOnly(string(content))

	for _, one := range keywords {
		if one.pattern.MatchString(code) {
			return one.name
		}
	}

	inBlock := false

	for statement := range strings.SplitSeq(code, ";") {
		words := strings.Fields(strings.ToUpper(statement))
		if len(words) == 0 {
			continue
		}

		if inBlock {
			// The body's own statements run in the transaction. The END
			// alone closes the body.
			inBlock = len(words) != 1 || words[0] != "END"

			continue
		}

		if words[0] == "CREATE" && slices.Contains(words, "BEGIN") &&
			slices.ContainsFunc(blockWords, func(word string) bool { return slices.Contains(words, word) }) {
			inBlock = true

			continue
		}

		if slices.Contains(transactionWords, words[0]) ||
			(len(words) > 1 && (words[0] == "START" || words[0] == "PREPARE") && words[1] == "TRANSACTION") {
			return "a transaction statement of its own, " + words[0]
		}
	}

	return ""
}

// codeOnly is content with its comments, string literals, quoted identifiers
// and dollar-quoted bodies taken out. A literal leaves ” behind.
func codeOnly(content string) string {
	var out strings.Builder

	for at := 0; at < len(content); {
		rest := content[at:]

		switch {
		case strings.HasPrefix(rest, "--"):
			end := strings.IndexByte(rest, '\n')
			if end < 0 {
				return out.String()
			}

			at += end
		case strings.HasPrefix(rest, "/*"):
			end := strings.Index(rest[2:], "*/")
			if end < 0 {
				return out.String()
			}

			out.WriteByte(' ')

			at += len("/*") + end + len("*/")
		case rest[0] == '\'' || rest[0] == '"':
			at += quotedLength(rest, rest[0])

			out.WriteString("''")
		case rest[0] == '$':
			if tag := dollarTag(rest); tag != "" {
				end := strings.Index(rest[len(tag):], tag)
				if end < 0 {
					return out.String()
				}

				out.WriteString("''")

				at += len(tag) + end + len(tag)

				continue
			}

			out.WriteByte(rest[0])

			at++
		default:
			out.WriteByte(rest[0])

			at++
		}
	}

	return out.String()
}

// quotedLength is the length of the literal rest starts with, quote doubled
// inside it included, or all of rest when it never closes.
func quotedLength(rest string, quote byte) int {
	for at := 1; at < len(rest); at++ {
		if rest[at] != quote {
			continue
		}

		if at+1 < len(rest) && rest[at+1] == quote {
			at++

			continue
		}

		return at + 1
	}

	return len(rest)
}

// dollarQuote is the $tag$ or $$ that opens a Postgres dollar-quoted body.
// $1 is a parameter and opens none.
//
//nolint:gochecknoglobals // a compiled pattern read at run time, never written.
var dollarQuote = regexp.MustCompile(`^\$([A-Za-z_][A-Za-z0-9_]*)?\$`)

// dollarTag is the tag that opens a dollar-quoted body at the start of rest,
// and empty when rest does not start one.
func dollarTag(rest string) string { return dollarQuote.FindString(rest) }
