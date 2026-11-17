package sqlstore

import (
	"strconv"
	"strings"
)

// digitsPerPlaceholder is how much room Renumber reserves for one rewritten
// placeholder beyond the '?' it replaces. Three digits cover the widest
// statement these stores build, so a multi-row insert stays in one allocation.
const digitsPerPlaceholder = 3

// Renumber rewrites a statement's '?' placeholders as PostgreSQL numbers them,
// so one statement text serves both engines.
//
// Every statement the stores here write uses '?' and holds no literal '?', so
// the rewrite is mechanical and touches nothing else. A store on SQLite passes
// the text through and never calls this.
//
// It is in kernel because the stores that need it may not import each other:
// internal/authz is a domain, internal/platform/history is an outbound adapter
// and internal/extension/state is the extension host.
func Renumber(query string) string {
	count := strings.Count(query, "?")
	if count == 0 {
		return query
	}

	var out strings.Builder

	out.Grow(len(query) + count*digitsPerPlaceholder)

	index := 0

	for i := range len(query) {
		if query[i] != '?' {
			out.WriteByte(query[i])

			continue
		}

		index++

		out.WriteByte('$')
		out.WriteString(strconv.Itoa(index))
	}

	return out.String()
}
