package ref

import (
	"slices"

	"github.com/prusa3d/prusa-connect/sdk/vocabulary"
)

// What a published resource-kind reference declares about itself. The content
// version moves when a kind is added, retired or redefined; the format version
// moves when the shape of an entry changes, so a consumer can tell a reshaped
// artefact from a reworded one.
const (
	VocabularyName    = "resource-kinds"
	VocabularyVersion = "1.6.0"
	VocabularyFormat  = "1.1"
)

// KindDoc is one resource kind with the prose a published reference carries.
// The strings land in .rodata and are demand-paged, so they cost nothing
// resident until something reads them.
type KindDoc struct {
	Kind         Kind   `json:"kind"`
	Summary      string `json:"summary"`
	Description  string `json:"description"`
	Configurable bool   `json:"configurable"`
	// Runtimes is what an instance of this kind may declare under the runtime
	// key, in declaration order. It is empty for a kind that declares no
	// instance at all.
	Runtimes []Runtime `json:"runtimes"`
	Version  string    `json:"version"`
	// Deprecated marks a kind that is going away, and is nil for one that is
	// staying. It is removed only after a release published this marker for it,
	// and the compatibility gate refuses any other removal.
	Deprecated *vocabulary.Deprecation `json:"deprecated,omitempty"`
}

