package httpapi

import (
	"context"
	"fmt"
	"maps"
	"net/http"
	"strconv"

	"github.com/danielgtaylor/huma/v2"

	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpfault"
)

// The resource paths. The instance path is kind and name, not one opaque id,
// because ref.Ref renders exactly that way and a path a person types should be
// the same string they see in a log line.
const (
	resourcesPath = "/api/v1/resources"
	resourcePath  = resourcesPath + "/{kind}/{name}"

	// restartResourcePath is the one operation on this surface that changes
	// what an instance is doing without changing what it is declared to be. It
	// is a segment under the instance and not a member of the declaration,
	// because a restart leaves nothing behind for a later read to return.
	restartResourcePath = resourcePath + "/restart"
)

// listResourcesInput narrows the listing. Nothing here is a permission: what a
// caller may see is decided in the query that produces the rows.
//
// The enum duplicates ResourceRef.Kind's because a struct tag cannot hold a
// call. TestTheKindEnumMatchesTheClosedSet reads both copies, so neither can
// fall behind the closed set unnoticed.
type listResourcesInput struct {
	Kind string `query:"kind" enum:"storage,auth,sink,driver,db,history,link,asset" doc:"Narrow the listing to one kind." example:"storage"`
}

type listResourcesOutput struct {
	// ETag is the version of the overlay the listing was read from, which is
	// the artefact a write rewrites whole.
	ETag string `header:"ETag" doc:"The version of the configuration this listing was read from."`
	Body struct {
		Items []ResourceInstance `json:"items"`
	}
}

// ResourceRef is the path of one instance. It is exported because Huma binds
// parameters by reflection and cannot write into an unexported embedded field.
type ResourceRef struct {
	Kind string `path:"kind" enum:"storage,auth,sink,driver,db,history,link,asset" doc:"The resource kind."        example:"storage"`
	Name string `path:"name"                                                       doc:"The operator-chosen name." example:"local"   maxLength:"63"`
}

type getResourceInput struct {
	ResourceRef
}

// getResourceOutput is separate from resourceOutput because a read sends no
// Location. Huma derives a response's headers from the struct it returns, so
// sharing one type with the write would publish a Location on the 200 of a
// GET, which never sends one.
type getResourceOutput struct {
	Status int
	ETag   string `header:"ETag" doc:"The version to quote in If-Match on the next write."`
	Body   ResourceInstance
}

type resourceOutput struct {
	Status   int
	ETag     string `header:"ETag"     doc:"The version to quote in If-Match on the next write. Absent on a dry run, which wrote nothing to quote."`
	Location string `header:"Location" doc:"Where the created instance lives. Sent only with 201."`
	Body     ResourceInstance
}

// resourceSpecBody is what a PUT accepts: the declaration, plus every read-only
// member a GET returns, so the body of a read can be sent straight back and a
// client does not have to remove the read-only members first.
//
// The read-only members are optional and are not a second way to set anything.
// kind and name are compared against the path and refused when they disagree,
// which is stricter than dropping them; the rest are the server's own and are
// ignored. additionalProperties stays closed, so a member this binary does not
// publish is still a 422.
type resourceSpecBody struct {
	ResourceSpec

	Kind       string          `json:"kind,omitempty"        doc:"Ignored unless it disagrees with the path, which is refused."`
	Name       string          `json:"name,omitempty"        doc:"Ignored unless it disagrees with the path, which is refused."`
	UID        string          `json:"uid,omitempty"         doc:"Read-only. Accepted so a read can be sent back, and ignored."`
	Runtime    string          `json:"runtime,omitempty"     doc:"Read-only. Accepted so a read can be sent back, and ignored."`
	Namespace  string          `json:"namespace,omitempty"   doc:"Read-only. Accepted so a read can be sent back, and ignored."`
	Mount      string          `json:"mount,omitempty"       doc:"Read-only. Accepted so a read can be sent back, and ignored."`
	OpenAPIURL string          `json:"openapi_url,omitempty" doc:"Read-only. Accepted so a read can be sent back, and ignored."`
	UI         *ResourceUI     `json:"ui,omitempty"          doc:"Read-only. Accepted so a read can be sent back, and ignored."`
	Module     *ResourceModule `json:"module,omitempty"      doc:"Read-only. Accepted so a read can be sent back, and ignored."`
	Source     string          `json:"source,omitempty"      doc:"Read-only. Accepted so a read can be sent back, and ignored."`
	Version    string          `json:"version,omitempty"     doc:"Read-only. The precondition rides in If-Match, not here."`
	Generation int64           `json:"generation,omitempty"  doc:"Read-only. Accepted so a read can be sent back, and ignored."`
	Status     *ResourceStatus `json:"status,omitempty"      doc:"Read-only. Accepted so a read can be sent back, and ignored."`
}

