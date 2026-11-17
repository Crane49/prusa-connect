package httpapi

// The device-state vocabulary: the states each device kind's adapter declared,
// and the moves between them (ADR-20260831-c7b rules 5, 6 and 9).
//
// It is the per-install half of ADR-20260828-xdp rule 8's split, and the first one this
// server serves. Two installs of one build answer differently here, which is
// what ADR-20260831-nbj rule 23 already accepts for the device kind. Everything else
// about the shape follows the seven vocabularies beside it.

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/prusa3d/prusa-connect/internal/kernel/devicestate"
	"github.com/prusa3d/prusa-connect/sdk/fault"
	"github.com/prusa3d/prusa-connect/sdk/vocabulary"
)

// deviceStatesOutput is the declared state set per device kind.
//
// The content version is empty. The content is what this deployment installed,
// so a version naming it would name a set the build does not carry (ADR-20260827-qvk
// rule 1). The member is served all the same, because every vocabulary read
// answers the same four members (ADR-20260826-c77 rule 18). The format version is the
// build's own and moves when the shape below changes, so a consumer can tell a
// reshaped answer from a differently populated one.
type deviceStatesOutput struct {
	Body struct {
		Vocabulary string                 `json:"vocabulary" doc:"The vocabulary this set belongs to."`
		Version    string                 `json:"version"    doc:"The content version. This set declares none."`
		Format     string                 `json:"format"     doc:"The shape of an entry."`
		Items      []deviceStateKindEntry `json:"items"`
	}
}

// deviceStateKindEntry is one connector type's declared state model for one
// device kind.
//
//nolint:lll // the huma tags are each field's whole contract and do not wrap.
type deviceStateKindEntry struct {
	Kind        string                  `json:"kind"        doc:"The device kind whose states these are."`
	Namespace   string                  `json:"namespace"   doc:"The adapter that declared them, derived by the host."`
	Connector   string                  `json:"connector"   doc:"The connector type that declared them, qualified by its namespace."`
	Connectors  []string                `json:"connectors"  doc:"The connector instances of that type this server runs. A device names its instance under driver, so a client finds the entry for a device by looking its driver up here."`
	Guarded     bool                    `json:"guarded"     doc:"Whether the adapter said which verbs each state accepts. When false, accepts is empty on every state and says nothing."`
	States      []deviceStateEntry      `json:"states"      doc:"Every state the kind reports, in the order a person reads them."`
	Transitions []deviceStateTransition `json:"transitions" doc:"Every move between two of those states."`
}

//nolint:lll // the huma tags are each field's whole contract and do not wrap.
type deviceStateEntry struct {
	State       string   `json:"state"        doc:"The value a device record carries and a client branches on."`
	DisplayName string   `json:"display_name" doc:"What to show for a state the client has no words of its own for."`
	Accepts     []string `json:"accepts"      doc:"Every verb a device in this state accepts, when guarded is true. A verb missing here is one to offer as unavailable in this state. It is a hint: the device's answer to the command decides, and a verb a device's own capability list lacks is one it cannot carry out in any state."`
	//nolint:lll // the huma tag is one field's whole contract and does not wrap.
	Deprecated *vocabulary.Deprecation `json:"deprecated,omitempty" doc:"Present only for an entry on its way out. It names the application release the deprecation shipped in and the entry to move to, when one takes over. The entry keeps being served until a later release removes it."`
}

type deviceStateTransition struct {
	From string `json:"from"           doc:"The state the device leaves."`
	To   string `json:"to"             doc:"The state the device enters."`
	Verb string `json:"verb,omitempty" doc:"The verb that causes the move. Absent when the device makes it alone."`
}

// registerDeviceStateVocabulary mounts the one vocabulary operation that takes
// an identity.
//
// The other seven answer without a credential because they publish a set that
// is the same in every deployment of a build, which is what the package comment
// in vocabularies.go reasons from. This one publishes what an operator
// installed: the device kinds this deployment runs, the adapter that declared
// each, and the words a person sees for its states. ADR-20260831-nbj rules 22 and 24
// keep an internal device class out of the published core for exactly that
// reason, and an anonymous reader of this path would have the class's name, its
// states and its display names.
//
// An identity and no grant. A caller this server resolved is a caller of the
// deployment that runs the machine, and ADR-20260827-rqc rule 7 has no read scope finer
// than an origin to hold this to.
func registerDeviceStateVocabulary(api huma.API, installed devicestate.Installed) {
	register(api, huma.Operation{
		OperationID: "get-device-state-vocabulary",
		Method:      http.MethodGet,
		Path:        vocabularyDeviceStatesPath,
		Summary:     "The states each device kind's adapter declared",
		Description: "Every state a device of each kind reports, what to show for it, the verbs it " +
			"accepts in that state, and every move between two of them with the verb that causes it. A " +
			"move a device makes on its own names no verb. There is one entry per connector type, and a " +
			"device's driver names the instance its entry lists under connectors. This answer is what " +
			"this deployment installed rather than what the build ships, " +
			"so two servers on one release answer differently and a client reads it per server. That " +
			"is why this vocabulary takes an identity and the others do not. A verb named here is not " +
			"permission to send it: the server decides that on the request itself, and this list is a " +
			"hint a client renders from.",
		Tags:   []string{"vocabularies"},
		Errors: append([]int{http.StatusUnauthorized}, vocabularyErrors...),
	}, func(ctx context.Context, _ *struct{}) (*deviceStatesOutput, error) {
		if principalFrom(ctx) == "" {
			return nil, fault.AuthTokenInvalid.New("the request carries no usable identity")
		}

		out := &deviceStatesOutput{}
		out.Body.Vocabulary = devicestate.VocabularyName
		out.Body.Version = undeclaredVersion
		out.Body.Format = devicestate.VocabularyFormat
		out.Body.Items = deviceStateEntries(installed)

		return out, nil
	})
}

// deviceStateEntries is the projection, separate from the read so a test can
// exercise it against a set it placed. A server whose mounted connectors
// declared no state set answers an empty list.
func deviceStateEntries(installed devicestate.Installed) []deviceStateKindEntry {
	declarations := installed.Declarations()

	out := make([]deviceStateKindEntry, 0, len(declarations))
	for _, declaration := range declarations {
		entry := deviceStateKindEntry{
			Kind:        declaration.Kind.String(),
			Namespace:   declaration.Namespace.String(),
			Connector:   declaration.Connector.String(),
			Connectors:  append([]string{}, declaration.Instances...),
			Guarded:     declaration.States.Guarded(),
			States:      make([]deviceStateEntry, 0, len(declaration.States.States)),
			Transitions: make([]deviceStateTransition, 0, len(declaration.States.Transitions)),
		}

		for _, state := range declaration.States.States {
			accepts := make([]string, 0, len(state.Accepts))
			for _, verb := range state.Accepts {
				accepts = append(accepts, verb.String())
			}

			entry.States = append(entry.States, deviceStateEntry{
				State:       state.Name.String(),
				DisplayName: state.DisplayName,
				Accepts:     accepts,
				Deprecated:  state.Deprecated,
			})
		}

		for _, transition := range declaration.States.Transitions {
			entry.Transitions = append(entry.Transitions, deviceStateTransition{
				From: transition.From.String(),
				To:   transition.To.String(),
				Verb: transition.Verb.String(),
			})
		}

		out = append(out, entry)
	}

	return out
}
