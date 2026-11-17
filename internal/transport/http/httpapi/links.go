package httpapi

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/prusa3d/prusa-connect/internal/transport/http/httpfault"
)

const (
	linksPath          = "/api/v1/links"
	linkPath           = linksPath + "/{name}"
	approveLinkNodeURL = linkPath + "/nodes/{id}/approve"
	rejectLinkNodeURL  = linkPath + "/nodes/{id}/reject"
	revokeLinkNodeURL  = linkPath + "/nodes/{id}/revoke"
)

// LinkStore is the configured links and what each running link observes of the
// nodes around it. It is declared here because this is where it is consumed.
//
// Authorization belongs inside it, the way it belongs inside ResourceStore: a
// listing resolves view_resource on each link instance in the query that
// produces the rows, and a decision about a node takes operate_resource on the
// instance. A caller holding no grant on a link is told what a caller naming a
// link that does not exist is told.
type LinkStore interface {
	// ListLinks returns every link the caller may see.
	ListLinks(ctx context.Context, principal string) (LinkListing, error)

	// GetLink returns one link by the name the operator gave it.
	GetLink(ctx context.Context, principal, name string) (LinkInstance, error)

	// ApproveLinkNode admits a node the link is holding pending, and returns
	// the link as it stands afterwards.
	ApproveLinkNode(ctx context.Context, principal, name, node string) (LinkInstance, error)

	// RejectLinkNode removes a node the link is holding pending, and returns
	// the link as it stands afterwards.
	RejectLinkNode(ctx context.Context, principal, name, node string) (LinkInstance, error)

	// RevokeLinkNode takes admission away from a node under this link, and
	// returns the link as it stands afterwards.
	RevokeLinkNode(ctx context.Context, principal, name, node string) (LinkInstance, error)
}

// LinkListing is every link the caller may see.
type LinkListing struct {
	Items []LinkInstance `json:"items" doc:"Every link the caller may see, ordered by name."`
}

// LinkInstance is one configured link: what the operator declared, and what the
// running link knows. Everything the link alone can answer is absent while it
// is not running, because a disabled link has no key, no node id and no nodes.
type LinkInstance struct {
	Name string `json:"name" doc:"The operator-chosen name, unique within the kind."      example:"parent"`
	Kind string `json:"kind" doc:"The resource kind, which is always link."               example:"link"`
	Type string `json:"type" doc:"The registered implementation this link is built from." example:"com.github.aux.prusa3d.fractal/fractal"`

	Enabled bool `json:"enabled" doc:"Whether the supervisor starts this link."`
	Running bool `json:"running" doc:"Whether the link is serving right now. Everything below is absent while it is not."`

	Mode      string `json:"mode,omitempty"      doc:"The short reading of what this site does: parent when it accepts sites under it, child when it does not. The uplink member and the accepting member say it in full." example:"child"`
	Enrolment string `json:"enrolment,omitempty" doc:"How this site decides about a site asking to join under it: inherit to let in every site that proves its key, explicit to keep each one waiting for a person."       example:"inherit"`

	Accepting bool `json:"accepting" doc:"Whether this site takes other sites under it."`

	NodeID    string `json:"node_id,omitempty"    doc:"What this site is called in the network."                                                         example:"6f1c0a2d5b8e4711"`
	NodePath  string `json:"node_path,omitempty"  doc:"Where this site sits in the tree, from the root down to it. Absent before it joins."              example:"root.workshop.6f1c0a2d5b8e4711"`
	PublicKey string `json:"public_key,omitempty" doc:"This site's public identity key as text. The private half is on no path that reaches this value."`
	Address   string `json:"address,omitempty"    doc:"The address this link joins, or the first address it accepts on."                                 example:"nats://pcl-a:4222"`

	Uplink *LinkUplink `json:"uplink,omitempty" doc:"The site above this one, and absent on a site that names none."`

	Listeners []LinkListener `json:"listeners" doc:"Every network listener this link opened, so an operator sees where other sites reach it."`

	Nodes []LinkNode `json:"nodes" doc:"Every site below this one, ordered by id."`

	Counters *LinkCounters `json:"counters,omitempty" doc:"What this link has handled since it started. Absent for a link that counts nothing, and absent while the link is not running. The counts start at zero again every time the link starts."`
}