// writeResourceInput carries the precondition that makes a write safe. An
// If-Match tag list or If-None-Match: * is required: without one a client that
// read the file a minute ago would overwrite whatever an operator typed since.
//
// The other two forms are accepted and evaluated, and neither is enough on its
// own. See Precondition.Fences for which is which and why.
type writeResourceInput struct {
	ResourceRef

	IfMatch     string `header:"If-Match"      doc:"The ETag last read, quotes included. A list is accepted; a weak validator is not. * alone does not fence a write."`
	IfNoneMatch string `header:"If-None-Match" doc:"Send * to create only, failing if the instance already exists. A tag list is compared weakly and alone does not fence a write."`
	DryRun      bool   `                       doc:"Validate and report the result without writing the file or reconciling."                                                        query:"dry_run" example:"true"`

	Body resourceSpecBody
}

type deleteResourceInput struct {
	ResourceRef

	IfMatch     string `header:"If-Match"      doc:"The ETag last read, quotes included. A list is accepted. * alone does not fence a delete."`
	IfNoneMatch string `header:"If-None-Match" doc:"A tag list, compared weakly: the delete applies only if none is current. It alone does not fence a delete."`
	DryRun      bool   `                       doc:"Validate without writing the file or reconciling."                                                          query:"dry_run" example:"true"`
}

type deleteResourceOutput struct {
	Status int
}

// restartResourceInput carries the instance and nothing else. There is no
// precondition and no dry run: a restart writes no file, so there is no version
// for a token to fence and nothing a rehearsal would tell a caller.
type restartResourceInput struct {
	ResourceRef
}

type restartResourceOutput struct {
	Status int
}

// mutationErrors are the refusals every write on this surface can answer with.
// A failed precondition is 412 and not 409: the token is sent in If-Match, and
// RFC 9110 §13.1.1 requires 412 for that. 409 is correct only for a token in
// the request body. 409 is still here, for the instance the operator declared
// by hand, which no token can unlock.
var mutationErrors = []int{
	http.StatusUnauthorized,
	http.StatusNotFound,
	http.StatusConflict,
	http.StatusPreconditionFailed,
	http.StatusUnprocessableEntity,
	http.StatusPreconditionRequired,
}

// registerResources publishes the resource inventory: the configured instances
// as a REST surface, with the supervision plane's view of each one beside the
// declaration that produced it.
func registerResources(api huma.API, store ResourceStore) {
	registerListResources(api, store)
	registerGetResource(api, store)
	registerPutResource(api, store)
	registerDeleteResource(api, store)
	registerRestartResource(api, store)
	registerResourceOperations(api, store)
}

func registerListResources(api huma.API, store ResourceStore) {
	register(api, huma.Operation{
		OperationID: "list-resources",
		Method:      http.MethodGet,
		Path:        resourcesPath,
		Summary:     "Configured resource instances",
		Description: "Every instance the caller may see, with the supervision plane's view of each. " +
			"Each instance reports the source that declared it: the operator's hand-authored file, or " +
			"the machine-owned overlay the API writes. The ETag is the version of that overlay. " +
			"Every setting the type declared as a credential reads as `xxxxx`, and a password written " +
			"into a declared address is replaced inside it. No caller reads a credential back over " +
			"this API, an administrator included.",
		Tags:   []string{"resources"},
		Errors: []int{http.StatusUnauthorized, http.StatusUnprocessableEntity},
	}, func(ctx context.Context, in *listResourcesInput) (*listResourcesOutput, error) {
		kind, err := parseKindFilter(in.Kind)
		if err != nil {
			return nil, err
		}

		listing, err := store.List(ctx, principalFrom(ctx), kind)
		if err != nil {
			return nil, err
		}

		out := &listResourcesOutput{}
		out.Body.Items = listing.Items

		if listing.Version != "" {
			if out.ETag, err = validatedETag(listing.Version); err != nil {
				return nil, err
			}
		}

		return out, nil
	})
}

