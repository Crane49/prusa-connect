// Package httpfault registers the error codes the HTTP transport returns on
// its own account: the failures that happen before or instead of a handler,
// where no domain has an opinion. Codes a handler returns belong to the domain
// that raised them.
//
// It is a package of its own so cmd/generate-error-codes can link the
// registrations without linking a router, a web framework or an event hub.
// Nothing here imports outside sdk/fault and the standard library.
package httpfault

import (
	"net/http"
	"strconv"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// The param names these codes interpolate. A message bundle indexes them, so
// they are part of the published contract for the code that declares them.
const (
	paramNameLocation    = "location"
	paramNameReason      = "reason"
	paramNameMethod      = "method"
	paramNamePath        = "path"
	paramNameLimit       = "limit_bytes"
	paramNameHeader      = "header"
	paramNameResource    = "resource"
	paramNameVersion     = "version"
	paramNameField       = "field"
	paramNameSource      = "source"
	paramNameFile        = "file"
	paramNameStreamLimit = "limit_streams"
	paramNameCoarsest    = "coarsest_step"
	paramNameInstances   = "instances"
	paramNameVerb        = "verb"
	paramNameMount       = "mount"
	paramNameTimeout     = "timeout_ms"
)

// referenceSeparator joins a kind and a name into the one rendering a reference
// has on the wire.
const referenceSeparator = ":"

// Reference renders a resource instance the way a fault param carries it: the
// kind, a colon, then the name. It is what ref.Ref.String() produces, spelled
// again here because this package imports nothing of ours outside sdk/fault. A
// caller that already holds a ref.Ref passes its String() instead.
//
// A refusal that names the kind and withholds the name renders the kind alone,
// so a caller reads "account" rather than a reference with nothing after the
// colon.
func Reference(kind, name string) string {
	if name == "" {
		return kind
	}

	return kind + referenceSeparator + name
}

// The codes this package returns. Registration documents them, and the
// published error reference is generated from the registry.
var (
	requestInvalid = fault.Register(
		fault.DomainValidation(), "REQUEST_INVALID", http.StatusUnprocessableEntity, fault.Document(
			"The request does not match the schema the operation publishes",
			"Correct the member named by location and send it again. The schema is in the OpenAPI document at /openapi.json.",
			fault.RetryNever,
			fault.ShowDetail,
			paramNameLocation,
			paramNameReason,
		))

	bodyTooLarge = fault.Register(
		fault.DomainValidation(), "BODY_TOO_LARGE", http.StatusRequestEntityTooLarge, fault.Document(
			"The request body is larger than this endpoint accepts",
			"Send at most limit_bytes, or use the upload endpoint, which streams instead of buffering.",
			fault.RetryNever,
			fault.ShowDetail,
			paramNameLimit,
		))

	// uploadTooLarge is the storage upload's own refusal, apart from
	// bodyTooLarge, because only this bound follows core.api.max_upload_size.
	uploadTooLarge = fault.Register(
		fault.DomainValidation(), "UPLOAD_TOO_LARGE", http.StatusRequestEntityTooLarge, fault.Document(
			"The file is larger than this server accepts",
			"Send a file of at most {limit_bytes}. An administrator sets the largest file with core.api.max_upload_size.",
			fault.RetryNever,
			fault.ShowDetail,
			paramNameLimit,
		))

	routeNotFound = fault.Register(
		fault.DomainValidation(), "ROUTE_NOT_FOUND", http.StatusNotFound, fault.Document(
			"No operation is published at that path",
			"Check the path against the OpenAPI document at /openapi.json.",
			fault.RetryNever,
			fault.ShowDetail,
			paramNameMethod, paramNamePath,
		))

	methodNotAllowed = fault.Register(
		fault.DomainValidation(), "METHOD_NOT_ALLOWED", http.StatusMethodNotAllowed, fault.Document(
			"The path exists but publishes no operation for that method",
			"Use one of the methods the OpenAPI document lists for this path.",
			fault.RetryNever,
			fault.ShowDetail,
			paramNameMethod, paramNamePath,
		))

	preconditionRequired = fault.Register(
		fault.DomainValidation(), "PRECONDITION_REQUIRED", http.StatusPreconditionRequired, fault.Document(
			"The write carries no precondition, and this resource has another writer",
			"Read the resource, then send its ETag back in the named header, quotes included. Send "+
				"If-None-Match: * to create an instance that must not exist yet.",
			fault.RetryNever,
			fault.ShowDetail,
			paramNameHeader,
		))

	resourceNotFound = fault.Register(
		fault.DomainValidation(), "RESOURCE_NOT_FOUND", http.StatusNotFound, fault.Document(
			"No resource instance of that kind carries that name",
			"List the kind to see what is configured. A name is unique within its kind and nowhere wider.",
			fault.RetryNever,
			fault.ShowDetail,
			paramNameResource,
		))

	// 412 rather than 409, because the token rides in If-Match. RFC 9110
	// §13.1.1 mandates 412 for a failed precondition; 409 is correct only for a
	// token carried in the request body.
	versionConflict = fault.Register(
		fault.DomainValidation(), "VERSION_CONFLICT", http.StatusPreconditionFailed, fault.Document(
			"The instance changed since the version the write quotes",
			"Read the instance again, reapply the change to what it now says, and send the new ETag. "+
				"Somebody else wrote it, possibly by editing the configuration file directly.",
			fault.RetryNever,
			fault.ShowDetail,
			paramNameResource, paramNameVersion,
		))

	fieldImmutable = fault.Register(
		fault.DomainValidation(), "FIELD_IMMUTABLE", http.StatusUnprocessableEntity, fault.Document(
			"The write changes a field that is fixed for the life of the instance",
			"Delete the instance and declare a new one. A different implementation cannot inherit the "+
				"identity of the old one, and the data rows that reference it would follow the wrong type.",
			fault.RetryNever,
			fault.ShowDetail,
			paramNameField,
		))

	resourceNotWritable = fault.Register(
		fault.DomainValidation(), "RESOURCE_NOT_WRITABLE", http.StatusConflict, fault.Document(
			"The instance is declared in the hand-authored configuration file, which the API does not write",
			"Edit the file named by file and reload, or move the instance to the machine-owned overlay "+
				"first. The API writes the overlay only, so the operator's file keeps its comments.",
			fault.RetryNever,
			fault.ShowDetail,
			paramNameResource, paramNameSource, paramNameFile,
		))

	// It is core.validation and 409 for the reason RESOURCE_NOT_WRITABLE is:
	// the request is well formed and the state it asks about will not take it.
	// Restarting an instance nobody declared is RESOURCE_NOT_FOUND instead, so
	// this code never tells a caller that a name exists.
	resourceNotRunning = fault.Register(
		fault.DomainValidation(), "RESOURCE_NOT_RUNNING", http.StatusConflict, fault.Document(
			"The instance is declared and the supervision plane is not running it",
			"Read the instance and look at enabled. A disabled instance is started by declaring it "+
				"enabled, which is a write and takes configure_resource; a kind with nothing to "+
				"supervise has no instance to restart at all.",
			fault.RetryNever,
			fault.ShowDetail,
			paramNameResource,
		))

	// A module that claimed a root prefix and is not answering on it. It is 503
	// and not 404 so that an integration can tell "switched off here" from
	// "wrong server": the address is this deployment's, the module that owns it
	// is named, and the operator can say why in one read of the instance.
	moduleNotServing = fault.Register(
		fault.DomainValidation(), "MODULE_NOT_SERVING", http.StatusServiceUnavailable, fault.Document(
			"A module claims this address and is not serving on it",
			"Read the instance at /api/v1/resources to see where it stands. A module that is "+
				"switched on and whose files are not installed is installed with "+
				"`prusa-connect module add`; one that is switched off is started by "+
				"declaring it enabled.",
			fault.RetryAllowed,
			fault.ShowDetail,
			paramNameResource, paramNameMount,
		))

	// It sits beside BODY_TOO_LARGE in core.validation: both are a bound on what
	// one caller may ask this transport for, rather than a server failure or a
	// permission the caller lacks.
	tooManyStreams = fault.Register(
		fault.DomainValidation(), "TOO_MANY_STREAMS", http.StatusTooManyRequests, fault.Document(
			"This caller already holds as many event streams as the server allows one",
			"Close a stream before opening another. One stream carries every origin the caller may "+
				"observe, so a second is rarely needed; narrow one with the origins parameter instead.",
			fault.RetryAllowed,
			fault.ShowDetail,
			paramNameStreamLimit,
		))

	// 429 beside TOO_MANY_STREAMS: a bound on what callers may ask of this
	// server at once, not a failure. One bundle holds the log buffer scrubbed
	// in memory, and on a small board a second one would race it to the
	// request timeout.
	diagnosticsBusy = fault.Register(
		fault.DomainValidation(), "DIAGNOSTICS_BUSY", http.StatusTooManyRequests, fault.Document(
			"Another diagnostics bundle is being built",
			"Wait for the bundle in progress to finish, then ask again. The server builds one at a time.",
			fault.RetryAllowed,
			fault.ShowDetail,
		))

	// 501 rather than 404 or 503. The path is published by the contract on every
	// build, so it is not missing, and no wait makes it answer: this deployment
	// keeps no past of that kind. A feature that needs a history provider is
	// absent without one, and this code says so.
	//
	// It covers two deployments a caller cannot tell apart and does not need to:
	// one that configured no provider, and one whose provider serves the other
	// half of the contract. sdk/historyapi lets a provider carry facts and
	// refuse samples, and the reason a measurement cannot be read is the same
	// either way.
	historyNotConfigured = fault.Register(
		fault.DomainResource(), "HISTORY_NOT_CONFIGURED", http.StatusNotImplemented, fault.Document(
			"This deployment keeps no history of the kind the request asks for",
			"Declare a history instance under resources.history in the server's configuration. A "+
				"provider that is serving answers this on the half of the contract it does not serve, "+
				"and one that is restarting reads the same as one never declared. Events already "+
				"published are gone: a provider stores what it sees from the moment it is attached.",
			fault.RetryNever,
			fault.ShowDetail,
		))

	// A provider that serves this half of the contract and will not answer this
	// particular query truthfully. It is separate from HISTORY_NOT_CONFIGURED
	// because the two are different answers: that one says the feature is
	// absent, and this one says the feature is here and the window asked for is
	// not. A provider refuses such a query and does not approximate an answer.
	historyQueryRefused = fault.Register(
		fault.DomainResource(), "HISTORY_QUERY_REFUSED", http.StatusUnprocessableEntity, fault.Document(
			"The history provider will not answer a query in this shape",
			"Ask for a window the provider still keeps, or the coarsest step it names. A provider "+
				"refuses rather than approximating, because a short page of raw samples reads exactly "+
				"like a printer that was switched off.",
			fault.RetryNever,
			fault.ShowDetail,
			paramNameCoarsest,
		))

	// Two serving instances claiming one query verb. It is a configuration
	// conflict and not an absent feature, so it has its own code. The server
	// refuses and does not guess which store answers. The way out is a
	// `reads:` list on each of the two declarations.
	historyReaderAmbiguous = fault.Register(
		fault.DomainResource(), "HISTORY_READER_AMBIGUOUS", http.StatusConflict, fault.Document(
			"Two history instances both answer this query, and the server will not guess",
			"Write a reads: list on each of the two declarations, so that one of them names this "+
				"verb and the other does not. An instance with no reads: list claims every verb its "+
				"capabilities carry, so a list on one of the two leaves the other claiming this verb "+
				"as well.",
			fault.RetryNever,
			fault.ShowDetail,
			paramNameInstances, paramNameVerb,
		))

	// It sits beside BODY_TOO_LARGE and TOO_MANY_STREAMS in core.validation:
	// all three bound what one caller may ask this transport for. It is its own
	// code because the bound is per value and not per body, so a settings pane
	// that is refused can say which setting was too big rather than reporting
	// that the whole request was.
	stateValueTooLarge = fault.Register(
		fault.DomainValidation(), "STATE_VALUE_TOO_LARGE", http.StatusRequestEntityTooLarge, fault.Document(
			"The value is larger than one module state record may hold",
			"Store at most limit_bytes under one key. A document that does not fit is a file and "+
				"belongs on a storage instance, which streams.",
			fault.RetryNever,
			fault.ShowDetail,
			paramNameLimit,
		))

	// 501 rather than 404 or 503, on the model of HISTORY_NOT_CONFIGURED. The
	// path is published by the contract on every build, so it is not missing,
	// and no wait makes it answer: this deployment runs no record backend, so
	// there is nowhere for a module's settings to be kept.
	stateNotConfigured = fault.Register(
		fault.DomainResource(), "STATE_NOT_CONFIGURED", http.StatusNotImplemented, fault.Document(
			"This deployment keeps no module state, so there is nowhere to read or write a record",
			"Declare a db instance under resources.db in the server's configuration. Every module "+
				"namespace is served from that one backend, and a deployment with none serves no "+
				"module record at all.",
			fault.RetryNever,
			fault.ShowDetail,
		))

	// 501, on the model of STATE_NOT_CONFIGURED. The install paths are
	// published by the contract on every build, so they are not missing, and no
	// wait makes them answer: this process holds no writable configuration of
	// its own. An embedded build and a test server are the two.
	installNotConfigured = fault.Register(
		fault.DomainResource(), "INSTALL_NOT_CONFIGURED", http.StatusNotImplemented, fault.Document(
			"This build installs no module over HTTP",
			"Install the module with the `module add` command against this server's configuration "+
				"file, and restart the core. A build that serves the install paths is one started "+
				"from a configuration file it may write an overlay beside.",
			fault.RetryNever,
			fault.ShowDetail,
		))

	// 409 and retryable, because the state this names passes on its own. The
	// module's process has already been stopped and its files are still there,
	// which on Windows is a handle the system has not released yet. The
	// declaration is kept, so the module is still installed and the same call
	// is what finishes the job.
	moduleFilesHeld = fault.Register(
		fault.DomainResource(), "MODULE_FILES_HELD", http.StatusConflict, fault.Document(
			"The module's files could not be deleted, so it is still installed",
			"Send the same request again in a moment. If it keeps failing, something on this server "+
				"is holding a file under the named path open. Close it, or delete the directory by "+
				"hand, and the declaration goes with the next request.",
			fault.RetryAllowed,
			fault.ShowDetail,
			paramNamePath,
		))

	// 502 and retryable. The caller's request is correct and this server is
	// working; the device behind the instance did not answer, which is what a
	// gateway status says. A 500 here would send an operator reading this
	// server's own log for a failure that is on their network, and a browser
	// grid of thumbnails would report the server as broken while one camera is
	// unplugged.
	//
	// The detail is redacted because the reason carries the address this server
	// dialled and whatever the device wrote back. The resource param names the
	// camera for the log line that the trace id keys.
	cameraUnreachable = fault.Register(
		fault.DomainResource(), "CAMERA_UNREACHABLE", http.StatusBadGateway, fault.Document(
			"The camera did not answer, so there is no picture to serve",
			"Try again in a moment. If it keeps failing, check that the camera is powered on and "+
				"that this server reaches the address the instance declares.",
			fault.RetryAllowed,
			fault.RedactDetail,
			paramNameResource,
		))

	// 415 and core.validation, beside BODY_TOO_LARGE. The body is the caller's
	// own and no permission and no wait changes it: what arrived is not a
	// picture this server can serve back.
	//
	// The check is on the bytes and not on the Content-Type header, because a
	// header is what the sender claims. What is served from this origin to a
	// browser has to be what this server looked at, and a camera is an
	// untrusted device on somebody's network.
	frameNotAnImage = fault.Register(
		fault.DomainValidation(), "FRAME_NOT_AN_IMAGE", http.StatusUnsupportedMediaType, fault.Document(
			"The body is not the JPEG image this operation takes",
			"Send the frame as image/jpeg, with the encoded image as the whole body. Nothing is "+
				"converted here, so a camera that cannot encode JPEG cannot use this path.",
			fault.RetryNever,
			fault.ShowDetail,
			paramNameResource,
		))

	// 504 and core.validation, beside BODY_TOO_LARGE and TOO_MANY_STREAMS: all
	// three are a bound this transport puts on one request rather than a
	// failure inside a domain.
	//
	// The action text has to be honest about what a repeat costs. The budget
	// ends the caller's wait and not necessarily the work: a command already on
	// the wire reaches the printer whatever this server answers, so a second
	// POST is a second command and not a retry of the first.
	requestTimedOut = fault.Register(
		fault.DomainValidation(), "REQUEST_TIMED_OUT", http.StatusGatewayTimeout, fault.Document(
			"The request took longer than the budget this server gives one",
			"Read the resource to see whether the work went through before sending it again: the "+
				"budget ends the wait and not the work. An operator moves the bound at "+
				"core.api.request_timeout, and the streaming and file routes carry "+
				"core.api.stream_timeout instead.",
			fault.RetryAllowed,
			fault.ShowDetail,
			paramNameTimeout,
		))

	// A caller that hangs up before the answer never reads this code. It is
	// registered so the server's own log and its request metrics say what
	// happened: the caller left, and the server did not fail. 499 is the status
	// proxies already use for a client that closed the request.
	requestAbandoned = fault.Register(
		fault.DomainValidation(), "REQUEST_ABANDONED", statusClientClosedRequest, fault.Document(
			"The caller closed the request before the server answered it",
			"Read the resource to see whether the work went through before sending it again: "+
				"the caller stopped waiting, and work already on its way, such as a command "+
				"written to a printer, may still have happened.",
			fault.RetryAllowed,
			fault.ShowDetail,
		))

	// A link instance the host could not get a reading or a decision out of. It
	// is 503 and not 500 because the address is right and the answer may come:
	// the instance is declared, the operator can read where it stands, and a
	// link module that is restarting answers the next request.
	//
	// One code for every way it fails. A module that never answers, one that
	// answers past the call budget, and one that answers something this host
	// will not read are one situation for the operator: that instance is not
	// working, and its own log says which of the three it was.
	linkNotAnswering = fault.Register(
		fault.DomainInternal(), "LINK_NOT_ANSWERING", http.StatusServiceUnavailable, fault.Document(
			"The link did not answer this request inside the budget, or answered something this "+
				"server will not read",
			"Read the instance at /api/v1/resources to see where it stands, and read its log for "+
				"what the link said. A link that runs as a module is restarted by switching the "+
				"instance off and on again.",
			fault.RetryAllowed,
			fault.ShowDetail,
			paramNameResource, paramNameReason,
		))

	// A client that does not accept gzip makes the server decompress a file of
	// the web app while the client reads it. Only a few may do so at once, so
	// a printer's memory stays bounded. A browser accepts gzip and never meets
	// this code.
	appFileBusy = fault.Register(
		fault.DomainInternal(), "APP_FILE_BUSY", http.StatusServiceUnavailable, fault.Document(
			"The server is decompressing files of the web app for as many other clients as it allows",
			"Wait for the number of seconds in Retry-After and ask again, or send "+
				"Accept-Encoding: gzip to get the file at once. A browser always sends it, so a "+
				"browser never gets this answer.",
			fault.RetryAllowed,
			fault.ShowDetail,
		))

	moduleUploadsBusy = fault.Register(
		fault.DomainInternal(), "MODULE_UPLOADS_BUSY", http.StatusServiceUnavailable, fault.Document(
			"The module is receiving as many large bodies at once as the server allows it",
			"Wait for the number of seconds in Retry-After and send the body again.",
			fault.RetryAllowed,
			fault.ShowDetail,
		))

	moduleStorageFull = fault.Register(
		fault.DomainInternal(), "MODULE_STORAGE_FULL", http.StatusInsufficientStorage, fault.Document(
			"The disk that holds the module's data has no room for the request body",
			"Send a smaller body, or ask the operator to free space on the server's disk and then send it again.",
			fault.RetryAllowed,
			fault.ShowDetail,
		))

	unexpected = fault.Register(
		fault.DomainInternal(), "UNEXPECTED", http.StatusInternalServerError, fault.Document(
			"The server failed in a way it does not describe",
			fault.NoAction,
			fault.RetryNever,
			fault.RedactDetail,
		))
)

// RequestInvalid reports a request that failed the operation's schema. location
// is the member, in the dotted form the OpenAPI validator produces
// (body.printers[0].id, path.printerId); reason is what was wrong with it.
func RequestInvalid(location, reason, detail string) *fault.Fault {
	return requestInvalid.New(detail).With(
		fault.StringParam(paramNameLocation, location),
		fault.StringParam(paramNameReason, reason),
	)
}

// CameraUnreachable reports a camera this server could not read a frame from,
// naming the instance. Build the reference with Reference.
func CameraUnreachable(resource string) *fault.Fault {
	return cameraUnreachable.New("the camera could not be read").With(
		fault.StringParam(paramNameResource, resource),
	)
}

// CodeCameraUnreachable names a camera whose device did not answer.
func CodeCameraUnreachable() fault.Code { return cameraUnreachable.Code() }

// FrameNotAnImage reports a pushed camera frame this server will not store,
// naming the camera it was sent to. Build the reference with Reference.
func FrameNotAnImage(resource string) *fault.Fault {
	return frameNotAnImage.New("the body does not start with a JPEG image").With(
		fault.StringParam(paramNameResource, resource),
	)
}

// BodyTooLarge reports a body over the configured bound, naming the bound.
func BodyTooLarge(limit int64) *fault.Fault {
	return bodyTooLarge.New("request body over the limit").With(fault.IntParam(paramNameLimit, limit))
}

// UploadTooLarge reports a file uploaded to a storage over
// core.api.max_upload_size, naming the bound.
func UploadTooLarge(limit int64) *fault.Fault {
	return uploadTooLarge.New("file over the upload limit").With(fault.IntParam(paramNameLimit, limit))
}

// RequestTimedOut reports a request that outlived the budget this server gives
// one, naming the budget it outlived.
//
// The budget is carried in milliseconds under the key the supervisor's own
// timeout already uses, because it is the same fact: the bound the caller ran
// out of.
func RequestTimedOut(budget time.Duration) *fault.Fault {
	return requestTimedOut.New("the request outlived its budget").With(
		fault.IntParam(paramNameTimeout, budget.Milliseconds()),
	)
}

// CodeRequestTimedOut names a request that outlived its budget.
func CodeRequestTimedOut() fault.Code { return requestTimedOut.Code() }

// statusClientClosedRequest is the status for a caller that hung up. The
// standard library has no name for it, because no client ever receives it.
const statusClientClosedRequest = 499

// RequestAbandoned reports a request whose caller closed it before the server
// answered.
func RequestAbandoned() *fault.Fault {
	return requestAbandoned.New("the caller closed the request before the answer")
}

// CodeRequestAbandoned names a request whose caller closed it first.
func CodeRequestAbandoned() fault.Code { return requestAbandoned.Code() }

// RouteNotFound reports a path with no published operation.
func RouteNotFound(method, path string) *fault.Fault {
	return routeNotFound.New("no operation at this path").With(
		fault.StringParam(paramNameMethod, method),
		fault.StringParam(paramNamePath, path),
	)
}

// MethodNotAllowed reports a path that exists for other methods.
func MethodNotAllowed(method, path string) *fault.Fault {
	return methodNotAllowed.New("no operation for this method").With(
		fault.StringParam(paramNameMethod, method),
		fault.StringParam(paramNamePath, path),
	)
}

// PreconditionRequired reports a write with no fencing token, naming the header
// that carries one.
func PreconditionRequired(header string) *fault.Fault {
	return preconditionRequired.New("this write carries no token that fences it").With(
		fault.StringParam(paramNameHeader, header),
	)
}

// ResourceNotFound reports a named instance that is not configured. resource is
// the kind and the name as Reference renders them, or ref.Ref.String().
func ResourceNotFound(resource string) *fault.Fault {
	return resourceNotFound.New("no such resource instance").With(
		fault.StringParam(paramNameResource, resource),
	)
}

// VersionConflict reports a write whose fencing token no longer holds, naming
// the version the store has now. A write that fenced the whole document rather
// than one instance passes an empty resource.
func VersionConflict(resource, current string) *fault.Fault {
	return versionConflict.New("the instance moved on since that version").With(
		fault.StringParam(paramNameResource, resource),
		fault.StringParam(paramNameVersion, current),
	)
}

// FieldImmutable reports a write that changes a field fixed at creation, naming
// it in the dotted form the validator uses (body.type).
func FieldImmutable(field string) *fault.Fault {
	return fieldImmutable.New("that field is fixed for the life of the instance").With(
		fault.StringParam(paramNameField, field),
	)
}

// ResourceNotWritable reports a write aimed at an instance the operator declared
// by hand, naming the source and the file that holds it.
func ResourceNotWritable(resource, source, file string) *fault.Fault {
	return resourceNotWritable.New("this instance is not writable through the API").With(
		fault.StringParam(paramNameResource, resource),
		fault.StringParam(paramNameSource, source),
		fault.StringParam(paramNameFile, file),
	)
}

// ResourceNotRunning reports an instance the supervision plane is not running,
// for an operation that acts on a running one.
func ResourceNotRunning(resource string) *fault.Fault {
	return resourceNotRunning.New("nothing is supervising that instance").With(
		fault.StringParam(paramNameResource, resource),
	)
}

// CodeResourceNotRunning names an instance the supervision plane is not running.
func CodeResourceNotRunning() fault.Code { return resourceNotRunning.Code() }

// ModuleNotServing reports a root prefix a module claims and is not answering
// on, naming the instance, the prefix and why. It is what an old integration
// reads instead of a 404 that would read as the wrong server.
func ModuleNotServing(resource, mount, detail string) *fault.Fault {
	return moduleNotServing.New(detail).With(
		fault.StringParam(paramNameResource, resource),
		fault.StringParam(paramNameMount, mount),
	)
}

// CodeModuleNotServing names a module that claims an address and is not
// answering on it.
func CodeModuleNotServing() fault.Code { return moduleNotServing.Code() }

// LinkNotAnswering reports a link instance that gave this host no usable
// answer, naming the instance and what went wrong. reason is a short phrase for
// an operator and never the failure's own text, which names this host's
// internals.
func LinkNotAnswering(resource, reason, detail string) *fault.Fault {
	return linkNotAnswering.New(detail).With(
		fault.StringParam(paramNameResource, resource),
		fault.StringParam(paramNameReason, reason),
	)
}

// CodeLinkNotAnswering names a link that gave this host no usable answer.
func CodeLinkNotAnswering() fault.Code { return linkNotAnswering.Code() }

// TooManyStreams reports a caller at the per-principal stream bound, naming it.
func TooManyStreams(limit int) *fault.Fault {
	return tooManyStreams.New("this caller holds every event stream it is allowed").With(
		fault.IntParam(paramNameStreamLimit, int64(limit)),
	)
}

// CodeTooManyStreams names a caller at the per-principal stream bound.
func CodeTooManyStreams() fault.Code { return tooManyStreams.Code() }

// DiagnosticsBusy reports a bundle asked for while another is being built.
func DiagnosticsBusy() *fault.Fault {
	return diagnosticsBusy.New("another diagnostics bundle is being built")
}

// StateValueTooLarge reports a module state value over the configured bound,
// naming the bound.
func StateValueTooLarge(limit int64) *fault.Fault {
	return stateValueTooLarge.New("the value is over the limit for one module record").With(
		fault.IntParam(paramNameLimit, limit),
	)
}

// CodeStateValueTooLarge names a module state value over the bound.
func CodeStateValueTooLarge() fault.Code { return stateValueTooLarge.Code() }

// StateNotConfigured reports a deployment running no record backend, so no
// module record can be read or written.
func StateNotConfigured() *fault.Fault {
	return stateNotConfigured.New("this server runs no record backend")
}

// CodeStateNotConfigured names a deployment running no record backend.
func CodeStateNotConfigured() fault.Code { return stateNotConfigured.Code() }

// InstallNotConfigured reports a build that serves the module install paths and
// has nothing behind them.
func InstallNotConfigured() *fault.Fault {
	return installNotConfigured.New("this build installs no module over HTTP")
}

// CodeInstallNotConfigured names a build that installs no module over HTTP.
func CodeInstallNotConfigured() fault.Code { return installNotConfigured.Code() }

// ModuleFilesHeld reports an uninstall that stopped the module and could not
// delete its directory, naming the directory that is still there. The
// declaration is kept, so the module is still installed and the same request
// finishes it.
func ModuleFilesHeld(root, detail string) *fault.Fault {
	return moduleFilesHeld.New(detail).With(fault.StringParam(paramNamePath, root))
}

// HistoryNotConfigured reports a read of the past on a deployment that keeps
// none of that kind: no provider at all, or one that serves the other half of
// the contract.
func HistoryNotConfigured() *fault.Fault {
	return historyNotConfigured.New("this server keeps no history the request could be answered from")
}

// CodeHistoryNotConfigured names a deployment that keeps no history.
func CodeHistoryNotConfigured() fault.Code { return historyNotConfigured.Code() }

// HistoryQueryRefused reports a query the serving provider will not answer
// truthfully, naming the coarsest step it can still answer where there is one.
func HistoryQueryRefused(reason, coarsest string) *fault.Fault {
	return historyQueryRefused.New(reason).With(fault.StringParam(paramNameCoarsest, coarsest))
}

// CodeHistoryQueryRefused names a query a provider refused rather than
// approximated.
func CodeHistoryQueryRefused() fault.Code { return historyQueryRefused.Code() }

// HistoryReaderAmbiguous reports two serving instances claiming one query verb,
// naming both and the verb.
func HistoryReaderAmbiguous(verb, instances string) *fault.Fault {
	return historyReaderAmbiguous.New(
		"two history instances answer "+verb+": "+instances).With(
		fault.StringParam(paramNameVerb, verb),
		fault.StringParam(paramNameInstances, instances),
	)
}

// CodeHistoryReaderAmbiguous names two instances claiming one query verb.
func CodeHistoryReaderAmbiguous() fault.Code { return historyReaderAmbiguous.Code() }

// AppFileBusy reports a client without gzip that arrived while the server was
// already unpacking files for as many other clients as it allows.
func AppFileBusy(others int) *fault.Fault {
	return appFileBusy.New(strconv.Itoa(others) + " other clients are already being sent unpacked files")
}

// CodeAppFileBusy names a client without gzip that must wait for a slot.
func CodeAppFileBusy() fault.Code { return appFileBusy.Code() }

// ModuleUploadsBusy reports a large body refused because the module already
// receives slots of them.
func ModuleUploadsBusy(slots int) *fault.Fault {
	return moduleUploadsBusy.New(strconv.Itoa(slots) + " bodies are being received by this module already")
}

// CodeModuleUploadsBusy names a large body that must wait for a place.
func CodeModuleUploadsBusy() fault.Code { return moduleUploadsBusy.Code() }

// ModuleStorageFull reports a large body refused because the disk of the
// module's data has no room for it.
func ModuleStorageFull() *fault.Fault {
	return moduleStorageFull.New("the server's disk has no room for the body")
}

// CodeModuleStorageFull names a large body the disk has no room for.
func CodeModuleStorageFull() fault.Code { return moduleStorageFull.Code() }

// Unexpected reports a failure with no better description, and redacts: the
// cause reaches an operator through the log line the trace id keys, never
// through the response.
func Unexpected(detail string) *fault.Fault { return unexpected.New(detail) }

// CodeRequestInvalid names a request that failed schema validation.
func CodeRequestInvalid() fault.Code { return requestInvalid.Code() }

// CodeBodyTooLarge names a body over the endpoint's bound.
func CodeBodyTooLarge() fault.Code { return bodyTooLarge.Code() }

// CodeUploadTooLarge names a file over the storage upload bound.
func CodeUploadTooLarge() fault.Code { return uploadTooLarge.Code() }

// CodeRouteNotFound names a path with no published operation.
func CodeRouteNotFound() fault.Code { return routeNotFound.Code() }

// CodeMethodNotAllowed names a path that publishes no operation for the method.
func CodeMethodNotAllowed() fault.Code { return methodNotAllowed.Code() }

// CodePreconditionRequired names a write that carries no fencing token.
func CodePreconditionRequired() fault.Code { return preconditionRequired.Code() }

// CodeResourceNotFound names an instance that is not configured.
func CodeResourceNotFound() fault.Code { return resourceNotFound.Code() }

// CodeVersionConflict names a write whose fencing token no longer holds.
func CodeVersionConflict() fault.Code { return versionConflict.Code() }

// CodeFieldImmutable names a write that changes a field fixed at creation.
func CodeFieldImmutable() fault.Code { return fieldImmutable.Code() }

// CodeResourceNotWritable names an instance the operator declared by hand.
func CodeResourceNotWritable() fault.Code { return resourceNotWritable.Code() }

// CodeUnexpected names a failure the server does not describe.
func CodeUnexpected() fault.Code { return unexpected.Code() }
