package ref

import "slices"

// Runtime is how an instance's type is reached. It is a closed, core-owned set
// like Kind, and for the same reason: an operator writes one of these values in
// the configuration file, so a value nothing declares has to be refused at
// load, before the first launch.
//
// Runtime is a different question from whether an instance runs. Enabled says
// whether it runs; this says what is behind it.
type Runtime string

const (
	// RuntimeBuiltin is a type compiled into this binary and resolved through
	// the registry. It is the default, because it is what most types are.
	RuntimeBuiltin Runtime = "builtin"
	// RuntimeService is a type reached as a supervised child process over the
	// socket. Its binary is not in this artefact.
	RuntimeService Runtime = "service"
	// RuntimeScript is an expression the operator wrote in the configuration
	// file, compiled by the host and run over every event.
	RuntimeScript Runtime = "script"
	// RuntimeAsset is a directory of files the host verifies against a
	// recorded digest and serves, and never runs. Only the asset kind offers
	// it.
	RuntimeAsset Runtime = "asset"
)

// String returns the runtime as it is written in configuration.
func (r Runtime) String() string { return string(r) }

// Valid reports whether r is a member of the closed set.
func (r Runtime) Valid() bool {
	switch r {
	case RuntimeBuiltin, RuntimeService, RuntimeScript, RuntimeAsset:
		return true
	}

	return false
}

// Runtimes returns the closed set, in declaration order.
func Runtimes() []Runtime {
	return []Runtime{RuntimeBuiltin, RuntimeService, RuntimeScript, RuntimeAsset}
}

// runtimesOfKind is what each kind may offer. It is a table so a refusal can
// list what the kind does offer.
//
// It lives here and not beside the loader that enforces it, because the
// published vocabulary of resource kinds is generated from this package and a
// second table would drift from this one.
//
// # What RuntimeService means in every row it appears in
//
// It means the instance is a module: a child process the host launches,
// verifies and supervises, reachable at an HTTP surface of its own, with a
// record store and a way to publish events.
//
// Whether the kind's own contract crosses the wire is a separate question, and
// the answer differs per kind. A link publishes one: sdk/linkapi numbers
// link.status, link.approve and link.revoke, so the host asks a link module the
// questions it asks a builtin link. Storage, auth, driver and camera publish
// none, so the kind on such an instance says which door of the core it was
// declared at and what an operator may grant on it, and not what the host may
// ask it for.
//
// A host that reads a supervised target back as a contract therefore holds the
// target to that contract and not to the kind. A `runtime: service` sink is a
// module that subscribes to the event API itself, so the sink feed in
// internal/app keeps it off the bus (moduleSink), and supervised.AttachSink
// refuses any other sink target that is not an event processor.
//
//nolint:gochecknoglobals,exhaustive // the configurable kinds only; an intrinsic kind declares no instance.
var runtimesOfKind = map[Kind][]Runtime{
	KindStorage: {RuntimeBuiltin, RuntimeService},
	KindAuth:    {RuntimeBuiltin, RuntimeService},
	// A sink is the one kind a script may serve. A script maps one event onto
	// messages and performs no effect, which is what a sink already is; every
	// other kind holds a connection, answers a request or keeps state, and a
	// language with no assignment expresses none of those.
	KindSink:   {RuntimeBuiltin, RuntimeService, RuntimeScript},
	KindDriver: {RuntimeBuiltin, RuntimeService},
	// Neither database contract crosses the wire. sdk/stateapi and
	// sdk/historyapi are published contracts, but sdk/wirerpc speaks only
	// plugin.info and plugin.shutdown. A `runtime: service` instance of either
	// kind would build a target that satisfies nothing and would store zero
	// rows without reporting an error. Both kinds stay builtin until each
	// contract has a wire method set.
	KindDB:      {RuntimeBuiltin},
	KindHistory: {RuntimeBuiltin},
	// A link speaks a network protocol to the sites above and below this one,
	// and sdk/linkapi numbers the calls the host makes on it. So a link runs
	// inside this binary or in a process of its own, and the links API reads
	// either the same way.
	KindLink: {RuntimeBuiltin, RuntimeService},
	// An asset is served and never run, so no runtime that executes anything
	// fits it, and the one runtime that serves fits nothing else.
	KindAsset: {RuntimeAsset},
}

// KindRuntimes returns the runtimes a kind may offer, in declaration order. A
// kind that declares no instance offers none.
func KindRuntimes(k Kind) []Runtime { return slices.Clone(runtimesOfKind[k]) }

// RuntimeDoc is one runtime with the prose a published reference carries.
type RuntimeDoc struct {
	Runtime     Runtime `json:"runtime"`
	Summary     string  `json:"summary"`
	Description string  `json:"description"`
	Version     string  `json:"version"`
}

// runtimeDocs is the descriptor table, a static composite literal for the same
// reason kindDocs is one.
//
// The order is Runtimes(), and TestTheRuntimeTableIsInRuntimesOrder holds it
// there so the published artefact is stable across builds.
//
//nolint:gochecknoglobals // a descriptor table, never written to.
var runtimeDocs = []RuntimeDoc{
	{
		Runtime: RuntimeBuiltin,
		Summary: "A type compiled into this binary.",
		Description: "The implementation ships in this artefact and is resolved through the type " +
			"registry by the name written under type. It is the default, and it is the only runtime " +
			"an instance created through the resource API can have.",
		Version: VocabularyVersion,
	},
	{
		Runtime: RuntimeService,
		Summary: "A type reached as a supervised child process.",
		Description: "The implementation is a separate binary the host launches and speaks to over a " +
			"socket. The instance declares where that binary is, the digest that proves it is the one " +
			"installed, and the wire major it implements, and a mismatch on any of the three is a " +
			"refusal rather than a warning.",
		Version: VocabularyVersion,
	},
	{
		Runtime: RuntimeScript,
		Summary: "An expression the operator wrote, compiled by the host.",
		Description: "There is no implementation to reach: the script is the implementation. It is " +
			"handed one event and returns the messages the host publishes, so it transforms and routes " +
			"and never performs the effect itself. Only a sink offers it, because a script has no " +
			"assignment, no loop and no way to remember anything between events.",
		Version: VocabularyVersion,
	},
	{
		Runtime: RuntimeAsset,
		Summary: "A directory of files the host serves and never runs.",
		Description: "The instance declares where the directory is and the digest recorded over every " +
			"file in it. The host checks the digest at every start and serves the files under the " +
			"module's namespace; nothing is launched, and a directory that no longer matches its " +
			"digest is refused rather than served. Only the asset kind offers it.",
		Version: VocabularyVersion,
	},
}

// RuntimeDocs returns the descriptor for every runtime, in Runtimes() order. It
// is the one accessor the published page and the API endpoint both read, so the
// two cannot answer differently.
func RuntimeDocs() []RuntimeDoc { return slices.Clone(runtimeDocs) }

// LookupRuntime returns the descriptor for r, reporting false for a runtime
// outside the closed set.
func LookupRuntime(r Runtime) (RuntimeDoc, bool) {
	for _, doc := range runtimeDocs {
		if doc.Runtime == r {
			return doc, true
		}
	}

	return RuntimeDoc{}, false
}