func registerGetResource(api huma.API, store ResourceStore) {
	register(api, huma.Operation{
		OperationID: "get-resource",
		Method:      http.MethodGet,
		Path:        resourcePath,
		Summary:     "One configured resource instance",
		Description: "The declaration and the observed status. The ETag is the version a write must quote, " +
			"and the whole body can be sent back to PUT unchanged. " +
			"Every setting the type declared as a credential reads as `xxxxx`, and a password written " +
			"into a declared address is replaced inside it. No caller reads a credential back over " +
			"this API, an administrator included. Sending the body back unchanged keeps each stored " +
			"credential, because the marker means keep what is stored.",
		Tags:   []string{"resources"},
		Errors: []int{http.StatusUnauthorized, http.StatusNotFound, http.StatusUnprocessableEntity},
	}, func(ctx context.Context, in *getResourceInput) (*getResourceOutput, error) {
		instance, err := in.parse()
		if err != nil {
			return nil, err
		}

		found, err := store.Get(ctx, principalFrom(ctx), instance)
		if err != nil {
			return nil, err
		}

		etag, err := validatedETag(found.Version)
		if err != nil {
			return nil, err
		}

		return &getResourceOutput{Status: http.StatusOK, ETag: etag, Body: found}, nil
	})
}

func registerPutResource(api huma.API, store ResourceStore) {
	register(api, huma.Operation{
		OperationID: "put-resource",
		Method:      http.MethodPut,
		Path:        resourcePath,
		Summary:     "Declare a resource instance",
		Description: "Writes the declaration into the machine-owned overlay and reconciles. The spec is " +
			"validated first: a rejected one leaves the files untouched. An If-Match tag list or " +
			"If-None-Match: * is required, because a person editing the configuration by hand is the " +
			"other writer and neither of those can be sent without having read this instance or " +
			"asked that it not exist. " +
			"A write that creates answers 201 with a Location; one that replaces answers 200. " +
			"type is fixed at creation, and an instance whose source is the operator's file is refused. " +
			"A read never carries a credential, so `xxxxx` under a key the type declared as one means " +
			"keep the value that is stored. Write the credential itself to change it. The marker sent " +
			"for a credential this instance has none stored for is refused, because there would be " +
			"nothing to keep and the instance would hold the marker as its password. The answer is " +
			"redacted the way a read is. While the operator's core.modules_enabled is false, a write " +
			"that switches a module this server installed from off to on answers 409 " +
			"core.resource/MODULES_DISABLED. A write that leaves it as it is, or switches it off, " +
			"is accepted. In safe mode the same write answers 409 core.resource/SAFE_MODE, for a module " +
			"declared in the configuration file as well.",
		Tags:          []string{"resources"},
		DefaultStatus: http.StatusOK,
		Errors:        mutationErrors,
	}, func(ctx context.Context, in *writeResourceInput) (*resourceOutput, error) {
		instance, err := in.parse()
		if err != nil {
			return nil, err
		}

		if err = in.Body.checkNames(instance); err != nil {
			return nil, err
		}

		pre, err := precondition(in.IfMatch, in.IfNoneMatch, in.DryRun)
		if err != nil {
			return nil, err
		}

		applied, err := store.Apply(ctx, principalFrom(ctx), instance, in.Body.ResourceSpec, pre)
		if err != nil {
			return nil, err
		}

		return applyOutput(applied, instance, pre.DryRun)
	})

	documentCreatedResponse(api, resourcePath)
}

func registerDeleteResource(api huma.API, store ResourceStore) {
	register(api, huma.Operation{
		OperationID:   "delete-resource",
		Method:        http.MethodDelete,
		Path:          resourcePath,
		Summary:       "Remove a resource instance",
		Description:   "Removes the declaration from the machine-owned overlay and reconciles.",
		Tags:          []string{"resources"},
		DefaultStatus: http.StatusNoContent,
		Errors:        mutationErrors,
	}, func(ctx context.Context, in *deleteResourceInput) (*deleteResourceOutput, error) {
		instance, err := in.parse()
		if err != nil {
			return nil, err
		}

		pre, err := precondition(in.IfMatch, in.IfNoneMatch, in.DryRun)
		if err != nil {
			return nil, err
		}

		if err = store.Delete(ctx, principalFrom(ctx), instance, pre); err != nil {
			return nil, err
		}

		return &deleteResourceOutput{Status: http.StatusNoContent}, nil
	})
}

