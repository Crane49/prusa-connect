package httpapi

import (
	"context"
	"net/http"
	"sync"

	"github.com/danielgtaylor/huma/v2"

	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/kernel/deviceissue"
	"github.com/prusa3d/prusa-connect/internal/kernel/devicestate"
	"github.com/prusa3d/prusa-connect/internal/kernel/hostfeature"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/internal/printer/profile"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpfault"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/event"
	"github.com/prusa3d/prusa-connect/sdk/extension"
	"github.com/prusa3d/prusa-connect/sdk/fault"
	"github.com/prusa3d/prusa-connect/sdk/vocabulary"
)

// The vocabulary paths. A vocabulary is a closed set this codebase's behaviour
// depends on. Every one of them is readable from the running server: a page
// describes the release it was generated from, and an operator debugging a
// live instance needs what this build carries.
const (
	vocabulariesPath            = "/api/v1/vocabularies"
	vocabularyErrorCodesPath    = vocabulariesPath + "/error-codes"
	vocabularyExtensionsPath    = vocabulariesPath + "/extensions"
	vocabularyCapabilitiesPath  = vocabulariesPath + "/capabilities"
	vocabularyResourceKindsPath = vocabulariesPath + "/resource-kinds"
	vocabularyAuthorizationPath = vocabulariesPath + "/authorization"
	vocabularyEventTypesPath    = vocabulariesPath + "/event-types"
	vocabularyDeviceIssuesPath  = vocabulariesPath + "/device-issues"
	vocabularyDeviceStatesPath  = vocabulariesPath + "/device-states"
	vocabularyDeviceModelsPath  = vocabulariesPath + "/device-models"
)

// The three sets whose name is declared nowhere else. The other six read theirs
// from the package that owns the set, and these three have no such package to
// read it from.
const (
	errorCodesVocabulary   = "error-codes"
	extensionsVocabulary   = "extensions"
	capabilitiesVocabulary = "capabilities"
)

// undeclaredVersion is what a version or a format member carries for a set that
// declares neither: the three above, and the content version of the device
// states. A number invented here would promise a consumer something no gate
// holds, so the member is left empty instead. All four are named under
// "unversioned" in tools/vocabcompat/baseline.json, a list that may shrink and
// never grow.
//
// The member is served all the same. A client that has to tell "this set
// declares no version" from "a member I do not know about" is a client
// branching on a missing key.
const undeclaredVersion = ""

// vocabularySecurity is what a build-wide vocabulary takes, and each of them is
// public for a stated reason and not by default. They publish a closed set
// that is identical in every deployment of a given build: no principal, no
// grant, no instance, no configured value. A client branching on an error code,
// a plugin author choosing a contract, a driver author filling in a baseline and
// an operator reading a 403 all need the answer before they have credentials,
// and the same bytes are already on the public documentation site. The one thing
// they narrow is which optional features this binary was compiled with, which
// /api/v1/health already tells an unauthenticated caller by publishing the
// version.
//
// The device models are build-wide too, though not all of their bytes are on
// the documentation site. A driver composed into the build adds its own rows,
// and a driver enters a binary only when it is built, so two deployments of
// one build still answer the same.
//
// A per-install vocabulary does not take it, because none of that reasoning
// holds for a set an operator's own install filled in. The device states are
// the one such set. They take an identity in vocabularydevicestates.go, and
// perInstallVocabularies in vocabularies_test.go is the list the gate checks
// in both directions.
var vocabularySecurity = noSecurity

// vocabularyErrors is what a vocabulary operation can answer besides its body.
// It takes no parameter and reads no dependency, so the only failure it has is
// the one recoverMiddleware writes for a panic.
//
// It is declared and not left empty because huma writes OpenAPI's `default`
// response only for an operation declaring no error at all. `default` names no
// status, so a generated client gets an untyped fallback where every other
// operation here gives it a status to branch on.
//
//nolint:gochecknoglobals // the error table for this surface, read-only after init.
var vocabularyErrors = []int{http.StatusInternalServerError}

