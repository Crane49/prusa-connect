package httpapi

import (
	"slices"

	"github.com/danielgtaylor/huma/v2"
)

// apiDescription is what the reference renders on its landing page, and it is
// the only prose in the document addressed to somebody who has never called
// this API. Scalar takes info.description as the body of the document entry in
// its sidebar, so a document without one opens on a blank page.
//
// It is markdown, and its headings become sidebar entries under the title.
const apiDescription = "Prusa Connect puts every printer on your network behind one HTTP API. " +
	"It runs on your own hardware and calls no cloud service.\n" +
	"\n" +
	"A printer is never configured here. It dials in over its connector's own wire and this server " +
	"admits it, so the fleet listing is what has connected rather than what somebody typed into a " +
	"file. Every printer carries the state it last reported and the verbs this server resolved it " +
	"accepts. Those verbs are its capability set, narrowed by its model and its firmware, and a " +
	"command outside the set is refused before the machine is reached.\n" +
	"\n" +
	"## The fleet, the stream and one command\n" +
	"\n" +
	"`GET /api/v1/devices` answers one page of the devices the caller may see.\n" +
	"\n" +
	"`GET /api/v1/events` is the same information as it changes, as a server-sent event stream.\n" +
	"\n" +
	"`POST /api/v1/devices/{device_id}/commands` sends one verb from a printer's capability set.\n" +
	"\n" +
	"## Credentials and grants\n" +
	"\n" +
	"Almost every operation needs a credential this server issued. A browser carries it in the " +
	"session cookie the login sets, and any other client sends it as a bearer token.\n" +
	"\n" +
	"An operation publishing an empty security requirement takes neither. Liveness, readiness, the " +
	"two login steps and the seven vocabulary operations answer anybody, and nothing else does.\n" +
	"\n" +
	"A credential says who the caller is. A grant on one device says what they may do with it, so " +
	"two callers holding valid credentials see different fleets. " +
	"`GET /api/v1/grants/device/{object_id}` reads the grants written on one device.\n" +
	"\n" +
	"## A refusal carries a code and a trace id\n" +
	"\n" +
	"Every failure is an RFC 9457 problem document. Branch on its `code`, which comes from a " +
	"published set, and never on the wording of `detail`. Quote its `trace_id` to an operator, who " +
	"can find the same request in the log.\n" +
	"\n" +
	"`GET /api/v1/vocabularies/error-codes` lists every code this build can return, with what a " +
	"caller does about each one.\n" +
	"\n" +
	"## The base URL is the origin serving this document\n" +
	"\n" +
	"The first server is `/`, which resolves against wherever this document was fetched from. A " +
	"request sent from this page therefore reaches the deployment being read. A client reading the " +
	"document outside a browser takes the second server and fills in the host it wants."

