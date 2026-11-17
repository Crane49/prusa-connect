package main

import (
	"fmt"
	"strings"

	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/docsref"
)

const (
	frontMatter = "---\ntitle: Authorization vocabulary\n" +
		"description: The closed set of actions, object types, relations and permitted tuple shapes " +
		"this build authorizes with, and which relation confers each action.\n" +
		"audience: extend\n" +
		"rewrite:\n  describes: current\n  implementedBy:\n    - internal/authz\n---\n\n"

	preamble = "The authorization graph is one of the four things this project does not make pluggable. " +
		"Its whole vocabulary is below: what a caller can perform, what those things are performed on, " +
		"the edges that join them, and every shape a stored edge is allowed to take.\n\n" +
		"A grant is a tuple, written `object#relation@subject`. A handler checks an **action**, never a " +
		"role, so the role layer stays changeable without touching call sites, and the table of who " +
		"confers what is the join between the two.\n\n" +
		"An action outside this set is refused rather than resolved: Go cannot stop a caller writing " +
		"`Action(\"delete_everything\")`, so the check does, and the refusal carries a code rather than " +
		"a lookup that happens to miss.\n"
)

// renderReference writes the page an operator reads before writing a grant.
func renderReference(set vocabulary) []byte {
	var page strings.Builder

	page.WriteString(frontMatter)
	page.WriteString(generatedNotice)
	page.WriteString("\n\n")
	page.WriteString(preamble)

	writeVersion(&page)
	writeActions(&page, set)
	writeTypes(&page, set)
	writeRelations(&page, set)
	writeShapes(&page, set)
	writeAPI(&page)

	return []byte(page.String())
}

func writeVersion(page *strings.Builder) {
	page.WriteString("\n## Vocabulary version\n\n")
	fmt.Fprintf(page, "This build carries `%s` version `%s`, in format `%s`. The content version moves "+
		"when an action, a type, a relation or a permitted shape changes; the format version moves "+
		"when the shape of an entry changes, so a consumer can tell a reshaped artefact from a "+
		"reworded one.\n", authz.VocabularyName, authz.VocabularyVersion, authz.VocabularyFormat)
}

// writeActions renders one anchored section per action.
func writeActions(page *strings.Builder, set vocabulary) {
	page.WriteString("\n## Actions\n\n")
	page.WriteString("Every action belongs to exactly one object type. Asking for a printer action on a " +
		"resource object is a refusal, not a lookup that misses, and the two masks share no bit.\n\n")
	page.WriteString("`conferred by` names the relations that hand the action out **when they are " +
		"written on an object of that type**. A role held further up reaches the object through a " +
		"structure edge instead: `org` and `group` confer whatever the object they name holds.\n")

	for _, entry := range set.Actions {
		fmt.Fprintf(page, "\n### `%s` {#%s}\n\n", entry.Action, docsref.AnchorUnder("action", entry.Action.String()))
		fmt.Fprintf(page, "%s.\n\n", strings.TrimSuffix(docsref.EscapeProse(entry.Summary), "."))
		page.WriteString(docsref.DeprecationNotice(entry.Deprecated))
		fmt.Fprintf(page, "- **on** `%s`\n", entry.Domain)
		fmt.Fprintf(page, "- **conferred by** %s\n\n", codeList(relationStrings(entry.ConferredBy)))
		fmt.Fprintf(page, "%s\n", docsref.EscapeProse(entry.Description))
	}
}

// writeTypes renders one anchored section per object type.
func writeTypes(page *strings.Builder, set vocabulary) {
	page.WriteString("\n## Types\n\n")
	page.WriteString("A **subject side** type may stand on the subject side of a tuple, whether as a " +
		"principal holding a grant or as the object a structure edge points at. An **object side** " +
		"type may have grants written on it. Some are both.\n")

	for _, entry := range set.Types {
		fmt.Fprintf(page, "\n### `%s` {#%s}\n\n", entry.Type, docsref.AnchorUnder("type", entry.Type.String()))
		fmt.Fprintf(page, "%s.\n\n", strings.TrimSuffix(docsref.EscapeProse(entry.Summary), "."))
		page.WriteString(docsref.DeprecationNotice(entry.Deprecated))
		fmt.Fprintf(page, "- **subject side** %s\n", docsref.YesNo(entry.Subject))
		fmt.Fprintf(page, "- **object side** %s\n\n", docsref.YesNo(entry.Object))
		fmt.Fprintf(page, "%s\n", docsref.EscapeProse(entry.Description))
	}
}

// writeRelations renders one anchored section per relation.
func writeRelations(page *strings.Builder, set vocabulary) {
	page.WriteString("\n## Relations\n\n")
	page.WriteString("A **role** relation carries a level and confers actions. A structure relation " +
		"carries whatever the object it names holds, which is how one grant reaches a fleet.\n")

	for _, entry := range set.Relations {
		fmt.Fprintf(page, "\n### `%s` {#%s}\n\n",
			entry.Relation, docsref.AnchorUnder("relation", entry.Relation.String()))
		fmt.Fprintf(page, "%s.\n\n", strings.TrimSuffix(docsref.EscapeProse(entry.Summary), "."))
		page.WriteString(docsref.DeprecationNotice(entry.Deprecated))
		fmt.Fprintf(page, "- **role** %s\n\n", docsref.YesNo(entry.Role))
		fmt.Fprintf(page, "%s\n", docsref.EscapeProse(entry.Description))
	}
}

func writeShapes(page *strings.Builder, set vocabulary) {
	page.WriteString("\n## Permitted shapes\n\n")
	page.WriteString("The grammar a tuple write is held to, and the rows the database's composite " +
		"foreign key points at. A legal name in an illegal position is the fail-open case, which is " +
		"why the constraint is composite rather than a check per column.\n\n")
	page.WriteString("An empty subject relation is a principal named directly. A subject relation makes " +
		"the subject a userset: every principal holding that relation on that object.\n\n")
	page.WriteString("| object type | relation | subject type | subject relation |\n")
	page.WriteString("| --- | --- | --- | --- |\n")

	for _, entry := range set.Shapes {
		fmt.Fprintf(page, "| `%s` | `%s` | `%s` | %s |\n",
			entry.ObjectType, entry.Relation, entry.SubjectType, docsref.OrNone(entry.SubjectRelation.String()))
	}
}

func writeAPI(page *strings.Builder) {
	page.WriteString("\n## Reading the set from a running server\n\n")
	page.WriteString("`GET /api/v1/vocabularies/authorization` answers with what **this build** carries, " +
		"from the same accessors this page is generated from. It is public: the vocabulary names no " +
		"principal, no grant and no instance, and an operator reading a refusal needs to know which " +
		"action was checked and which relation would confer it.\n")
}

func relationStrings(relations []authz.Relation) []string {
	out := make([]string, 0, len(relations))
	for _, relation := range relations {
		out = append(out, relation.String())
	}

	return out
}

func codeList(values []string) string {
	if len(values) == 0 {
		return docsref.None
	}

	quoted := make([]string, 0, len(values))
	for _, value := range values {
		quoted = append(quoted, "`"+docsref.EscapeSpan(value)+"`")
	}

	return strings.Join(quoted, ", ")
}