// vocabularyIndexOutput lists what this build carries.
type vocabularyIndexOutput struct {
	Body struct {
		Items []vocabularyEntry `json:"items"`
	}
}

// vocabularyEntry is one row of the index. Version is empty for a vocabulary
// that carries none of its own. It is not filled in from a second declaration
// here.
type vocabularyEntry struct {
	Name      string `json:"name"              doc:"The vocabulary, as its path segment spells it."`
	Version   string `json:"version,omitempty" doc:"The content version. Absent when the set carries none yet."`
	Format    string `json:"format,omitempty"  doc:"The shape of an entry, so a reshape is not read as a rewording."`
	Path      string `json:"path"              doc:"Where the entries are served."`
	Reference string `json:"reference"         doc:"The published page generated from the same accessor."`
	Summary   string `json:"summary"           doc:"What the set decides."`
}

// errorCodesOutput is the error taxonomy this build can return.
//
// The version and the format are empty, because sdk/fault declares neither.
// See undeclaredVersion above for why they are served anyway.
type errorCodesOutput struct {
	Body struct {
		Vocabulary string           `json:"vocabulary" doc:"The vocabulary this set belongs to."`
		Version    string           `json:"version"    doc:"The content version. This set declares none."`
		Format     string           `json:"format"     doc:"The shape of an entry. This set declares none."`
		Items      []errorCodeEntry `json:"items"`
	}
}

type errorCodeEntry struct {
	Code      string   `json:"code"      doc:"The stable identifier a client branches on."`
	Status    int      `json:"status"    doc:"The HTTP status this code is reported with."`
	Summary   string   `json:"summary"   doc:"What went wrong."`
	Action    string   `json:"action"    doc:"What the caller does about it."`
	Retryable bool     `json:"retryable" doc:"Whether repeating the call with backoff can succeed."`
	Redacted  bool     `json:"redacted"  doc:"Whether the occurrence detail is kept out of the response."`
	Params    []string `json:"params"    doc:"The parameters the message interpolates."`
	//nolint:lll // the huma tag is one field's whole contract and does not wrap.
	Deprecated *vocabulary.Deprecation `json:"deprecated,omitempty" doc:"Present only for an entry on its way out. It names the application release the deprecation shipped in and the entry to move to, when one takes over. The entry keeps being served until a later release removes it."`
}

// extensionsOutput is the extension catalogue this build linked.
//
// The version and the format are empty, because sdk/extension declares neither
// for the catalogue. The version each entry carries is its own and not the
// set's.
type extensionsOutput struct {
	Body struct {
		Vocabulary string           `json:"vocabulary" doc:"The vocabulary this set belongs to."`
		Version    string           `json:"version"    doc:"The content version. This set declares none."`
		Format     string           `json:"format"     doc:"The shape of an entry. This set declares none."`
		Items      []extensionEntry `json:"items"`
	}
}

type extensionEntry struct {
	Kind        string `json:"kind"        doc:"The extension point this entry plugs into."`
	Name        string `json:"name"        doc:"The name an operator writes in configuration."`
	Qualified   string `json:"qualified"   doc:"The namespaced name, which separates two entries sharing one."`
	Module      string `json:"module"      doc:"The Go module the entry was declared in."`
	Summary     string `json:"summary"     doc:"What the entry does."`
	Description string `json:"description" doc:"The longer description from the entry's descriptor."`
	Stability   string `json:"stability"   doc:"How settled the entry's configuration surface is."`
	Version     string `json:"version"     doc:"The entry's own version."`
	// Refused is what the host did with the registration and not only that it
	// was made. An entry that carries one registered and is not in force: a
	// device kind with a refusal is in no verb table, and none of its commands
	// will dispatch.
	Refused string `json:"refused,omitempty" doc:"Why the host would not admit this registration."`
	// The config keys this entry reads, so a settings page builds its form from
	// the server it talks to. A key not in this list refuses the instance.
	Options []extensionOption `json:"options" doc:"The config keys this entry reads."`
	//nolint:lll // the huma tag is one field's whole contract and does not wrap.
	Deprecated *vocabulary.Deprecation `json:"deprecated,omitempty" doc:"Present only for an entry on its way out. It names the application release the deprecation shipped in and the entry to move to, when one takes over. The entry keeps being served until a later release removes it."`
}