// kindDocs is the descriptor table. It is a static composite literal, because
// the same entries registered by init would cost heap and resident set for the
// life of the process.
//
// The order is Kinds(), and TestTheDescriptorTableIsInKindsOrder holds it there
// so the published artefact is stable across builds.
var kindDocs = []KindDoc{
	{
		Kind:    KindStorage,
		Summary: "Where uploaded files and job artefacts are kept.",
		Description: "A storage instance is a place bytes live: a directory on the host, a bucket, or a " +
			"temporary area that is emptied on restart. Uploads, job files and camera snapshots are " +
			"addressed through one, and an instance that is not ready takes only the feature that " +
			"needs it out of service.",
		Configurable: true,
		Version:      VocabularyVersion,
	},
	{
		Kind:    KindAuth,
		Summary: "An identity provider that turns a credential into a principal.",
		Description: "An auth instance runs the login ceremony and resolves the caller of a request into " +
			"the subject the authorization graph is keyed by. It decides who somebody is and never " +
			"what they may do, which is the authorization graph's answer and not this one's.",
		Configurable: true,
		Version:      VocabularyVersion,
	},
	{
		Kind:    KindSink,
		Summary: "An outbound destination events are delivered to.",
		Description: "A sink instance forwards the event stream somewhere off this process: a broker, a " +
			"queue or an HTTP endpoint. It attaches downstream of the state update, so a slow or " +
			"failing sink never delays a printer frame reaching a browser.",
		Configurable: true,
		Version:      VocabularyVersion,
	},
	{
		Kind:    KindDriver,
		Summary: "A printer connector: the code that speaks one device protocol.",
		Description: "A driver instance is a configured connector, and the capability keys it declares " +
			"are what the API offers for the printers it serves. Printers themselves are not " +
			"declared here: a device dials in and is admitted by the connector that answers it.",
		Configurable: true,
		Version:      VocabularyVersion,
	},
	{
		Kind:    KindDB,
		Summary: "A record backend: the key/value store a module's namespace is served out of.",
		Description: "A db instance is the store behind sdk/stateapi. A module is handed a namespace of " +
			"its own out of it and cannot tell one backend from another, which is why the contract " +
			"is unchanged whichever instance serves it. It holds no event history: that is the " +
			"history kind, whose contract shares no method with this one. SQLite and Postgres are " +
			"both first class, and an instance owns its own store, its own credentials and its own " +
			"migration ledger.",
		Configurable: true,
		Version:      VocabularyVersion,
	},
	{
		Kind:    KindCamera,
		Summary: "A source of still images or a video stream for a printer.",
		Description: "A camera is a device beside the printers, added through PUT /api/v1/cameras/{camera} and " +
			"kept in the server's database, so a snapshot is authorized, streamed and audited through the " +
			"same path as telemetry. No configuration file declares one.",
		Configurable: true,
		Version:      VocabularyVersion,
	},
	{
		Kind:    KindHistory,
		Summary: "A store that keeps metrics and events after the hub has forgotten them.",
		Description: "A history instance subscribes to the event bus under a principal of its own and " +
			"stores what it is granted: telemetry as samples against the state path it was reported " +
			"under, state and audit events whole. It declares what it keeps and for how long, and a " +
			"query it cannot answer truthfully is refused rather than approximated. A deployment " +
			"that declares none has no past, and every feature that needs one is absent and says so.",
		Configurable: true,
		Version:      VocabularyVersion,
	},
	{
		Kind:    KindLink,
		Summary: "This site's link to one parent site.",
		Description: "A link instance carries this site's device state up to one parent and the " +
			"parent's commands down, and nothing else: a print file travels by URL and the device " +
			"fetches it. A deployment declares at most one, because a node has one parent. A parent " +
			"commands a child as an ordinary principal, so what it may do on this site is a grant " +
			"written here and never a property of the wire.",
		Configurable: true,
		Version:      VocabularyVersion,
	},
	{
		Kind:    KindAsset,
		Summary: "A module that ships files for the browser and runs no process.",
		Description: "An asset instance is a directory of files and the digest recorded over them, " +
			"installed the way a module is and declared under the module's type. The host verifies " +
			"the digest at every start and serves the files at /api/v1/modules/<namespace>/ui/, " +
			"authorizing each request on the instance the way it authorizes a module's own routes. " +
			"Nothing is launched: a page loads the entry file as a script, and the instance is " +
			"enabled and disabled like any other.",
		Configurable: true,
		Version:      VocabularyVersion,
	},
	{
		Kind:    KindGCodePreset,
		Summary: "A named, stored gcode sequence a caller runs against a printer.",
		Description: "A preset is a maintenance macro: an ordered list of gcode commands, the models it " +
			"was written for, and a name. It is configuration rather than something that runs, so " +
			"nothing supervises one. Writing a preset takes configure_resource on this kind and " +
			"reading one takes view_resource; running one takes send_gcode on the printer and nothing " +
			"on this kind at all, so an operator may be trusted with a macro they may not rewrite.",
		Configurable: true,
		Version:      VocabularyVersion,
	},
	{
		Kind:    KindDashboard,
		Summary: "A page of cards a person lays out, drawn on the home page.",
		Description: "A dashboard is a grid of cards: printers, the fleet, temperatures, cameras, notes. " +
			"It is configuration rather than something that runs, so nothing supervises one. Each " +
			"dashboard says who sees it: reading one at the level everyone takes view_resource on this " +
			"kind, at the level operators takes operate_resource, and at the level administrators takes " +
			"configure_resource. Making, changing or deleting one takes operate_resource, and choosing " +
			"the default one takes configure_resource.",
		Configurable: true,
		Version:      VocabularyVersion,
	},
	{
		Kind:    KindPrinter,
		Summary: "One physical printer, registered by the device plane.",
		Description: "A printer is intrinsic: it is not declared in the configuration document and has no " +
			"resource API. It arrives when a connector admits a device, carries a fleet-unique " +
			"identity, and is addressable here because the event envelope, the health key and the " +
			"authorization key are one vocabulary.",
		Configurable: false,
		Version:      VocabularyVersion,
	},
	{
		Kind:    KindNode,
		Summary: "A peer site this server is linked to.",
		Description: "A node is another installation of this product, federated with this one. It is " +
			"intrinsic for the same reason a printer is: the link handshake creates it, no " +
			"configuration block declares it, and events forwarded from it keep the source the " +
			"origin wrote.",
		Configurable: false,
		Version:      VocabularyVersion,
	},
}

// KindDocs returns the descriptor for every kind, in Kinds() order. It is the
// one accessor the published page and the API endpoint both read, so the two
// cannot answer differently.
//
// Runtimes is filled in here and is not written into the table above. The
// answer already exists in runtimesOfKind, which the loader refuses against,
// and a second copy would go stale.
func KindDocs() []KindDoc {
	out := slices.Clone(kindDocs)
	for i := range out {
		out[i].Runtimes = KindRuntimes(out[i].Kind)
	}

	return out
}

// LookupKind returns the descriptor for k, reporting false for a kind outside
// the closed set.
func LookupKind(k Kind) (KindDoc, bool) {
	for _, doc := range kindDocs {
		if doc.Kind == k {
			doc.Runtimes = KindRuntimes(k)

			return doc, true
		}
	}

	return KindDoc{}, false
}