// LinkCounters is what one link has handled since it started.
//
// The counts are here and not on the metrics page. Every series there is
// written by this server from a table compiled into it, so a link cannot add
// one, and a count per node would spend the whole per-machine allowance of that
// page on one link.
type LinkCounters struct {
	Messages []LinkMessageCount `json:"messages" doc:"How many messages of each kind ended each way. A pair that counted nothing is left out."`
	Drops    []LinkDropCount    `json:"drops"    doc:"How many messages of each kind the link threw away for each reason. A pair that counted nothing is left out."`
}

// LinkMessageCount is how many messages of one kind ended one way.
type LinkMessageCount struct {
	Kind    string `json:"kind"    doc:"The link protocol's name for the type of message."                                           example:"RPC"`
	Outcome string `json:"outcome" doc:"The link protocol's name for what the link did with it: handled, passed on, or thrown away." example:"processed"`
	Count   uint64 `json:"count"   doc:"How many messages of that kind ended that way since the link started."                       example:"412"`
}

// LinkDropCount is how many messages of one kind the link threw away for one
// reason.
type LinkDropCount struct {
	Kind   string `json:"kind"   doc:"The link protocol's name for the type of message."                                       example:"REP"`
	Reason string `json:"reason" doc:"The link protocol's name for why the link threw the message away."                       example:"malformed"`
	Count  uint64 `json:"count"  doc:"How many messages of that kind were thrown away for that reason since the link started." example:"3"`
}

// LinkUplink is the site above this one.
type LinkUplink struct {
	Address string `json:"address,omitempty" doc:"Where this link joins." example:"nats://above.workshop.example:4222"`

	State string `json:"state" enum:"not_configured,joining,connected,lost,denied" doc:"What the connection to the site above is doing. A link that was never given a site to join reads not_configured; one that was refused reads denied and fills code and reason." example:"connected"`

	Reason string `json:"reason,omitempty" doc:"The other end's own sentence for the state, for a person to read. Absent when it gave none."`
	Code   int    `json:"code,omitempty"   doc:"The protocol's number for a refusal. Absent unless the state is denied."`

	FaultCode string `json:"fault_code,omitempty" doc:"The link's own registered error code for a refusal, written as domain/REASON. A page shows the sentence its catalogue holds for this code and falls back to the reason when it holds none. The protocol's number in code is the other end's; this one is the link's, and a link that registers none leaves it out." example:"com.prusa.fractal/JOIN_DENIED"`

	ConnectedAt *time.Time `json:"connected_at,omitempty" doc:"When the connection last opened. Absent until one has, and it stays at the last opening while the state is lost."`
}

// LinkListener is one network listener the link opened.
type LinkListener struct {
	Protocol string `json:"protocol"       doc:"What the listener speaks, as the link names it."              example:"nats"`
	Address  string `json:"address"        doc:"The interface it is bound to."                                example:"0.0.0.0"`
	Port     int    `json:"port,omitempty" doc:"The port it accepts on. Absent for a transport with no port." example:"4222"`
}