// extensionOption is one config key an entry reads, as a settings form needs it.
//
// A form masks a key whose secret is true, a read of an instance answers a
// marker in its place, and that marker written back keeps the stored value.
// Such a key carries no default here.
type extensionOption struct {
	Key      string `json:"key"               doc:"The key as an operator writes it, in lower_snake_case."`
	Type     string `json:"type"              doc:"The shape of the value."`
	Required bool   `json:"required"          doc:"Whether the instance refuses to start without it."`
	Secret   bool   `json:"secret"            doc:"Whether what goes in this key is a credential."`
	Default  string `json:"default,omitempty" doc:"What a missing optional key means, as an operator would write it."`
	Summary  string `json:"summary"           doc:"One line saying what the key does."`
}

// capabilitiesOutput is the capability vocabulary a driver author declares
// against.
type capabilitiesOutput struct {
	Body capabilitiesBody
}

// capabilitiesBody carries no items, because it carries three lists and each
// one is named for what it holds.
//
// The version and the format are empty, because sdk/connectorapi declares
// neither for the key set. The contract version beside them is a different
// number: it says which connector contract this build serves, not which set of
// keys.
type capabilitiesBody struct {
	Vocabulary string           `json:"vocabulary" doc:"The vocabulary this set belongs to."`
	Version    string           `json:"version"    doc:"The content version. This set declares none."`
	Format     string           `json:"format"     doc:"The shape of an entry. This set declares none."`
	Contract   string           `json:"contract"   doc:"The connector contract version this build serves."`
	Kinds      []capabilityKind `json:"kinds"      doc:"The value kinds an argument may carry."`
	Keys       []capabilityKey  `json:"keys"       doc:"Every key the shipped profiles declare, at its loosest form."`
	// What this build promises about itself, which is a different question
	// from what a printer can do. A module names one of these keys under
	// requires, and a build that carries none publishes an empty list.
	Host []hostFeature `json:"host_features" doc:"The features this build promises a module. A module names one of these keys under requires in its manifest, and a host that does not carry it refuses the module. The published set is every key that exists; this is the subset this binary carries."`
}

// hostFeature is one key the host declares about itself.
type hostFeature struct {
	Key     string `json:"key"     doc:"The capability key a manifest writes."`
	Summary string `json:"summary" doc:"What the host promises under this key."`
}

type capabilityKind struct {
	Kind    string `json:"kind"    doc:"The kind, as it is written in a descriptor."`
	Numeric bool   `json:"numeric" doc:"Whether the kind carries a number."`
}

type capabilityKey struct {
	Key      string `json:"key"                doc:"The capability key."`
	Verb     string `json:"verb,omitempty"     doc:"The verb this key is an argument of, when it is one."`
	Argument string `json:"argument,omitempty" doc:"The argument name, when this key is an argument."`
	Kind     string `json:"kind"               doc:"The value kind."`
	Required bool   `json:"required"           doc:"Whether a caller must send it."`
	//nolint:lll // the huma tag is one field's whole contract and does not wrap.
	Deprecated *vocabulary.Deprecation `json:"deprecated,omitempty" doc:"Present only for an entry on its way out. It names the application release the deprecation shipped in and the entry to move to, when one takes over. The entry keeps being served until a later release removes it."`
}