func registerRestartResource(api huma.API, store ResourceStore) {
	register(api, huma.Operation{
		OperationID: "restart-resource",
		Method:      http.MethodPost,
		Path:        restartResourcePath,
		Summary:     "Restart a running resource instance",
		Description: "Stops the instance and builds it again from the declaration in force, which is " +
			"how a failed sink or a dropped connection is retried without waiting for the next backoff " +
			"step. It takes operate_resource on the kind: a caller who may restart an instance is not " +
			"thereby a caller who may rewrite the settings a connection string lives in.\n\n" +
			"Nothing is written and no precondition is required, so there is no ETag to quote and none " +
			"is returned. The instance comes back as the merged configuration currently declares it, " +
			"not as whatever it was last built from.\n\n" +
			"An instance the supervision plane is not running answers 409 " +
			"core.validation/RESOURCE_NOT_RUNNING. A disabled instance is that case: starting it is a " +
			"write and takes configure_resource. A module this server installed answers 409 " +
			"core.resource/MODULES_DISABLED instead while the operator's core.modules_enabled is false. " +
			"In safe mode every module answers 409 core.resource/SAFE_MODE instead, whoever declared it.",
		Tags:          []string{"resources"},
		DefaultStatus: http.StatusNoContent,
		Errors: []int{
			http.StatusUnauthorized,
			http.StatusForbidden,
			http.StatusNotFound,
			http.StatusConflict,
			http.StatusUnprocessableEntity,
		},
	}, func(ctx context.Context, in *restartResourceInput) (*restartResourceOutput, error) {
		instance, err := in.parse()
		if err != nil {
			return nil, err
		}

		if err = store.Restart(ctx, principalFrom(ctx), instance); err != nil {
			return nil, err
		}

		return &restartResourceOutput{Status: http.StatusNoContent}, nil
	})
}

// applyOutput frames what the store wrote. A dry run carries no ETag and no
// version: it wrote nothing, so a token handed back would name a state that
// never existed and would fail the next If-Match it was quoted in.
func applyOutput(applied ApplyResult, instance ref.Ref, dryRun bool) (*resourceOutput, error) {
	if dryRun {
		applied.Instance.Version = ""

		return &resourceOutput{Status: http.StatusOK, Body: applied.Instance}, nil
	}

	etag, err := validatedETag(applied.Instance.Version)
	if err != nil {
		return nil, err
	}

	out := &resourceOutput{Status: http.StatusOK, ETag: etag, Body: applied.Instance}
	if applied.Created {
		out.Status = http.StatusCreated
		out.Location = resourcesPath + "/" + instance.Kind().String() + "/" + instance.ID()
	}

	return out, nil
}

// documentCreatedResponse publishes the 201 a creating PUT answers with.
// huma.Operation carries one DefaultStatus, so the second success status has to
// be added to the document by hand or a generated client will not know it.
// Location moves with it, because a replacing 200 does not send one.
func documentCreatedResponse(api huma.API, path string) {
	op := api.OpenAPI().Paths[path].Put

	ok := op.Responses[strconv.Itoa(http.StatusOK)]
	if ok == nil {
		return
	}

	created := *ok
	created.Description = "The instance did not exist and was created."
	created.Headers = maps.Clone(ok.Headers)

	delete(ok.Headers, "Location")

	op.Responses[strconv.Itoa(http.StatusCreated)] = &created
}

// validatedETag renders a version as an entity-tag, refusing one that cannot be
// represented. A store that mints a version carrying a double quote would
// otherwise put an unparseable ETag on the wire, and the next If-Match quoting
// it back would fail for a reason no client could see.
func validatedETag(version string) (string, error) {
	if !etagSafe(version) {
		return "", httpfault.Unexpected("the store produced a version that cannot be an entity-tag")
	}

	return etagOf(version), nil
}

// parse turns the path into a Ref, which is where the kind and the name are
// validated against the closed set and DNS-1123. Doing it here means the store
// never receives a name the rest of the system could not have produced.
func (r ResourceRef) parse() (ref.Ref, error) {
	kind := ref.Kind(r.Kind)
	if detail := unsupervisedDetail(kind); detail != "" {
		return ref.Ref{}, httpfault.RequestInvalid("path.kind",
			fmt.Sprintf("expected one of %s", resourceKinds()), detail)
	}

	instance, err := ref.New(kind, r.Name)
	if err != nil {
		return ref.Ref{}, httpfault.RequestInvalid("path.name",
			"expected a DNS-1123 label: lowercase letters, digits and hyphens", err.Error())
	}

	return instance, nil
}