// LinkNode is one site below this one, as the link sees it.
type LinkNode struct {
	ID   string `json:"id"   doc:"The node's identity in the network."        example:"6f1c0a2d5b8e4711"`
	Role string `json:"role" doc:"What this node is to the one reporting it." example:"child"            enum:"parent,child"`

	Path string `json:"path,omitempty" doc:"Where the node sits in the tree. Absent before it joins." example:"root.workshop.6f1c0a2d5b8e4711"`

	Admission string `json:"admission" enum:"admitted,pending,revoked" doc:"Whether the node may act here. A node stays pending under explicit enrolment until an operator approves it, and a node an operator revoked stays in the list as revoked." example:"admitted"`

	PublicKey string `json:"public_key,omitempty" doc:"The node's public identity key as text."`
	Address   string `json:"address,omitempty"    doc:"Where the connection to this node goes." example:"127.0.0.1:44312"`

	Alive         bool       `json:"alive"                    doc:"Whether the connection to the node is open right now."`
	LastHeartbeat *time.Time `json:"last_heartbeat,omitempty" doc:"When something last arrived from the node. Absent until something has."`
	LatencyMS     *float64   `json:"latency_ms,omitempty"     doc:"The last round trip the link measured, in milliseconds. Absent on a protocol that measures none."`
}

// LinkRef is the path parameter one link is named by. It is exported because
// Huma binds parameters by reflection and cannot write into an unexported
// embedded field.
type LinkRef struct {
	Name string `path:"name" maxLength:"63" doc:"The operator-chosen name of the link." example:"parent"`
}

// LinkNodeRef names one node under one link.
type LinkNodeRef struct {
	LinkRef

	ID string `path:"id" maxLength:"63" doc:"The node's identity, as the link reports it." example:"6f1c0a2d5b8e4711"`
}

type listLinksOutput struct {
	Body LinkListing
}

type getLinkInput struct {
	LinkRef
}

type getLinkOutput struct {
	Body LinkInstance
}

type approveLinkNodeInput struct {
	LinkNodeRef
}

type approveLinkNodeOutput struct {
	Body LinkInstance
}

type rejectLinkNodeInput struct {
	LinkNodeRef
}

type rejectLinkNodeOutput struct {
	Body LinkInstance
}

type revokeLinkNodeInput struct {
	LinkNodeRef
}

type revokeLinkNodeOutput struct {
	Body LinkInstance
}

// linkErrors is what a caller naming one link may be answered with. A link the
// caller may not see and one that is not declared answer the same 404, so the
// listing is the only thing that says which links exist.
var linkErrors = []int{
	http.StatusNotFound,
	http.StatusUnauthorized,
	http.StatusForbidden,
}

// unknownLink is what a name no link carries is answered with, and what a link
// the caller may not see is answered with too.
func unknownLink(name string) error {
	return httpfault.ResourceNotFound(httpfault.Reference("link", name))
}

// registerLinks mounts every link operation. They are published whether or not
// a link plane is wired, because the document is a contract and every server
// of one release must publish the same paths.
func registerLinks(api huma.API, store LinkStore) {
	registerListLinks(api, store)
	registerGetLink(api, store)
	registerApproveLinkNode(api, store)
	registerRejectLinkNode(api, store)
	registerRevokeLinkNode(api, store)
}

func registerListLinks(api huma.API, store LinkStore) {
	register(api, huma.Operation{
		OperationID: "list-links",
		Method:      http.MethodGet,
		Path:        linksPath,
		Summary:     "The links this site holds",
		Description: "Every link the caller may see, which takes view_resource on each link " +
			"instance. A link carries whether it is enabled, what it does on the wire, how nodes " +
			"are admitted, this node's own identity, and every node it has met with the round " +
			"trip the link last measured to it. A deployment with no link plane answers an " +
			"empty list.",
		Tags:   []string{"resources"},
		Errors: []int{http.StatusUnauthorized, http.StatusForbidden},
	}, func(ctx context.Context, _ *struct{}) (*listLinksOutput, error) {
		out := &listLinksOutput{}
		out.Body.Items = []LinkInstance{}

		if store == nil {
			return out, nil
		}

		listing, err := store.ListLinks(ctx, principalFrom(ctx))
		if err != nil {
			return nil, err
		}

		if listing.Items != nil {
			out.Body.Items = listing.Items
		}

		return out, nil
	})
}