// resourceKindsOutput is the closed set of resource kinds.
type resourceKindsOutput struct {
	Body struct {
		Vocabulary string                 `json:"vocabulary" doc:"The vocabulary this set belongs to."`
		Version    string                 `json:"version"    doc:"The content version of the set."`
		Format     string                 `json:"format"     doc:"The shape of an entry."`
		Items      []resourceKindEntry    `json:"items"`
		Runtimes   []resourceRuntimeEntry `json:"runtimes"   doc:"The set an instance's runtime key is drawn from."`
	}
}

type resourceKindEntry struct {
	Kind         string   `json:"kind"         doc:"The kind, as it is written in a ref, a path and a metric label."`
	Summary      string   `json:"summary"      doc:"What instances of the kind are."`
	Description  string   `json:"description"  doc:"The longer description."`
	Configurable bool     `json:"configurable" doc:"Whether instances are declared in configuration."`
	Runtimes     []string `json:"runtimes"     doc:"What an instance of this kind may declare under runtime."`
	//nolint:lll // the huma tag is one field's whole contract and does not wrap.
	Deprecated *vocabulary.Deprecation `json:"deprecated,omitempty" doc:"Present only for an entry on its way out. It names the application release the deprecation shipped in and the entry to move to, when one takes over. The entry keeps being served until a later release removes it."`
}

// resourceRuntimeEntry is one runtime an instance may declare. It rides with
// the kinds because the two are read together: a kind's entry names the
// runtimes it offers, and this says what each of them is.
type resourceRuntimeEntry struct {
	Runtime     string `json:"runtime"     doc:"The runtime, as it is written under the runtime key."`
	Summary     string `json:"summary"     doc:"What is behind an instance that declares it."`
	Description string `json:"description" doc:"The longer description."`
}

// eventTypesOutput is the closed set of core event types.
type eventTypesOutput struct {
	Body struct {
		Vocabulary string           `json:"vocabulary" doc:"The vocabulary this set belongs to."`
		Version    string           `json:"version"    doc:"The content version of the set."`
		Format     string           `json:"format"     doc:"The shape of an entry."`
		Namespace  string           `json:"namespace"  doc:"The prefix this set closes. An unlisted type is refused."`
		Items      []eventTypeEntry `json:"items"`
	}
}

type eventTypeEntry struct {
	Type        string `json:"type"        doc:"The value an envelope carries and a consumer routes on."`
	Class       string `json:"class"       doc:"The queue, propagation and expiry policy for this type."`
	Durable     bool   `json:"durable"     doc:"Whether the class is replayed on resume rather than conflated."`
	Summary     string `json:"summary"     doc:"What happened."`
	Description string `json:"description" doc:"The longer description."`
	//nolint:lll // the huma tag is one field's whole contract and does not wrap.
	Deprecated *vocabulary.Deprecation `json:"deprecated,omitempty" doc:"Present only for an entry on its way out. It names the application release the deprecation shipped in and the entry to move to, when one takes over. The entry keeps being served until a later release removes it."`
}

// capabilityCache holds the shipped profile data the capability vocabulary is
// read from. It is loaded on the first request that asks for it and not at
// start-up: the parse costs heap and resident memory, and most deployments
// never read this endpoint.
//
// It is created per server and not held in a package variable, so two servers
// in one test binary do not share it.
type capabilityCache struct {
	once sync.Once
	body capabilitiesBody
	err  error
}

func (c *capabilityCache) load() (capabilitiesBody, error) {
	c.once.Do(func() {
		registry, err := profile.Builtin()
		if err != nil {
			c.err = err

			return
		}

		c.body = capabilityBodyOf(registry.Coverage())
	})

	return c.body, c.err
}