// apiTags describes every tag an operation carries and fixes the order the
// reference lists them in. OpenAPI 3.1 says a tool may order undeclared tags
// however it likes, so without this the sidebar lists bare tag names in an
// order nobody chose.
//
// The order is what a reader arrives for, then what they need in order to reach
// it, then what runs the process. Adding a tag to an operation without adding it
// here fails TestEveryTagAnOperationCarriesIsDescribed.
func apiTags() []*huma.Tag {
	tags := append([]*huma.Tag{
		{
			Name: "devices",
			Description: "The devices that have dialled in, which today means printers: what each one " +
				"is, what it may be told to do, and what it holds. Nothing here creates a device, " +
				"because a device is not configuration.",
		},
		{
			Name: dashboardsTag,
			Description: "The pages of cards people lay out, one of which the home page draws. Who " +
				"sees a dashboard follows the organization roles on the dashboard kind: everyone, " +
				"operators or admins. The server keeps each card's place and settings but never reads " +
				"the settings: they belong to the card type.",
		},
		{
			Name: "events",
			Description: "One server-sent event stream carrying everything the caller may observe, " +
				"from every printer and every resource at once.",
		},
		{
			Name: "history",
			Description: "What this server stored, once a history provider caught it off the event " +
				"bus: the events that happened and the measurements that were reported, searchable " +
				"by origin, by type and by time. A deployment that configures no provider keeps no " +
				"past and refuses these paths.",
		},
		{
			Name: "registrations",
			Description: "The devices waiting to be let onto this server. A device introduces itself " +
				"over a connector's own wire and displays a code; somebody decides whether it comes " +
				"in. A connector configured to admit on arrival puts nothing here.",
		},
		{
			Name: "cameras",
			Description: "The cameras an operator configured: one still frame, or a stream of them. " +
				"A build wired with no media plane serves none of these paths.",
		},
		{
			Name: "auth",
			Description: "Getting a credential and giving it back. The login is a redirect to the " +
				"configured issuer, and the callback is where the browser lands with a session.",
		},
		{
			Name: "grants",
			Description: "Who may do what, to which printer or resource kind. One grant is one edge " +
				"of the authorization graph, written as object, relation and subject.",
		},
		{
			Name: "access",
			Description: "What the caller may do, rather than which grants say so. It answers from " +
				"the same rows the server enforces on, so a client can render an interface without " +
				"attempting an action to find out whether it is allowed.",
		},
		{
			Name: "resources",
			Description: "The configured inventory: storage, databases, sinks, drivers, cameras and " +
				"authenticators. Each instance carries the operator's declaration and the " +
				"supervision plane's view of it. The addresses this server dials are declared here " +
				"too, under the connector that dials them.",
		},
		{
			Name: "discovery",
			Description: "The other printers this server hears on the local network, and whether it " +
				"announces itself there. Hearing a printer grants it nothing.",
		},
		{
			Name: "storages",
			Description: "What is inside this server's own storage: browsing it, and putting a file " +
				"in or taking one out. A storage instance is declared under resources, and these " +
				"paths are about its contents rather than its declaration.",
		},
		{
			Name: "vocabularies",
			Description: "The closed sets this build's behaviour depends on: error codes, capability " +
				"keys, event types, resource kinds, the authorization model and the linked " +
				"extensions. They are identical in every deployment of one build.",
		},
		{
			Name: "system",
			Description: "Whether this process is up, whether it will take traffic, who it thinks the " +
				"caller is, how its connectors are faring, and what it is measuring.",
		},
	}, developerTags()...)

	// A build without the dashboards carries no operation under their tag.
	if !dashboardsCarried {
		tags = slices.DeleteFunc(tags, func(tag *huma.Tag) bool { return tag.Name == dashboardsTag })
	}

	return tags
}

// apiServers is the base URL a request is sent to.
//
// The first is `/`, and it is first because it is the one that is always right:
// a relative server resolves against the origin the document was fetched from,
// which is the same origin that serves the operations, whether that is a laptop
// on port 8080 or a self-hosted instance behind a name. With no servers at all,
// OpenAPI 3.1 section 4.8.5 says a tool may assume `/`, and Scalar instead
// selects nothing and sends a request with no host.
//
// The second exists for a client reading the document outside a browser, where
// there is no origin to resolve against and the host has to be supplied.
func apiServers() []*huma.Server {
	return []*huma.Server{
		{
			URL: "/",
			Description: "The origin this document was fetched from. A request sent from the " +
				"reference reaches the deployment the reference is being read on.",
		},
		{
			URL:         "{scheme}://{host}",
			Description: "A deployment addressed by name, for a client that is not reading this document over HTTP.",
			Variables: map[string]*huma.ServerVariable{
				"scheme": {
					Enum:        []string{"http", "https"},
					Default:     "http",
					Description: "https wherever a certificate is configured, and http for a plain local run.",
				},
				"host": {
					Default:     "localhost:8080",
					Description: "The host and port the API listener is bound to, from core.api.addr.",
				},
			},
		},
	}
}