func registerGetLink(api huma.API, store LinkStore) {
	register(api, huma.Operation{
		OperationID: "get-link",
		Method:      http.MethodGet,
		Path:        linkPath,
		Summary:     "One link and the nodes it reaches",
		Description: "One link by the name the operator gave it, which takes view_resource on " +
			"that instance. A name no link carries, and a link the caller holds no grant on, " +
			"answer the same refusal.",
		Tags:   []string{"resources"},
		Errors: linkErrors,
	}, func(ctx context.Context, in *getLinkInput) (*getLinkOutput, error) {
		if store == nil {
			return nil, unknownLink(in.Name)
		}

		link, err := store.GetLink(ctx, principalFrom(ctx), in.Name)
		if err != nil {
			return nil, err
		}

		return &getLinkOutput{Body: link}, nil
	})
}

func registerApproveLinkNode(api huma.API, store LinkStore) {
	register(api, huma.Operation{
		OperationID: "approve-link-node",
		Method:      http.MethodPost,
		Path:        approveLinkNodeURL,
		Summary:     "Admit a node this link is holding",
		Description: "Admits a node a link under explicit enrolment is holding pending, and " +
			"answers the link as it stands afterwards. It takes operate_resource on the link " +
			"instance. Approving a node that is already admitted changes nothing and answers " +
			"the same body, so a retried request is safe.",
		Tags:   []string{"resources"},
		Errors: linkErrors,
	}, func(ctx context.Context, in *approveLinkNodeInput) (*approveLinkNodeOutput, error) {
		if store == nil {
			return nil, unknownLink(in.Name)
		}

		link, err := store.ApproveLinkNode(ctx, principalFrom(ctx), in.Name, in.ID)
		if err != nil {
			return nil, err
		}

		return &approveLinkNodeOutput{Body: link}, nil
	})
}

func registerRejectLinkNode(api huma.API, store LinkStore) {
	register(api, huma.Operation{
		OperationID: "reject-link-node",
		Method:      http.MethodPost,
		Path:        rejectLinkNodeURL,
		Summary:     "Remove a node this link is holding",
		Description: "Removes a node a link under explicit enrolment is holding pending, and " +
			"answers the link as it stands afterwards. It takes operate_resource on the link " +
			"instance, the same grant approving takes. The node leaves the list, and the name " +
			"it asked under is free for another machine to ask for. A node that has already " +
			"joined is removed with revoke instead.",
		Tags:   []string{"resources"},
		Errors: linkErrors,
	}, func(ctx context.Context, in *rejectLinkNodeInput) (*rejectLinkNodeOutput, error) {
		if store == nil {
			return nil, unknownLink(in.Name)
		}

		link, err := store.RejectLinkNode(ctx, principalFrom(ctx), in.Name, in.ID)
		if err != nil {
			return nil, err
		}

		return &rejectLinkNodeOutput{Body: link}, nil
	})
}

func registerRevokeLinkNode(api huma.API, store LinkStore) {
	register(api, huma.Operation{
		OperationID: "revoke-link-node",
		Method:      http.MethodPost,
		Path:        revokeLinkNodeURL,
		Summary:     "Take admission away from a node under this link",
		Description: "Takes admission away from a node under this link, and answers the link as " +
			"it stands afterwards. It takes operate_resource on the link instance, the same " +
			"grant approving takes. The node stays in the list as revoked, so an operator sees " +
			"what was refused. Revoking a node that is already revoked changes nothing and " +
			"answers the same body, so a retried request is safe.",
		Tags:   []string{"resources"},
		Errors: linkErrors,
	}, func(ctx context.Context, in *revokeLinkNodeInput) (*revokeLinkNodeOutput, error) {
		if store == nil {
			return nil, unknownLink(in.Name)
		}

		link, err := store.RevokeLinkNode(ctx, principalFrom(ctx), in.Name, in.ID)
		if err != nil {
			return nil, err
		}

		return &revokeLinkNodeOutput{Body: link}, nil
	})
}