// registerVocabularies mounts the index and one operation per vocabulary. Each
// reads the same accessor the generated page reads, so the page and the
// endpoint cannot differ.
func registerVocabularies(api huma.API, states devicestate.Installed, models profile.Catalogue) {
	capabilities := &capabilityCache{}

	registerVocabularyIndex(api)
	registerErrorCodeVocabulary(api)
	registerExtensionVocabulary(api)
	registerCapabilityVocabulary(api, capabilities)
	registerResourceKindVocabulary(api)
	registerAuthorizationVocabulary(api)
	registerEventTypeVocabulary(api)
	registerDeviceIssueVocabulary(api)
	registerDeviceStateVocabulary(api, states)
	registerDeviceModelVocabulary(api, models)
	registerDeviceModelArtwork(api, models)
}

func registerVocabularyIndex(api huma.API) {
	register(api, huma.Operation{
		OperationID: "list-vocabularies",
		Method:      http.MethodGet,
		Path:        vocabulariesPath,
		Summary:     "The closed sets this build carries",
		Description: "Lists every vocabulary this binary serves: the closed sets its behaviour depends " +
			"on, each with the version of the set and where to read it. A published page describes the " +
			"release it was generated from, and this describes the process that is running." +
			paragraphBreak +
			"This is the index over those sets and is not a vocabulary itself, so it answers `items` " +
			"alone. The `vocabulary`, `version` and `format` members belong to each set, and a row " +
			"here carries the version and the format of the set it points at.",
		Tags:     []string{"vocabularies"},
		Security: vocabularySecurity,
		Errors:   vocabularyErrors,
	}, func(_ context.Context, _ *struct{}) (*vocabularyIndexOutput, error) {
		out := &vocabularyIndexOutput{}
		out.Body.Items = vocabularyIndex()

		return out, nil
	})
}

func registerErrorCodeVocabulary(api huma.API) {
	register(api, huma.Operation{
		OperationID: "get-error-code-vocabulary",
		Method:      http.MethodGet,
		Path:        vocabularyErrorCodesPath,
		Summary:     "Every error code this build can return",
		Description: "The taxonomy a client branches on, read from the registry the responses are built " +
			"from. A code this endpoint does not list cannot leave this process, and the published " +
			"reference is generated from the same registry.",
		Tags:     []string{"vocabularies"},
		Security: vocabularySecurity,
		Errors:   vocabularyErrors,
	}, func(_ context.Context, _ *struct{}) (*errorCodesOutput, error) {
		out := &errorCodesOutput{}
		out.Body.Vocabulary = errorCodesVocabulary
		out.Body.Version = undeclaredVersion
		out.Body.Format = undeclaredVersion
		out.Body.Items = errorCodeEntries()

		return out, nil
	})
}

func registerExtensionVocabulary(api huma.API) {
	register(api, huma.Operation{
		OperationID: "get-extension-vocabulary",
		Method:      http.MethodGet,
		Path:        vocabularyExtensionsPath,
		Summary:     "Every extension this build linked",
		Description: "The catalogue an operator's configuration names, read from the registries the " +
			"process resolves against. An entry that is not here cannot be configured in this build, " +
			"whatever the documentation of another release says.",
		Tags:     []string{"vocabularies"},
		Security: vocabularySecurity,
		Errors:   vocabularyErrors,
	}, func(_ context.Context, _ *struct{}) (*extensionsOutput, error) {
		out := &extensionsOutput{}
		out.Body.Vocabulary = extensionsVocabulary
		out.Body.Version = undeclaredVersion
		out.Body.Format = undeclaredVersion
		out.Body.Items = extensionEntries()

		return out, nil
	})
}