// checkNames refuses a body that names a different instance from the path. The
// body is not a second way to choose what is written: it is echoed back from a
// read, and a disagreement is a client bug, so it is reported and not dropped
// in silence.
func (b resourceSpecBody) checkNames(instance ref.Ref) error {
	if b.Kind != "" && b.Kind != instance.Kind().String() {
		return httpfault.RequestInvalid("body.kind",
			"expected "+instance.Kind().String(), "the body names a different kind from the path")
	}

	if b.Name != "" && b.Name != instance.ID() {
		return httpfault.RequestInvalid("body.name",
			"expected "+instance.ID(), "the body names a different instance from the path")
	}

	return nil
}

// parseKindFilter accepts an empty filter, which is every kind.
func parseKindFilter(value string) (ref.Kind, error) {
	if value == "" {
		return "", nil
	}

	kind := ref.Kind(value)
	if detail := unsupervisedDetail(kind); detail != "" {
		return "", httpfault.RequestInvalid("query.kind",
			fmt.Sprintf("expected one of %s", resourceKinds()), detail)
	}

	return kind, nil
}

// unsupervisedDetail says why this surface refuses a kind, and returns the
// empty string for one it accepts. A kind outside the closed set and a kind
// this surface does not serve are two different corrections, and Supervised
// answers false to both.
func unsupervisedDetail(kind ref.Kind) string {
	class, err := kind.Classify()
	if err != nil {
		return "unknown resource kind"
	}

	if class != ref.ClassSupervised {
		return fmt.Sprintf("%s is a %s kind, which this surface does not serve", kind, class)
	}

	return ""
}

// precondition turns the conditional headers into the fencing token the store
// enforces. A write carrying none is refused: it could overwrite whatever an
// operator typed into the file in the meantime, and there is no safe default
// for it.
//
// Every form RFC 9110 §13.1 defines is accepted, because a cache, a proxy and
// every conforming client emit them: the quoted entity-tag, a comma-separated
// list, and "*". A weak validator in If-Match is the one refusal, and it is a
// 422 and not a 412 because re-reading the resource will not help.
//
// Accepted is not the same as sufficient, and Precondition.Fences decides
// which a write is. Parsing a form the RFC defines is conformance. Deciding
// that a write carries a token its sender could only hold by having read the
// resource is this API's own rule. Treating the two as one would accept
// If-None-Match: "i-made-this-up".
func precondition(ifMatch, ifNoneMatch string, dryRun bool) (Precondition, error) {
	matched, matchAny, ok := parseTagList(ifMatch)
	if !ok {
		return Precondition{}, httpfault.RequestInvalid("header.If-Match",
			`expected * or a comma-separated list of quoted entity-tags, as in If-Match: "abc123"`,
			"the If-Match header is not an RFC 9110 entity-tag list")
	}

	if anyWeak(matched) {
		return Precondition{}, httpfault.RequestInvalid("header.If-Match",
			"expected a strong entity-tag, without the W/ prefix",
			"RFC 9110 compares If-Match strongly, so a weak validator can never satisfy it")
	}

	noneMatched, noneAny, ok := parseTagList(ifNoneMatch)
	if !ok {
		return Precondition{}, httpfault.RequestInvalid("header.If-None-Match",
			`expected * or a comma-separated list of entity-tags, as in If-None-Match: * or W/"abc123"`,
			"the If-None-Match header is not an RFC 9110 entity-tag list")
	}

	pre := Precondition{
		IfMatch:      valuesOf(matched),
		IfMatchAny:   matchAny,
		IfNoneMatch:  valuesOf(noneMatched),
		MustNotExist: noneAny,
		DryRun:       dryRun,
	}

	if !pre.Fences() {
		return Precondition{}, httpfault.PreconditionRequired("If-Match")
	}

	return pre, nil
}

func valuesOf(tags []entityTag) []string {
	if len(tags) == 0 {
		return nil
	}

	values := make([]string, 0, len(tags))
	for _, tag := range tags {
		values = append(values, tag.value)
	}

	return values
}
