package httpapi

// The device-issue vocabulary: the closed set of reasons a device needs
// somebody's attention.
//
// It sits in a file of its own because vocabularies.go is at the length limit
// revive holds this package to, and not because it is a different kind of
// thing. Everything here follows the shape the other vocabularies follow.

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/prusa3d/prusa-connect/internal/kernel/deviceissue"
	"github.com/prusa3d/prusa-connect/sdk/vocabulary"
)

// deviceIssuesOutput is the closed set of reasons a device needs attention.
//
// Severities rides with the reasons because the two are read together: an entry
// names its severity, and a client ordering badges reads the order from here
// and does not hard-code the strings.
type deviceIssuesOutput struct {
	Body struct {
		Vocabulary string             `json:"vocabulary" doc:"The vocabulary this set belongs to."`
		Version    string             `json:"version"    doc:"The content version of the set."`
		Format     string             `json:"format"     doc:"The shape of an entry."`
		Items      []deviceIssueEntry `json:"items"`
		Severities []string           `json:"severities" doc:"Every severity an entry may carry, least to most urgent."`
	}
}

type deviceIssueEntry struct {
	Reason      string `json:"reason"      doc:"The token a device record carries and a client branches on."`
	Severity    string `json:"severity"    doc:"How loudly to report it. It is fixed by the reason."`
	Summary     string `json:"summary"     doc:"What is wrong, in one sentence."`
	Description string `json:"description" doc:"The longer description: what raises it, and what it does not mean."`
	Action      string `json:"action"      doc:"What a person does about it."`
	//nolint:lll // the huma tag is one field's whole contract and does not wrap.
	Deprecated *vocabulary.Deprecation `json:"deprecated,omitempty" doc:"Present only for an entry on its way out. It names the application release the deprecation shipped in and the entry to move to, when one takes over. The entry keeps being served until a later release removes it."`
}

func registerDeviceIssueVocabulary(api huma.API) {
	register(api, huma.Operation{
		OperationID: "get-device-issue-vocabulary",
		Method:      http.MethodGet,
		Path:        vocabularyDeviceIssuesPath,
		Summary:     "The closed set of reasons a device needs attention",
		Description: "Every reason a device record's `issues` member can carry: what is wrong, how " +
			"loudly to report it, and what a person does about it. A device record carries the token " +
			"and its severity and no sentence at all, so a client reads this set once, caches it, and " +
			"translates it. It is read from the same table the device plane raises issues from.",
		Tags:     []string{"vocabularies"},
		Security: vocabularySecurity,
		Errors:   vocabularyErrors,
	}, func(_ context.Context, _ *struct{}) (*deviceIssuesOutput, error) {
		out := &deviceIssuesOutput{}
		out.Body.Vocabulary = deviceissue.VocabularyName
		out.Body.Version = deviceissue.VocabularyVersion
		out.Body.Format = deviceissue.VocabularyFormat
		out.Body.Items = deviceIssueEntries()
		out.Body.Severities = deviceIssueSeverities()

		return out, nil
	})
}

func deviceIssueEntries() []deviceIssueEntry {
	docs := deviceissue.Docs()

	out := make([]deviceIssueEntry, 0, len(docs))
	for _, doc := range docs {
		out = append(out, deviceIssueEntry{
			Reason:      doc.Reason.String(),
			Severity:    doc.Severity.String(),
			Summary:     doc.Summary,
			Description: doc.Description,
			Action:      doc.Action,
			Deprecated:  doc.Deprecated,
		})
	}

	return out
}

func deviceIssueSeverities() []string {
	all := deviceissue.Severities()

	out := make([]string, 0, len(all))
	for _, severity := range all {
		out = append(out, severity.String())
	}

	return out
}