func registerCapabilityVocabulary(api huma.API, cache *capabilityCache) {
	register(api, huma.Operation{
		OperationID: "get-capability-vocabulary",
		Method:      http.MethodGet,
		Path:        vocabularyCapabilitiesPath,
		Summary:     "The capability keys a driver declares against",
		Description: "The connector contract version, the value kinds an argument may carry, and every " +
			"capability key the shipped printer profiles declare. What one printer resolved to is a " +
			"different question, answered per printer.",
		Tags:     []string{"vocabularies"},
		Security: vocabularySecurity,
		Errors:   vocabularyErrors,
	}, func(_ context.Context, _ *struct{}) (*capabilitiesOutput, error) {
		body, err := cache.load()
		if err != nil {
			return nil, httpfault.Unexpected("the built-in capability profiles could not be read").Wrap(err)
		}

		return &capabilitiesOutput{Body: body}, nil
	})
}

func registerResourceKindVocabulary(api huma.API) {
	register(api, huma.Operation{
		OperationID: "get-resource-kind-vocabulary",
		Method:      http.MethodGet,
		Path:        vocabularyResourceKindsPath,
		Summary:     "The closed set of resource kinds",
		Description: "Every kind an instance can be named with, which half of the set an operator " +
			"declares in configuration, and what an instance of each kind may declare under runtime. " +
			"The other half names something the system already knows, so no operator declares one.",
		Tags:     []string{"vocabularies"},
		Security: vocabularySecurity,
		Errors:   vocabularyErrors,
	}, func(_ context.Context, _ *struct{}) (*resourceKindsOutput, error) {
		out := &resourceKindsOutput{}
		out.Body.Vocabulary = ref.VocabularyName
		out.Body.Version = ref.VocabularyVersion
		out.Body.Format = ref.VocabularyFormat
		out.Body.Items = resourceKindEntries()
		out.Body.Runtimes = resourceRuntimeEntries()

		return out, nil
	})
}

func registerEventTypeVocabulary(api huma.API) {
	register(api, huma.Operation{
		OperationID: "get-event-type-vocabulary",
		Method:      http.MethodGet,
		Path:        vocabularyEventTypesPath,
		Summary:     "The core event types this build publishes",
		Description: "What a consumer routes on, and the class that decides how each type is queued, " +
			"propagated and expired. The namespace is closed: a publication naming a type under it " +
			"that is not here is refused before it takes a sequence number. A plugin's or a peer's " +
			"types live under their own prefix and are not listed.",
		Tags:     []string{"vocabularies"},
		Security: vocabularySecurity,
		Errors:   vocabularyErrors,
	}, func(_ context.Context, _ *struct{}) (*eventTypesOutput, error) {
		out := &eventTypesOutput{}
		out.Body.Vocabulary = event.VocabularyName
		out.Body.Version = event.VocabularyVersion
		out.Body.Format = event.VocabularyFormat
		out.Body.Namespace = event.CoreNamespace
		out.Body.Items = eventTypeEntries()

		return out, nil
	})
}

