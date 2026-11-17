package main

import (
	"errors"
	"fmt"
	"io/fs"
	"net"

	"github.com/prusa3d/fractal-link/natsbind"

	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// The codes this module registers.
//
// Every failure a person can see carries one of them. A person reads the links
// page in Czech or in Polish, and a translation is keyed by the code. The
// English sentence goes out beside the code, so a page with no translation for
// it still has something to show.
//
// A registration states what went wrong, what the operator does about it,
// whether sending the same request again can work, and the values the code
// fills into a sentence. Registering a code without those does not compile.
//
// The errors inside the transport, the join exchange and the bridge are plain
// Go errors and stay plain Go errors. They reach this module's log and nothing
// else, and a code for one of them would be a public identifier nobody reads.

// The param keys these codes fill in. A key names one fact, and the same fact
// keeps the same key everywhere.
const (
	paramField    = "field"
	paramFile     = "file"
	paramPath     = "path"
	paramID       = "id"
	paramReason   = "reason"
	paramLocation = "location"
	paramName     = "name"
)

// The HTTP statuses these codes advise. A host that answers over HTTP uses
// them, and a host that does not still reads the code.
const (
	statusBadRequest  = 400
	statusForbidden   = 403
	statusNotFound    = 404
	statusConflict    = 409
	statusInternal    = 500
	statusUnavailable = 503
)

// domain is the namespace of this module's codes. It is reverse-DNS, and it is
// not the core namespace, so a code of this module can never collide with a
// code of the server that runs it.
var domain = mustDomain("com.prusa.fractal")

// The codes an operator meets while this module starts.
//
//nolint:gochecknoglobals // a registered code is a public identifier; a const cannot hold one.
var (
	// ConfigFieldMissing is a setting the module cannot start without.
	ConfigFieldMissing = fault.Register(domain, "CONFIG_FIELD_MISSING", statusBadRequest, fault.Document(
		"A setting this module cannot start without is missing",
		"Write the named key in the config block of this instance and start the link again.",
		fault.RetryNever,
		fault.ShowDetail,
		paramField,
	))

	// ConfigDurationInvalid is a length of time written in a format this
	// module does not read.
	ConfigDurationInvalid = fault.Register(domain, "CONFIG_DURATION_INVALID", statusBadRequest, fault.Document(
		"A setting that holds a length of time is not written as one",
		"Write the value as a number and a unit, such as 5m or 500ms, and start the link again.",
		fault.RetryNever,
		fault.ShowDetail,
		paramField,
	))

	// ConfigValueInvalid is a setting this module read and refused for a
	// reason of its own: a shadow pattern it cannot parse, a verification mode
	// it does not know.
	ConfigValueInvalid = fault.Register(domain, "CONFIG_VALUE_INVALID", statusBadRequest, fault.Document(
		"A setting holds a value this module does not accept",
		"Read the named key in the module's README and write a value it lists.",
		fault.RetryNever,
		fault.ShowDetail,
		paramField,
	))

	// GrantMissing is a port or a data directory the settings need and the
	// operator has not granted the module.
	GrantMissing = fault.Register(domain, "GRANT_MISSING", statusBadRequest, fault.Document(
		"The settings need a port or a data directory the operator has not granted this module",
		"Grant the named port with approve_listen, or the data directory with approve, and start the link again.",
		fault.RetryNever,
		fault.ShowDetail,
		paramName,
	))

	// DataDirUnusable is the directory this node stores its messages and its
	// keys in, which it could not create or could not write to.
	DataDirUnusable = fault.Register(domain, "DATA_DIR_UNUSABLE", statusInternal, fault.Document(
		"This node cannot create or write the directory it stores its messages and keys in",
		"Give the process write access to the named directory, or point data_dir at one it can write.",
		fault.RetryNever,
		fault.ShowDetail,
		paramPath,
	))

	// TLSFileUnreadable is a certificate, a private key or an authority file
	// this node could not read.
	TLSFileUnreadable = fault.Register(domain, "TLS_FILE_UNREADABLE", statusInternal, fault.Document(
		"This node cannot read one of the TLS files its settings name",
		"Check that the named file is there and that the process may read it, then start the link again.",
		fault.RetryNever,
		fault.ShowDetail,
		paramFile,
	))

	// ListenerUnavailable is an address this node was told to accept
	// connections on and could not take.
	ListenerUnavailable = fault.Register(domain, "LISTENER_UNAVAILABLE", statusInternal, fault.Document(
		"This node cannot listen on the address its settings name",
		"Free the port, or write another address in accept.client_listen or accept.leaf_listen.",
		fault.RetryNever,
		fault.ShowDetail,
		paramLocation,
	))

	// TransportUnavailable is the transport a call arrived for before it was
	// built, or after it stopped.
	TransportUnavailable = fault.Register(domain, "TRANSPORT_UNAVAILABLE", statusUnavailable, fault.Document(
		"This link is not serving yet",
		"Wait for the link to finish starting and send the request again.",
		fault.RetryAllowed,
		fault.ShowDetail,
	))

	// StartFailed is a start this module gave up on for a reason none of the
	// codes above names.
	StartFailed = fault.Register(domain, "START_FAILED", statusInternal, fault.Document(
		"This module could not start the Fractal node",
		"Read this module's log for the named reason. The host starts the process again on its own.",
		fault.RetryAllowed,
		fault.ShowDetail,
		paramReason,
	))
)

// startFailure names why this module could not build the transport.
//
// The reasons an operator can act on get their own code and the value the
// operator has to change: the directory, the file, the address. Everything else
// gets StartFailed with the reason on it, because an operator who cannot act on
// a failure still needs to be able to quote one identifier.
func startFailure(cfg Config, err error) error {
	switch {
	case errors.Is(err, natsbind.ErrNoDataDir), errors.Is(err, fs.ErrPermission), errors.Is(err, fs.ErrNotExist):
		return DataDirUnusable.New("this node could not use the directory data_dir names").
			With(fault.StringParam(paramPath, cfg.DataDir)).Wrap(err)
	case errors.Is(err, natsbind.ErrNoAnchors):
		return TLSFileUnreadable.New("this node read no certificate out of the authority files").
			With(fault.StringParam(paramFile, caFileOf(cfg))).Wrap(err)
	case errors.Is(err, natsbind.ErrServerNotReady):
		return ListenerUnavailable.New("this node could not open the addresses its accept block names").
			With(fault.StringParam(paramLocation, listenAddress(cfg))).Wrap(err)
	}

	if addressed, ok := errors.AsType[*net.OpError](err); ok {
		return ListenerUnavailable.New("this node could not take the address").
			With(fault.StringParam(paramLocation, addressOf(addressed, cfg))).Wrap(err)
	}

	return StartFailed.New("this node could not build the transport").
		With(fault.StringParam(paramReason, err.Error())).Wrap(err)
}

// listenAddress is the address of the accept block an operator would change. It
// is the client address when there is one, and the leafnode address otherwise.
func listenAddress(cfg Config) string {
	if cfg.Accept == nil {
		return ""
	}

	if cfg.Accept.ClientListen != "" {
		return cfg.Accept.ClientListen
	}

	return cfg.Accept.LeafListen
}

// addressOf is the address the network error names, and the address of the
// accept block when the error names none.
func addressOf(failed *net.OpError, cfg Config) string {
	if failed.Addr != nil {
		return failed.Addr.String()
	}

	return listenAddress(cfg)
}

// caFileOf is the first authority file the settings name, and an empty string
// when they name none.
func caFileOf(cfg Config) string {
	if cfg.TLS == nil || len(cfg.TLS.CAFiles) == 0 {
		return ""
	}

	return cfg.TLS.CAFiles[0]
}

// The codes an operator meets while servers join and while a person decides
// about them.
//
//nolint:gochecknoglobals // a registered code is a public identifier; a const cannot hold one.
var (
	// JoinDenied is the server above refusing this node's join request. The
	// param reason is the protocol's own name for the refusal, such as
	// ERR_JOIN_UNAUTHORIZED+1.
	JoinDenied = fault.Register(domain, "JOIN_DENIED", statusForbidden, fault.Document(
		"The server above refused this node's request to join",
		"Read the named protocol code on the links page. A node waiting for approval needs a person "+
			"on the server above to approve it, and it asks again by itself. A node with a bad credential "+
			"needs a new one, and it asks again when the module starts with it. After a rejection, a "+
			"name collision or a revocation, fix the cause on the server above. Then stop the module, "+
			"delete refusal.json from its data directory and start it.",
		fault.RetryAllowed,
		fault.ShowDetail,
		paramReason,
	))

	// GrantRefused is a grant this node refused. It comes from another server
	// above, names another Node Path or misses a field. Or the server above
	// runs a protocol version or offers extension data this node does not
	// support.
	GrantRefused = fault.Register(domain, "GRANT_REFUSED", statusBadRequest, fault.Document(
		"This server refused what the server above granted it",
		"Read the reason on the links page. Fix what it names, on this server or on the server above, "+
			"such as the protocol version, the files block or the address of the server above. Then stop "+
			"the module, delete refusal.json from its data directory and start it.",
		fault.RetryNever,
		fault.ShowDetail,
		paramReason,
	))

	// RefusalUnreadable is a saved refusal this node cannot read, as a power
	// loss can leave it. The node asks the server above nothing, because the
	// file may hold a refusal it must keep.
	RefusalUnreadable = fault.Register(domain, "REFUSAL_UNREADABLE", statusInternal, fault.Document(
		"This server cannot read its saved refusal and asks the server above nothing",
		"Read the error in this module's log. Then stop the module, delete refusal.json from its data "+
			"directory and start it.",
		fault.RetryNever,
		fault.ShowDetail,
		paramReason,
	))

	// NodeUnknown is an approve or a revoke naming a server this link has no
	// record of. The link answers the same way for an id it never saw and for
	// an id it has forgotten, so a caller cannot learn which ids it holds.
	NodeUnknown = fault.Register(domain, "NODE_UNKNOWN", statusNotFound, fault.Document(
		"This link holds no server under that id",
		"Open the links page and take the id from the list of servers below this one.",
		fault.RetryNever,
		fault.ShowDetail,
		paramID,
	))

	// DecisionRefused is an approve or a revoke the transport would not carry
	// out: the record store is full, or the node's admission cannot be
	// written.
	DecisionRefused = fault.Register(domain, "DECISION_REFUSED", statusConflict, fault.Document(
		"This link could not record the decision about that server",
		"Read this module's log for the named reason, then send the decision again.",
		fault.RetryAllowed,
		fault.ShowDetail,
		paramID, paramReason,
	))
)

// The codes a caller of this module's own routes meets. They name a request
// that never reached the network, so they are this module's codes and not the
// numbers of the Fractal specification. A method call that did reach the
// network is answered with a Fractal code instead, from bridge/codes.go.
//
//nolint:gochecknoglobals // a registered code is a public identifier; a const cannot hold one.
var (
	// RouteNodeInvalid is an address whose node part is not a node path.
	RouteNodeInvalid = fault.Register(domain, "ROUTE_NODE_INVALID", statusBadRequest, fault.Document(
		"The address does not name a place in the tree",
		"Take the path from GET /nodes and send the request again.",
		fault.RetryNever,
		fault.ShowDetail,
	))

	// RouteLimitInvalid is a page size that is not a whole number above zero.
	RouteLimitInvalid = fault.Register(domain, "ROUTE_LIMIT_INVALID", statusBadRequest, fault.Document(
		"The page size is not a number this route accepts",
		"Write limit as a whole number from 1 to 500, or leave it out.",
		fault.RetryNever,
		fault.ShowDetail,
	))

	// RouteCallInvalid is an address that names no interface, no instance or
	// no method.
	RouteCallInvalid = fault.Register(domain, "ROUTE_CALL_INVALID", statusBadRequest, fault.Document(
		"The address does not name a method of an interface of a node",
		"Write the address as /nodes/{path}/interfaces/{interface}/{instance}/methods/{method}.",
		fault.RetryNever,
		fault.ShowDetail,
	))

	// RouteParamsInvalid is a body that is not a JSON array.
	RouteParamsInvalid = fault.Register(domain, "ROUTE_PARAMS_INVALID", statusBadRequest, fault.Document(
		"The parameters are not a JSON array",
		"Send the parameters as a JSON array, in the order the method declares them, or send no body.",
		fault.RetryNever,
		fault.ShowDetail,
	))

	// ResultTooLarge is a result the node below returned that is larger than
	// one answer of a module route may be. The method ran: only the answer
	// does not fit.
	ResultTooLarge = fault.Register(domain, "RESULT_TOO_LARGE", statusInternal, fault.Document(
		"The node below returned more than this route may answer",
		"Read the value on the server that owns the printer. The method ran: only the answer does not fit.",
		fault.RetryNever,
		fault.ShowDetail,
	))

	// CallFailed is a method call this server could not send: the node is not
	// below this one, no server on the way admitted it, or no answer arrived
	// before the call expired.
	CallFailed = fault.Register(domain, "CALL_FAILED", statusUnavailable, fault.Document(
		"This server could not send the call to the node below",
		"Check on GET /nodes that the node is there and connected, then send the call again.",
		fault.RetryAllowed,
		fault.ShowDetail,
	))

	// FilesOff is a share route on a server whose settings have no files
	// block.
	FilesOff = fault.Register(domain, "FILES_OFF", statusNotFound, fault.Document(
		"This server does not run the file extension",
		"Add a files block to the settings of this link and start it again.",
		fault.RetryNever,
		fault.ShowDetail,
	))

	// ShareInvalid is an address that names the root folder of a storage, no
	// storage, a file, a folder that does not exist, or a path longer than a
	// storage takes.
	ShareInvalid = fault.Register(domain, "SHARE_INVALID", statusBadRequest, fault.Document(
		"The address does not name a folder that can be shared",
		"Name a folder inside the storage. The root folder of a storage cannot be shared.",
		fault.RetryNever,
		fault.ShowDetail,
	))

	// ShareDenied is a person who may operate this link and may not read the
	// storage that holds the folder.
	ShareDenied = fault.Register(domain, "SHARE_DENIED", statusForbidden, fault.Document(
		"You may not read the storage that holds this folder",
		"Ask the operator for read_storage on this server, then share the folder again.",
		fault.RetryNever,
		fault.ShowDetail,
	))

	// ShareNotSaved is a change to the shared folders this module could not
	// write to its data directory. The shared folders stay as they were.
	ShareNotSaved = fault.Register(domain, "SHARE_NOT_SAVED", statusInternal, fault.Document(
		"This server could not write the shared folders",
		"Check that the module may write its data directory, then send the request again.",
		fault.RetryAllowed,
		fault.ShowDetail,
	))

	// StoreAddressInvalid is an address that does not name a node, an
	// instance and an identity.
	StoreAddressInvalid = fault.Register(domain, "STORE_ADDRESS_INVALID", statusBadRequest, fault.Document(
		"The address does not name a listing of a content store",
		"Take the node, the instance and the root hash from GET /files/stores and send the request again.",
		fault.RetryNever,
		fault.ShowDetail,
	))

	// StoreOutsideLine is a node that is neither above nor below this server,
	// whose files this server cannot reach.
	StoreOutsideLine = fault.Register(domain, "STORE_OUTSIDE_LINE", statusNotFound, fault.Document(
		"This server cannot read the stores of that node",
		"Name a node above or below this server. GET /files/shares lists the folders this server shares itself.",
		fault.RetryNever,
		fault.ShowDetail,
	))

	// ListingNotFound is a listing the node above or below does not publish
	// or pass on.
	ListingNotFound = fault.Register(domain, "LISTING_NOT_FOUND", statusNotFound, fault.Document(
		"The node does not publish this listing",
		"Read the root hash on GET /files/stores again. A folder may have been unshared since.",
		fault.RetryNever,
		fault.ShowDetail,
	))

	// ListingUnavailable is a listing this server could not read now: no
	// answer came, or the answer was not a listing.
	ListingUnavailable = fault.Register(domain, "LISTING_UNAVAILABLE", statusUnavailable, fault.Document(
		"This server could not read the listing now",
		"Check on GET /nodes that the node is connected, then send the request again.",
		fault.RetryAllowed,
		fault.ShowDetail,
	))
)

// mustDomain builds the namespace of this module's codes. A misspelling here is
// a mistake in this file, so it stops the process at start-up rather than
// answering a person with an empty identifier.
func mustDomain(name string) fault.Domain {
	built, err := fault.NewDomain(name)
	if err != nil {
		panic(fmt.Sprintf("%s: %q is not a fault domain: %v", name, name, err))
	}

	return built
}
