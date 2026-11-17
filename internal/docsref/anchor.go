// Package docsref holds the rules every generated reference page is rendered
// by: the anchor a reader constructs from an identifier, and the escaping that
// stops a descriptor string becoming markup.
//
// Every generator shares the one pure anchor function here, so every page
// gives the same anchor for the same identifier. The generators keep their own
// renderers, because each renders a different shape: the rules are shared
// here and the layout is not.
package docsref

import "strings"

// None is what an empty value renders as. A blank reads as a value nobody
// filled in, and a screen reader announces nothing at all.
const None = "none"

// Anchor turns an identifier into the anchor a reader can construct from it:
// lowercase, and every run of anything else becomes one hyphen.
// `core.printer/GCODE_NOT_ALLOWED` becomes `core-printer-gcode-not-allowed`.
func Anchor(identifier string) string {
	var out strings.Builder

	out.Grow(len(identifier))
	appendSlug(&out, identifier, false)

	return out.String()
}

// AnchorUnder is Anchor for a page publishing several sets whose names may
// coincide: the kind leads the anchor, so the action `read` is `#action-read`
// and the relation of the same name is `#relation-read`.
func AnchorUnder(kind, identifier string) string {
	var out strings.Builder

	out.Grow(len(kind) + 1 + len(identifier))
	out.WriteString(kind)
	appendSlug(&out, identifier, true)

	return out.String()
}

// appendSlug writes the slug of identifier onto out. pendingHyphen says whether
// a separator is already owed. AnchorUnder sets it, so a hyphen stands between
// the kind and the anchor.
func appendSlug(out *strings.Builder, identifier string, pendingHyphen bool) {
	for _, char := range strings.ToLower(identifier) {
		switch {
		case (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9'):
			if pendingHyphen && out.Len() > 0 {
				out.WriteByte('-')
			}

			pendingHyphen = false

			out.WriteRune(char)
		default:
			pendingHyphen = true
		}
	}
}

// The two places a descriptor string is published, each with its own thing to
// neutralise. sdk/extension refuses what no escaping repairs: a control
// character, a line break, or a backtick in a value published inside a code
// span.

// EscapeProse renders free text as a paragraph or a list item. Markdown carries
// html through, so a tag an entry wrote would run on the page every client
// developer reads.
func EscapeProse(text string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;").Replace(text)
}

// EscapeSpan renders a value inside a code span, which markdown already reads
// as text. The pipe stays escaped on a page that carries no table: a descriptor
// is third-party text, and the escape guards against injection whatever the
// page renders.
func EscapeSpan(text string) string {
	return strings.ReplaceAll(text, "|", `\|`)
}

// YesNo renders a flag in a word that survives being read aloud on its own.
func YesNo(value bool) string {
	if value {
		return "yes"
	}

	return "no"
}

// OrNone renders a value inside a code span, or None where there is nothing to
// render.
func OrNone(value string) string {
	if value == "" {
		return None
	}

	return "`" + EscapeSpan(value) + "`"
}