// vocabularyIndex is the row per vocabulary. It is one written list and not a
// count derived from the handlers, so a vocabulary added without a row here is
// caught by TestEveryVocabularyRowIsServed.
func vocabularyIndex() []vocabularyEntry {
	return []vocabularyEntry{
		{
			Name:      errorCodesVocabulary,
			Path:      vocabularyErrorCodesPath,
			Reference: "/docs/error-codes/",
			Summary:   "What a failure is called, what the caller does about it, and whether a retry can succeed.",
		},
		{
			Name:      extensionsVocabulary,
			Path:      vocabularyExtensionsPath,
			Reference: "/docs/extensions/",
			Summary:   "The implementations an operator's configuration may name in this build.",
		},
		{
			Name:      capabilitiesVocabulary,
			Path:      vocabularyCapabilitiesPath,
			Reference: "/docs/capabilities/",
			Summary:   "The capability keys a driver declares and the arguments each verb takes.",
		},
		{
			Name:      ref.VocabularyName,
			Version:   ref.VocabularyVersion,
			Format:    ref.VocabularyFormat,
			Path:      vocabularyResourceKindsPath,
			Reference: "/docs/resource-kinds/",
			Summary: "The closed set of kinds every instance in the system is named with, and the " +
				"runtimes each of them offers.",
		},
		{
			Name:      authz.VocabularyName,
			Version:   authz.VocabularyVersion,
			Format:    authz.VocabularyFormat,
			Path:      vocabularyAuthorizationPath,
			Reference: "/docs/authorization-vocabulary/",
			Summary:   "The actions, types, relations and permitted tuple shapes the graph authorizes with.",
		},
		{
			Name:      event.VocabularyName,
			Version:   event.VocabularyVersion,
			Format:    event.VocabularyFormat,
			Path:      vocabularyEventTypesPath,
			Reference: "/docs/event-types/",
			Summary:   "The core event types this build publishes, and the class each one is carried under.",
		},
		{
			Name:      deviceissue.VocabularyName,
			Version:   deviceissue.VocabularyVersion,
			Format:    deviceissue.VocabularyFormat,
			Path:      vocabularyDeviceIssuesPath,
			Reference: "/docs/device-issues/",
			Summary:   "The reasons a device needs attention, and what a person does about each one.",
		},
		{
			Name:   devicestate.VocabularyName,
			Format: devicestate.VocabularyFormat,
			Path:   vocabularyDeviceStatesPath,
			// The contract page, because no page is generated from this
			// accessor and none can be: the entries are per-install and every
			// generator reads a compiled table. The contract page carries the
			// shape a declaration takes, which is what a reader of this row
			// is looking for. This set has no page of its own.
			Reference: "/docs/connector-contract/",
			Summary: "The states each device kind's adapter declared, and the moves between them. The " +
				"entries are this deployment's and not the build's.",
		},
		{
			Name:      profile.VocabularyName,
			Version:   profile.VocabularyVersion,
			Format:    profile.VocabularyFormat,
			Path:      vocabularyDeviceModelsPath,
			Reference: "/docs/device-models/",
			Summary: "The Prusa models a capability profile covers and the models a driver in this " +
				"build declared, each with the name it is sold under and a key to map to an icon.",
		},
	}
}

func eventTypeEntries() []eventTypeEntry {
	docs := event.TypeDocs()

	out := make([]eventTypeEntry, 0, len(docs))
	for _, doc := range docs {
		out = append(out, eventTypeEntry{
			Type:        doc.Type,
			Class:       doc.Class.String(),
			Durable:     doc.Class.Durable(),
			Summary:     doc.Summary,
			Description: doc.Description,
			Deprecated:  doc.Deprecated,
		})
	}

	return out
}

func errorCodeEntries() []errorCodeEntry {
	registered := fault.Global().All()

	out := make([]errorCodeEntry, 0, len(registered))
	for _, entry := range registered {
		out = append(out, errorCodeEntry{
			Code:       entry.Code.String(),
			Status:     entry.Status,
			Summary:    entry.Summary,
			Action:     entry.Action,
			Retryable:  entry.Retry == fault.RetryAllowed,
			Redacted:   entry.Redaction == fault.RedactDetail,
			Params:     entry.Params,
			Deprecated: entry.Deprecated,
		})
	}

	return out
}

func extensionEntries() []extensionEntry {
	return extensionEntriesOf(extension.Global().All(), extension.Global().Refusal)
}

// extensionEntriesOf is the projection, separate from the read so a test can
// exercise it against a descriptor it built. The httpapi test binary links no
// adapter, so the global catalogue is empty there and a comparison against it
// alone would pass on two empty lists.
// refusal is the second half: why the host would not admit an entry, and the
// empty string for one it admitted. It is a parameter and not a global read so
// that the projection stays pure and a test can drive both arms.
func extensionEntriesOf(
	registered []extension.Descriptor, refusal func(extension.Descriptor) string,
) []extensionEntry {
	out := make([]extensionEntry, 0, len(registered))
	for _, entry := range registered {
		doc := entry.Doc()

		out = append(out, extensionEntry{
			Kind:        entry.Kind().String(),
			Name:        entry.Name(),
			Qualified:   entry.Qualified(),
			Module:      entry.Module(),
			Summary:     doc.Summary,
			Description: doc.Description,
			Stability:   doc.Stability.String(),
			Version:     doc.Version.String(),
			// What the host did with the registration, and not only that it was
			// made. Without this the catalogue and the verb table are two
			// inventories that disagree, and an operator whose device kind was
			// refused reads a listing that still advertises it.
			Refused:    refusal(entry),
			Options:    extensionOptionsOf(doc.Options),
			Deprecated: doc.Deprecated,
		})
	}

	return out
}

