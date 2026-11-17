package docsref

import "github.com/prusa3d/prusa-connect/sdk/vocabulary"

// DeprecationNotice is the paragraph a generated reference prints under a
// deprecated entry's heading. It is empty for an entry that is staying, so
// every generator writes it without asking first.
//
// The reader of a published set has to be told that an entry is going before
// the release that removes it, and the page is where a person reads that. The
// served vocabulary carries the same marker for a program.
func DeprecationNotice(marked *vocabulary.Deprecation) string {
	if !marked.Deprecated() {
		return ""
	}

	return "**" + marked.Sentence() + "**\n\n"
}