// extensionOptionsOf renders the config keys one entry reads.
//
// A credential's default is dropped. An author who ships one has published a
// password in their source, and serving it here would put it in the answer a
// settings page reads and in every cache in front of it.
func extensionOptionsOf(options []extension.Option) []extensionOption {
	out := make([]extensionOption, 0, len(options))

	for _, option := range options {
		rendered := extensionOption{
			Key: option.Key, Type: option.Type.String(), Required: option.Required,
			Secret: option.Type == extension.OptionSecret, Summary: option.Summary,
		}

		if !rendered.Secret {
			rendered.Default = option.Default
		}

		out = append(out, rendered)
	}

	return out
}

func capabilityBodyOf(coverage connectorapi.Set) capabilitiesBody {
	body := capabilitiesBody{
		Vocabulary: capabilitiesVocabulary,
		Version:    undeclaredVersion,
		Format:     undeclaredVersion,
		Contract:   connectorapi.Contract.String(),
		Host:       []hostFeature{},
	}

	// The host's own keys, and only the ones this build carries. The published
	// page names every key that exists and says which builds carry it; this
	// answers the same question about the process that is running.
	for _, feature := range hostfeature.Advertised() {
		body.Host = append(body.Host, hostFeature{Key: feature.Key.String(), Summary: feature.Summary})
	}

	for kind := connectorapi.KindFlag; kind.Valid(); kind++ {
		body.Kinds = append(body.Kinds, capabilityKind{Kind: kind.String(), Numeric: kind.IsNumeric()})
	}

	for _, key := range coverage.Keys() {
		if !coverage.Has(key) {
			continue
		}

		value := coverage[key]
		entry := capabilityKey{
			Key: key.String(), Kind: value.Kind.String(),
			Required: value.Required, Deprecated: value.Deprecated,
		}

		if name, isArgument := key.Argument(); isArgument {
			entry.Argument = name
			entry.Verb = key.Verb().String()
		}

		body.Keys = append(body.Keys, entry)
	}

	return body
}

func resourceKindEntries() []resourceKindEntry {
	docs := ref.KindDocs()

	out := make([]resourceKindEntry, 0, len(docs))
	for _, doc := range docs {
		out = append(out, resourceKindEntry{
			Kind:         doc.Kind.String(),
			Summary:      doc.Summary,
			Description:  doc.Description,
			Configurable: doc.Configurable,
			Runtimes:     runtimeNames(doc.Runtimes),
			Deprecated:   doc.Deprecated,
		})
	}

	return out
}

// resourceRuntimeEntries is the runtime half of the resource-kind vocabulary,
// read from the same accessor the published page is generated from.
func resourceRuntimeEntries() []resourceRuntimeEntry {
	docs := ref.RuntimeDocs()

	out := make([]resourceRuntimeEntry, 0, len(docs))
	for _, doc := range docs {
		out = append(out, resourceRuntimeEntry{
			Runtime:     doc.Runtime.String(),
			Summary:     doc.Summary,
			Description: doc.Description,
		})
	}

	return out
}

// runtimeNames renders what a kind offers, in declaration order.
func runtimeNames(offered []ref.Runtime) []string {
	out := make([]string, 0, len(offered))
	for _, runtime := range offered {
		out = append(out, runtime.String())
	}

	return out
}
