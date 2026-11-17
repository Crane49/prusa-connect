package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/kernel/condition"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpfault"
	"github.com/prusa3d/prusa-connect/sdk/extension"
)

// validSpec is a body every write test can send. It restates the seeded type,
// because type is immutable and a write that changed it would be refused for a
// reason the test is not about.
const validSpec = `{"type":"local","enabled":true,"required":false,"config":{}}`

func TestListingResourcesReturnsWhatIsConfigured(t *testing.T) {
	t.Parallel()

	rec := call(t, serverWith(t, seededStore()).Handler(), http.MethodGet, resourcesPath, "")

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, `"v1"`, rec.Header().Get("ETag"),
		"the collection is the artefact a write rewrites whole, so it carries a token too")

	var body struct {
		Items []ResourceInstance `json:"items"`
	}

	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Len(t, body.Items, 1)
	require.Equal(t, "storage", body.Items[0].Kind)
	require.Equal(t, "local", body.Items[0].Name)
	require.Equal(t, "overlay", body.Items[0].Source, "a client has to be told what it may write")
	require.NotEmpty(t, body.Items[0].UID)
	require.Equal(t, "ready", body.Items[0].Status.State)
	require.JSONEq(t, `{"root":"/var/lib/prusa"}`, string(body.Items[0].Config))
}

func TestListingResourcesRefusesAnUnknownKind(t *testing.T) {
	t.Parallel()

	rec := call(t, serverWith(t, seededStore()).Handler(), http.MethodGet, resourcesPath+"?kind=teapot", "")

	body := requireProblem(t, rec, http.StatusUnprocessableEntity)
	require.Equal(t, httpfault.CodeRequestInvalid(), body.Code)
}

func TestGettingAResourceCarriesTheVersionAsAQuotedETag(t *testing.T) {
	t.Parallel()

	rec := call(t, serverWith(t, seededStore()).Handler(), http.MethodGet, resourcesPath+"/storage/local", "")

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	require.Equal(t, `"v1"`, rec.Header().Get("ETag"),
		"RFC 9110 defines an entity-tag as a quoted string; a bare token is one no conforming client can quote back")
}

func TestGettingAnAbsentResourceIsAProblemDocument(t *testing.T) {
	t.Parallel()

	rec := call(t, serverWith(t, seededStore()).Handler(), http.MethodGet, resourcesPath+"/storage/missing", "")

	body := requireProblem(t, rec, http.StatusNotFound)
	require.Equal(t, httpfault.CodeResourceNotFound(), body.Code)
}

// TestAWriteWithoutAPreconditionIsRefused is the property the whole write path
// exists for. The other writer is a person with the file open in an editor, and
// a blind read-modify-write is what loses their change.
func TestAWriteWithoutAPreconditionIsRefused(t *testing.T) {
	t.Parallel()

	store := seededStore()

	for name, request := range map[string]struct {
		method string
		body   string
	}{
		"put":    {method: http.MethodPut, body: validSpec},
		"delete": {method: http.MethodDelete},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			rec := call(t, serverWith(t, store).Handler(), request.method, resourcesPath+"/storage/local", request.body)

			body := requireProblem(t, rec, http.StatusPreconditionRequired)
			require.Equal(t, httpfault.CodePreconditionRequired(), body.Code)
			require.Equal(t, "If-Match", paramString(t, body.Params, "header"))
		})
	}

	require.Equal(t, "v1", store.instances["storage/local"].Version, "nothing may have been written")
}

// TestOnlyAFencingTokenSatisfiesTheWritePrecondition is the regression this
// package exists to make impossible. Widening If-None-Match from "*" to an RFC
// 9110 tag list carries the "a precondition is required" check along with it,
// and a tag list that matches nothing holds against every state there is. A
// caller that never read the resource can then send a tag it invented and
// replace or delete it.
//
// If-Match: * is the same shape by a different door. It asks that something is
// there and nothing about what, so it overwrites whatever an operator saved a
// second earlier. The refusal names the header that carries a real one, and
// httpfault's remediation names the two forms that are: an ETag in If-Match, or
// If-None-Match: *.
func TestOnlyAFencingTokenSatisfiesTheWritePrecondition(t *testing.T) {
	t.Parallel()

	for name, header := range map[string]http.Header{
		"a tag the caller invented":               {"If-None-Match": {`"i-made-this-up"`}},
		"a list of invented tags":                 {"If-None-Match": {`"nope", W/"also-nope"`}},
		"the tag the caller just read":            {"If-None-Match": {`"v1"`}},
		"star in If-Match, which pins no version": {"If-Match": {anyTag}},
		"both of the non-fencing forms at once": {
			"If-Match": {anyTag}, "If-None-Match": {`"i-made-this-up"`},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			for _, method := range []string{http.MethodPut, http.MethodDelete} {
				store := seededStore()

				body := validSpec
				if method == http.MethodDelete {
					body = ""
				}

				rec := write(t, serverWith(t, store).Handler(), method,
					resourcesPath+"/storage/local", header, body)

				problem := requireProblem(t, rec, http.StatusPreconditionRequired)
				require.Equal(t, httpfault.CodePreconditionRequired(), problem.Code)
				require.Equal(t, "If-Match", paramString(t, problem.Params, "header"))

				instance, present := store.instances["storage/local"]
				require.Truef(t, present, "%s removed the instance", method)
				require.Equalf(t, "v1", instance.Version, "%s wrote", method)
				require.Truef(t, instance.Enabled, "%s changed the declaration", method)
			}
		})
	}
}

// TestRepeatedConditionalFieldLinesAreOneList pins RFC 9110 §5.3: two field
// lines of a list-valued header are the comma-joined value, and a recipient
// that reads only the first is refusing a request the client cannot correct.
// Huma binds a header parameter with Header.Get, which is exactly that, so the
// two forms below answer 412 and 200 for the same header set.
func TestRepeatedConditionalFieldLinesAreOneList(t *testing.T) {
	t.Parallel()

	for name, header := range map[string]http.Header{
		"the current tag on the second line": {"If-Match": {`"v0"`, `"v1"`}},
		"the current tag on the first line":  {"If-Match": {`"v1"`, `"v9"`}},
		"three lines, one of them current":   {"If-Match": {`"v0"`, `"v9"`, `"v1"`}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			rec := write(t, serverWith(t, seededStore()).Handler(), http.MethodPut,
				resourcesPath+"/storage/local", header, validSpec)

			require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
		})
	}
}

// TestRepeatedIfMatchLinesThatAreAllStaleStillFail is the other half: folding
// the lines must not turn a list of stale tags into a pass.
func TestRepeatedIfMatchLinesThatAreAllStaleStillFail(t *testing.T) {
	t.Parallel()

	rec := write(t, serverWith(t, seededStore()).Handler(), http.MethodPut,
		resourcesPath+"/storage/local", http.Header{"If-Match": {`"v0"`, `"v9"`}}, validSpec)

	require.Equal(t, http.StatusPreconditionFailed, rec.Code, "body: %s", rec.Body.String())
}

// TestAReadSendsNoLocation covers the document as well as the wire. Location
// belongs to the 201 a creating PUT answers with; a read has nowhere to point
// at that is not the URL the client already used.
func TestAReadSendsNoLocation(t *testing.T) {
	t.Parallel()

	server := newTestServer(t)

	rec := call(t, server.Handler(), http.MethodGet, resourcesPath+"/storage/local", "")
	require.Equal(t, http.StatusOK, rec.Code)
	require.Empty(t, rec.Header().Get("Location"))

	ok := server.api.OpenAPI().Paths[resourcePath].Get.Responses["200"]
	require.NotNil(t, ok)
	require.NotContains(t, ok.Headers, "Location",
		"the document promises a header the read never sends")
	require.Contains(t, ok.Headers, "ETag")
}

// TestEveryConditionalFormTheRFCDefinesIsAccepted is the defect this table
// exists for. A server emitting a bare token as its ETag and comparing If-Match
// byte for byte fails every form RFC 9110 requires a client, a cache or a proxy
// to send, forever, with no way for the client to correct it.
func TestEveryConditionalFormTheRFCDefinesIsAccepted(t *testing.T) {
	t.Parallel()

	for name, test := range map[string]struct {
		ifMatch     string
		ifNoneMatch string
		status      int
		code        string
	}{
		"the quoted tag the read handed out": {ifMatch: `"v1"`, status: http.StatusOK},
		"a list holding the current tag":     {ifMatch: `"v0", "v1"`, status: http.StatusOK},
		"a list holding only stale tags":     {ifMatch: `"v0", "v9"`, status: http.StatusPreconditionFailed},
		"a stale tag":                        {ifMatch: `"v0"`, status: http.StatusPreconditionFailed},
		// RFC 9110 §5.6.1.2: a recipient parses and ignores an empty list
		// element. Both of these are the list holding the current tag.
		"a list with a trailing empty element": {ifMatch: `"v1",`, status: http.StatusOK},
		"a list with a leading empty element":  {ifMatch: `, "v1"`, status: http.StatusOK},
		"a field carrying separators and no member": {
			ifMatch: ",", status: http.StatusUnprocessableEntity, code: "REQUEST_INVALID",
		},
		"a weak validator, which If-Match compares strongly": {
			ifMatch: `W/"v1"`, status: http.StatusUnprocessableEntity, code: "REQUEST_INVALID",
		},
		"an unquoted token, which is not an entity-tag": {
			ifMatch: "v1", status: http.StatusUnprocessableEntity, code: "REQUEST_INVALID",
		},
		// The three forms below parse, which is what this table is about. None
		// of them is a fencing token, so the write is refused for that instead
		// of being carried out; TestOnlyAFencingTokenSatisfiesTheWritePrecondition
		// is where that rule lives.
		"star, meaning whatever is there": {
			ifMatch: anyTag, status: http.StatusPreconditionRequired, code: "PRECONDITION_REQUIRED",
		},
		"If-None-Match naming the current tag, compared weakly": {
			ifNoneMatch: `W/"v1"`, status: http.StatusPreconditionRequired, code: "PRECONDITION_REQUIRED",
		},
		"If-None-Match naming another tag": {
			ifNoneMatch: `"v0"`, status: http.StatusPreconditionRequired, code: "PRECONDITION_REQUIRED",
		},
		"a fencing If-Match beside a non-fencing If-None-Match": {
			ifMatch: `"v1"`, ifNoneMatch: `"v0"`, status: http.StatusOK,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			header := http.Header{}
			if test.ifMatch != "" {
				header.Set("If-Match", test.ifMatch)
			}

			if test.ifNoneMatch != "" {
				header.Set("If-None-Match", test.ifNoneMatch)
			}

			rec := write(t, serverWith(t, seededStore()).Handler(), http.MethodPut,
				resourcesPath+"/storage/local", header, validSpec)

			require.Equal(t, test.status, rec.Code, "body: %s", rec.Body.String())

			if test.code != "" {
				require.Contains(t, rec.Body.String(), test.code)
			}
		})
	}
}

// TestIfMatchIsEvaluatedBeforeIfNoneMatch pins RFC 9110 §13.2.2's order. With
// both headers present and the If-Match stale, the client has to be told its
// token moved on, not that the instance exists.
func TestIfMatchIsEvaluatedBeforeIfNoneMatch(t *testing.T) {
	t.Parallel()

	pre := Precondition{IfMatch: []string{"v0"}, MustNotExist: true}
	require.False(t, pre.Holds(true, "v1"))

	fresh := Precondition{IfMatch: []string{"v1"}, IfNoneMatch: []string{"v1"}}
	require.False(t, fresh.Holds(true, "v1"), "If-Match passes, then If-None-Match refuses")

	require.True(t, Precondition{IfMatch: []string{"v1"}}.Holds(true, "v1"))
	require.False(t, Precondition{IfMatchAny: true}.Holds(false, ""))
}

func TestAWriteQuotingAStaleVersionFailsThePrecondition(t *testing.T) {
	t.Parallel()

	store := seededStore()

	rec := write(t, serverWith(t, store).Handler(), http.MethodPut, resourcesPath+"/storage/local",
		http.Header{"If-Match": {`"v0"`}}, validSpec)

	// 412, not 409: the token rides in If-Match, which RFC 9110 §13.1.1
	// mandates 412 for. 409 is right only for a token in the request body.
	body := requireProblem(t, rec, http.StatusPreconditionFailed)
	require.Equal(t, httpfault.CodeVersionConflict(), body.Code)
	require.Equal(t, "v1", paramString(t, body.Params, "version"),
		"the refusal has to say what the caller should read back")
	require.Equal(t, "v1", store.instances["storage/local"].Version, "a failed precondition must not write")
}

func TestAWriteQuotingTheCurrentVersionApplies(t *testing.T) {
	t.Parallel()

	store := seededStore()

	rec := write(t, serverWith(t, store).Handler(), http.MethodPut, resourcesPath+"/storage/local",
		http.Header{"If-Match": {`"v1"`}},
		`{"type":"local","enabled":false,"required":true,"config":{"root":"/srv"}}`)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	require.Equal(t, `"v2"`, rec.Header().Get("ETag"))
	require.Empty(t, rec.Header().Get("Location"), "a replace is not a create")

	stored := store.instances["storage/local"]
	require.False(t, stored.Enabled)
	require.True(t, stored.Required)
	require.JSONEq(t, `{"root":"/srv"}`, string(stored.Config))
	require.Equal(t, int64(2), stored.Generation)
}

// TestAReadCanBeSentStraightBackToAWrite is the round trip every REST client
// makes: the write must accept the members the read emitted, not 422 on them.
func TestAReadCanBeSentStraightBackToAWrite(t *testing.T) {
	t.Parallel()

	handler := serverWith(t, seededStore()).Handler()

	read := call(t, handler, http.MethodGet, resourcesPath+"/storage/local", "")
	require.Equal(t, http.StatusOK, read.Code)

	rec := write(t, handler, http.MethodPut, resourcesPath+"/storage/local",
		http.Header{"If-Match": {read.Header().Get("ETag")}}, read.Body.String())

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
}

// TestTheBodyMayNotNameADifferentInstance is the half of that strictness which
// must stay: the path names the instance, and a body disagreeing with it is a
// client bug rather than a second way to choose what is written.
func TestTheBodyMayNotNameADifferentInstance(t *testing.T) {
	t.Parallel()

	store := seededStore()

	for name, test := range map[string]struct{ body, location string }{
		"kind": {body: `{"type":"local","enabled":true,"required":false,"config":{},"kind":"sink"}`, location: "body.kind"},
		"name": {body: `{"type":"local","enabled":true,"required":false,"config":{},"name":"other"}`, location: "body.name"},
		"an unpublished member": {
			body:     `{"type":"local","enabled":true,"required":false,"config":{},"privileged":true}`,
			location: "body",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			rec := write(t, serverWith(t, store).Handler(), http.MethodPut, resourcesPath+"/storage/local",
				http.Header{"If-Match": {`"v1"`}}, test.body)

			body := requireProblem(t, rec, http.StatusUnprocessableEntity)
			require.Equal(t, httpfault.CodeRequestInvalid(), body.Code)
			require.Contains(t, paramString(t, body.Params, "location"), test.location)
		})
	}

	require.Equal(t, "v1", store.instances["storage/local"].Version, "a refused body must not write")
}

// TestADuplicateMemberIsRefused covers the member that decides which
// implementation is built. encoding/json resolves it last-wins in silence.
func TestADuplicateMemberIsRefused(t *testing.T) {
	t.Parallel()

	store := seededStore()

	rec := write(t, serverWith(t, store).Handler(), http.MethodPut, resourcesPath+"/storage/local",
		http.Header{"If-Match": {`"v1"`}},
		`{"type":"local","type":"evil","enabled":true,"required":false,"config":{}}`)

	body := requireProblem(t, rec, http.StatusUnprocessableEntity)
	require.Equal(t, httpfault.CodeRequestInvalid(), body.Code)
	require.Equal(t, "local", store.instances["storage/local"].Type, "the second type must not have been written")
}

// TestADryRunWritesNothingAndHandsBackNoToken covers the flag an operator uses
// to find out whether a change would be accepted. A token for a state that was
// never written is a token that fails the next If-Match it is quoted in.
func TestADryRunWritesNothingAndHandsBackNoToken(t *testing.T) {
	t.Parallel()

	store := seededStore()

	rec := write(t, serverWith(t, store).Handler(), http.MethodPut,
		resourcesPath+"/storage/local?dry_run=true", http.Header{"If-Match": {`"v1"`}},
		`{"type":"local","enabled":false,"required":false,"config":{}}`)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	require.Empty(t, rec.Header().Get("ETag"), "a dry run wrote nothing, so there is no version to quote")

	var applied ResourceInstance

	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &applied))
	require.Empty(t, applied.Version, "a version in the body is the same token by another route")

	require.Equal(t, "v1", store.instances["storage/local"].Version, "a dry run must not write")
	require.True(t, store.instances["storage/local"].Enabled, "a dry run must not write")
}

// TestAMisspeltQueryParameterIsRefused is the one that made the dry run a live
// write. dryRun is the obvious typo for dry_run, and an ignored unknown
// parameter turns it into a committed change to the operator's configuration.
func TestAMisspeltQueryParameterIsRefused(t *testing.T) {
	t.Parallel()

	store := seededStore()

	rec := write(t, serverWith(t, store).Handler(), http.MethodPut,
		resourcesPath+"/storage/local?dryRun=true", http.Header{"If-Match": {`"v1"`}}, validSpec)

	body := requireProblem(t, rec, http.StatusUnprocessableEntity)
	require.Equal(t, httpfault.CodeRequestInvalid(), body.Code)
	require.Contains(t, paramString(t, body.Params, "location"), "dryRun")
	require.Equal(t, "v1", store.instances["storage/local"].Version, "the typo must not have written")
}

func TestCreatingRequiresThatNothingIsThere(t *testing.T) {
	t.Parallel()

	spec := `{"type":"s3","enabled":true,"required":false,"config":{"bucket":"prints"}}`

	t.Run("creates when absent, and says where", func(t *testing.T) {
		t.Parallel()

		store := seededStore()

		rec := write(t, serverWith(t, store).Handler(), http.MethodPut, resourcesPath+"/storage/remote",
			http.Header{"If-None-Match": {anyTag}}, spec)

		require.Equal(t, http.StatusCreated, rec.Code, "body: %s", rec.Body.String())
		require.Equal(t, resourcesPath+"/storage/remote", rec.Header().Get("Location"))
		require.NotEmpty(t, rec.Header().Get("ETag"))
		require.Contains(t, store.instances, "storage/remote")
	})

	t.Run("refuses when present", func(t *testing.T) {
		t.Parallel()

		rec := write(t, serverWith(t, seededStore()).Handler(), http.MethodPut, resourcesPath+"/storage/local",
			http.Header{"If-None-Match": {anyTag}}, spec)

		body := requireProblem(t, rec, http.StatusPreconditionFailed)
		require.Equal(t, httpfault.CodeVersionConflict(), body.Code)
	})
}

// TestTypeIsFixedAtCreation pins earlier research: a different implementation
// cannot inherit an instance's identity, and the data rows keyed by its UID
// would follow the wrong type.
func TestTypeIsFixedAtCreation(t *testing.T) {
	t.Parallel()

	store := seededStore()

	rec := write(t, serverWith(t, store).Handler(), http.MethodPut, resourcesPath+"/storage/local",
		http.Header{"If-Match": {`"v1"`}},
		`{"type":"s3","enabled":true,"required":false,"config":{}}`)

	body := requireProblem(t, rec, http.StatusUnprocessableEntity)
	require.Equal(t, httpfault.CodeFieldImmutable(), body.Code)
	require.Equal(t, "body.type", paramString(t, body.Params, "field"))
	require.Equal(t, "local", store.instances["storage/local"].Type)
}

// TestAnInstanceTheOperatorDeclaredIsNotWritable is earlier research: the
// API writes the machine-owned overlay, never the file with the comments in it.
func TestAnInstanceTheOperatorDeclaredIsNotWritable(t *testing.T) {
	t.Parallel()

	store := seededStore()
	store.declareInFile("storage", "handwritten")

	rec := write(t, serverWith(t, store).Handler(), http.MethodPut, resourcesPath+"/storage/handwritten",
		http.Header{"If-Match": {`"v1"`}}, validSpec)

	body := requireProblem(t, rec, http.StatusConflict)
	require.Equal(t, httpfault.CodeResourceNotWritable(), body.Code)
	require.Equal(t, "file", paramString(t, body.Params, "source"))
	require.NotEmpty(t, paramString(t, body.Params, "file"), "the refusal has to name what to edit instead")
}

func TestDeletingRemovesTheDeclaration(t *testing.T) {
	t.Parallel()

	store := seededStore()

	rec := write(t, serverWith(t, store).Handler(), http.MethodDelete, resourcesPath+"/storage/local",
		http.Header{"If-Match": {`"v1"`}}, "")

	require.Equal(t, http.StatusNoContent, rec.Code)
	require.Zero(t, rec.Body.Len())
	require.NotContains(t, store.instances, "storage/local")
}

func TestAMalformedInstanceNameIsRefusedBeforeTheStoreSeesIt(t *testing.T) {
	t.Parallel()

	store := seededStore()

	rec := call(t, serverWith(t, store).Handler(), http.MethodGet, resourcesPath+"/storage/Not_A_Label", "")

	body := requireProblem(t, rec, http.StatusUnprocessableEntity)
	require.Equal(t, httpfault.CodeRequestInvalid(), body.Code)
	require.Equal(t, "path.name", paramString(t, body.Params, "location"))
	require.Zero(t, store.calls, "the store must never be handed a name the rest of the system could not produce")
}

// TestEveryResourceRouteReachesTheStoreAsTheResolvedPrincipal is standing
// suspicion 1. Both mutations that break authorization here are isPublic
// returning true and the middleware substituting a fixed subject. The store
// fake records the principal rather than taking it as _ string, because a fake
// that throws it away leaves every resource test green under both.
func TestEveryResourceRouteReachesTheStoreAsTheResolvedPrincipal(t *testing.T) {
	t.Parallel()

	store := seededStore()
	handler := serverWith(t, store).Handler()

	call(t, handler, http.MethodGet, resourcesPath, "")
	call(t, handler, http.MethodGet, resourcesPath+"/storage/local", "")
	write(t, handler, http.MethodPut, resourcesPath+"/storage/local",
		http.Header{"If-Match": {`"v1"`}}, validSpec)
	write(t, handler, http.MethodDelete, resourcesPath+"/storage/local",
		http.Header{"If-Match": {`"v2"`}}, "")

	for _, method := range []string{"List", "Get", "Apply", "Delete"} {
		require.Equalf(t, []string{"operator"}, store.principalsFor(method),
			"%s was not handed the principal the middleware resolved", method)
	}
}

// TestEveryResourceRouteRefusesACallerWithNoIdentity is the refusal half. A
// permit test without it is half a test.
func TestEveryResourceRouteRefusesACallerWithNoIdentity(t *testing.T) {
	t.Parallel()

	store := seededStore()

	server, err := New(Config{
		Version:    "test",
		Principals: fixedPrincipal(""),
		Resources:  store,
		Presets:    seededPresets(),
		Endpoints:  seededEndpoints(),
		Grants:     &fakeGrantStore{},
		Sessions:   newFakeSessions(),
		Accounts:   &fakeAccounts{},
		Readiness:  &fakeReadiness{report: readyReport()},
		Devices:    noPrinters{},
		Stores:     noStorages{},
		Events:     noEvents{},
	})
	require.NoError(t, err)

	handler := server.Handler()

	for name, request := range map[string]struct {
		method, path, body string
	}{
		"list":   {method: http.MethodGet, path: resourcesPath},
		"get":    {method: http.MethodGet, path: resourcesPath + "/storage/local"},
		"put":    {method: http.MethodPut, path: resourcesPath + "/storage/local", body: validSpec},
		"delete": {method: http.MethodDelete, path: resourcesPath + "/storage/local"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			rec := write(t, handler, request.method, request.path,
				http.Header{"If-Match": {`"v1"`}}, request.body)

			requireProblem(t, rec, http.StatusUnauthorized)
		})
	}

	require.Zero(t, store.calls, "an unidentified caller must not reach the store at all")
}

// TestAbsenceAndNoAccessAreIndistinguishable is this project's third standing
// suspicion held at the transport: a caller who may not see an instance must
// not be able to learn that it exists. The two answers are compared byte for
// byte, because a difference in a header, a status or one field of the problem
// document is the whole leak.
func TestAbsenceAndNoAccessAreIndistinguishable(t *testing.T) {
	t.Parallel()

	store := seededStore()
	store.declareInFile("storage", "secret")
	store.hide("storage", "secret")

	handler := serverWith(t, store).Handler()

	invisible := call(t, handler, http.MethodGet, resourcesPath+"/storage/secret", "")
	absent := call(t, handler, http.MethodGet, resourcesPath+"/storage/nowhere", "")

	require.Equal(t, http.StatusNotFound, invisible.Code)
	require.Equal(t, http.StatusNotFound, absent.Code)
	require.Equal(t, absent.Header().Get("Content-Type"), invisible.Header().Get("Content-Type"))

	// The instance name is the one member that legitimately differs: the caller
	// asked for it, so echoing it tells them nothing they did not send.
	require.Equal(t,
		anonymise(absent.Body.String(), "nowhere"),
		anonymise(invisible.Body.String(), "secret"),
		"the refusal for an instance the caller may not see must read as the refusal for one that is not there")
}

// anonymise removes the two members two otherwise identical refusals differ in:
// the trace id of the request, and the name the caller themselves asked for.
func anonymise(body, name string) string {
	return strings.ReplaceAll(redactTrace(body), name, "<name>")
}

// TestTheConditionProjectionCoversTheKernelType keeps the wire shape and the
// kernel type from drifting. A field added to condition.Condition that this
// transport does not carry is a status a client cannot see.
func TestTheConditionProjectionCoversTheKernelType(t *testing.T) {
	t.Parallel()

	projected := map[string]struct{}{}
	for _, field := range reflect.VisibleFields(reflect.TypeFor[ResourceCondition]()) {
		projected[field.Name] = struct{}{}
	}

	for _, field := range reflect.VisibleFields(reflect.TypeFor[condition.Condition]()) {
		require.Containsf(t, projected, field.Name,
			"condition.Condition.%s is not carried by ResourceCondition, so no client can see it", field.Name)
	}
}

// TestTheKindEnumMatchesTheClosedSet keeps the published enum and ref.Kinds in
// agreement, since the enum is a literal in a struct tag. Both copies are
// read, because nothing else watches the filter's.
func TestTheKindEnumMatchesTheClosedSet(t *testing.T) {
	t.Parallel()

	for name, field := range map[string]reflect.StructField{
		"path":   fieldOf(t, reflect.TypeFor[ResourceRef](), "Kind"),
		"filter": fieldOf(t, reflect.TypeFor[listResourcesInput](), "Kind"),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			tag, ok := field.Tag.Lookup("enum")
			require.True(t, ok)
			require.Equal(t, resourceKinds(), tag)
		})
	}
}

func fieldOf(t *testing.T, typ reflect.Type, name string) reflect.StructField {
	t.Helper()

	field, ok := typ.FieldByName(name)
	require.True(t, ok)

	return field
}

// TestTheStateEnumMatchesTheDerivedStates does the same for the states the
// supervision plane can derive.
func TestTheStateEnumMatchesTheDerivedStates(t *testing.T) {
	t.Parallel()

	field, ok := reflect.TypeFor[ResourceStatus]().FieldByName("State")
	require.True(t, ok)

	published := strings.Split(field.Tag.Get("enum"), ",")

	for _, state := range []condition.State{
		condition.StateDisabled(), condition.StateOrphaned(), condition.StatePending(),
		condition.StateStarting(), condition.StateReady(), condition.StateDegraded(),
		condition.StateRetrying(), condition.StateFailed(), condition.StateStopping(),
		condition.StateStopped(),
	} {
		require.Containsf(t, published, state.String(),
			"the derived state %q is not in the published enum, so a client would reject a valid response",
			state.String())
	}

	require.Len(t, published, 10, "the enum names a state the supervision plane cannot derive")
}

// fakeResourceStore is an in-memory stand-in for the merged configuration. It
// implements the fencing, immutability and ownership rules so the transport's
// half of them can be exercised; the real store holds them against two files
// and a person with an editor.
//
// It records the principal it was handed on every call, because the transport's
// job is to pass the resolved one and nothing else, and a fake that discards it
// is a fake no authorization mutation can fail against.
type fakeResourceStore struct {
	mu         sync.Mutex
	instances  map[string]ResourceInstance
	version    string
	generation int64
	calls      int
	principals map[string][]string
	// hidden are instances that exist and that this caller may not see. A store
	// with an authorization graph behind it answers for them exactly as it does
	// for an instance that is not there.
	hidden map[string]struct{}
	// restarted is every instance a restart reached, in order.
	restarted []string
}

func seededStore() *fakeResourceStore {
	store := &fakeResourceStore{
		instances:  map[string]ResourceInstance{},
		version:    "v1",
		generation: 1,
		principals: map[string][]string{},
	}

	store.instances["storage/local"] = ResourceInstance{
		ResourceSpec: ResourceSpec{
			Type:    "local",
			Enabled: true,
			Config:  ResourceConfig(`{"root":"/var/lib/prusa"}`),
		},
		Kind:       "storage",
		Name:       "local",
		UID:        "01J0STORAGELOCAL",
		Source:     "overlay",
		Version:    store.version,
		Generation: 1,
		Status: ResourceStatus{
			State:              condition.StateReady().String(),
			Serving:            true,
			ObservedGeneration: 1,
			Conditions: []ResourceCondition{
				NewResourceCondition(condition.Condition{
					Type:               condition.TypeStarted,
					Status:             condition.StatusTrue,
					Reason:             "Started",
					ObservedGeneration: 1,
				}),
			},
		},
	}

	return store
}

// declareInFile seeds an instance the operator wrote by hand, which the API
// reports and refuses to write.
func (s *fakeResourceStore) declareInFile(kind, name string) {
	s.instances[kind+"/"+name] = ResourceInstance{
		ResourceSpec: ResourceSpec{Type: "local", Enabled: true},
		Kind:         kind,
		Name:         name,
		UID:          "01J0HANDWRITTEN",
		Source:       "file",
		Version:      s.version,
		Generation:   1,
	}
}

func (s *fakeResourceStore) principalsFor(method string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.principals[method]
}

func (s *fakeResourceStore) enter(method, principal string) {
	s.calls++

	if !slices.Contains(s.principals[method], principal) {
		s.principals[method] = append(s.principals[method], principal)
	}
}

func (s *fakeResourceStore) List(_ context.Context, principal string, kind ref.Kind) (ResourceListing, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.enter("List", principal)

	out := make([]ResourceInstance, 0, len(s.instances))

	for _, instance := range s.instances {
		if kind == "" || instance.Kind == kind.String() {
			out = append(out, instance)
		}
	}

	return ResourceListing{Items: out, Version: s.version}, nil
}

func (s *fakeResourceStore) Get(_ context.Context, principal string, instance ref.Ref) (ResourceInstance, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.enter("Get", principal)

	_, invisible := s.hidden[instance.Path()]

	found, ok := s.instances[instance.Path()]
	if !ok || invisible {
		return ResourceInstance{}, httpfault.ResourceNotFound(instance.String())
	}

	return found, nil
}

// hide makes an existing instance one this principal may not see. It is the
// answer a store with an authorization graph behind it would give, and the
// refusal has to be the one it gives for an instance that is not there.
func (s *fakeResourceStore) hide(kind, name string) {
	if s.hidden == nil {
		s.hidden = map[string]struct{}{}
	}

	s.hidden[kind+"/"+name] = struct{}{}
}

func (s *fakeResourceStore) Apply(
	_ context.Context, principal string, instance ref.Ref, desired ResourceSpec, pre Precondition,
) (ApplyResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.enter("Apply", principal)

	current, exists := s.instances[instance.Path()]

	if err := s.checkWritable(instance, current, exists, desired, pre); err != nil {
		return ApplyResult{}, err
	}

	next := ResourceInstance{
		ResourceSpec: desired,
		Kind:         instance.Kind().String(),
		Name:         instance.ID(),
		UID:          current.UID,
		Source:       "overlay",
		Version:      s.nextVersion(),
		Generation:   current.Generation + 1,
		Status:       current.Status,
	}

	if next.UID == "" {
		next.UID = "01J0" + strings.ToUpper(instance.ID())
	}

	if pre.DryRun {
		return ApplyResult{Instance: next, Created: !exists}, nil
	}

	s.commit()
	next.Version = s.version
	s.instances[instance.Path()] = next

	return ApplyResult{Instance: next, Created: !exists}, nil
}

func (s *fakeResourceStore) Delete(_ context.Context, principal string, instance ref.Ref, pre Precondition) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.enter("Delete", principal)

	current, exists := s.instances[instance.Path()]

	if err := s.checkWritable(instance, current, exists, current.ResourceSpec, pre); err != nil {
		return err
	}

	if !pre.DryRun {
		s.commit()
		delete(s.instances, instance.Path())
	}

	return nil
}

// Restart answers the way the real store does: an instance this caller may not
// see reads as absent, one that is declared and switched off has nothing to
// restart, and anything else is recorded and accepted.
func (s *fakeResourceStore) Restart(_ context.Context, principal string, instance ref.Ref) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.enter("Restart", principal)

	_, invisible := s.hidden[instance.Path()]

	found, ok := s.instances[instance.Path()]
	if !ok || invisible {
		return httpfault.ResourceNotFound(instance.String())
	}

	if !found.Enabled {
		return httpfault.ResourceNotRunning(instance.String())
	}

	s.restarted = append(s.restarted, instance.Path())

	return nil
}

func (s *fakeResourceStore) MemoryHistory(
	_ context.Context, _ string, instance ref.Ref, _ time.Time,
) (ResourceMemoryHistory, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, invisible := s.hidden[instance.Path()]; invisible {
		return ResourceMemoryHistory{}, httpfault.ResourceNotFound(instance.String())
	}

	value := int64(1 << 20)

	return ResourceMemoryHistory{
		Items: []MemoryPoint{{At: time.Unix(1, 0).UTC(), Value: &value}, {At: time.Unix(3, 0).UTC()}},
		Stops: []ResourceStop{},
	}, nil
}

func (s *fakeResourceStore) Log(_ context.Context, _ string, instance ref.Ref) (ResourceLog, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, invisible := s.hidden[instance.Path()]; invisible {
		return ResourceLog{}, httpfault.ResourceNotFound(instance.String())
	}

	return ResourceLog{Lines: []ResourceLogLine{{At: time.Unix(1, 0).UTC(), Text: "boom"}}}, nil
}

func (s *fakeResourceStore) checkWritable(
	instance ref.Ref, current ResourceInstance, exists bool, desired ResourceSpec, pre Precondition,
) error {
	switch {
	case exists && current.Source == "file":
		return httpfault.ResourceNotWritable(
			instance.String(), current.Source, "config/config.yaml")
	case !exists && !pre.MustNotExist && len(pre.IfNoneMatch) == 0:
		return httpfault.ResourceNotFound(instance.String())
	case !pre.Holds(exists, current.Version):
		return httpfault.VersionConflict(instance.String(), current.Version)
	case exists && desired.Type != "" && desired.Type != current.Type:
		return httpfault.FieldImmutable("body.type")
	default:
		return nil
	}
}

// nextVersion is what the overlay's token would be after this write. The token
// covers the file, so it is the store's and not the instance's.
func (s *fakeResourceStore) nextVersion() string {
	return fmt.Sprintf("v%d", s.generation+1)
}

func (s *fakeResourceStore) commit() {
	s.generation++
	s.version = fmt.Sprintf("v%d", s.generation)
}

// serverWith is the server the committed OpenAPI document is generated from, so
// every optional dependency is wired here. A nil one registers no operation,
// and the document would publish a smaller surface than the binary serves.
func serverWith(t *testing.T, store ResourceStore) *Server {
	t.Helper()

	server, err := New(Config{
		Version:    "test",
		InstanceID: testInstanceID,
		Principals: fixedPrincipal("operator"),
		Resources:  store,
		Presets:    seededPresets(),
		Endpoints:  seededEndpoints(),
		Grants:     &fakeGrantStore{},
		BetaPortal: &fakeBetaPortal{},
		Access:     &fakeAccessReview{},
		Sessions:   newFakeSessions(),
		Accounts:   &fakeAccounts{},
		Readiness:  &fakeReadiness{report: readyReport()},
		Devices:    noPrinters{},
		Cameras:    &stubCameras{},
		Stores:     noStorages{},
		Events:     noEvents{},

		CameraSettings: &stubCameraSettings{},
	})
	require.NoError(t, err)

	return server
}

// serverWithSessions is a server whose login ceremony is the caller's, so a
// test can drive the callback without an issuer.
func serverWithSessions(t *testing.T, sessions Sessions, insecureCookies bool) *Server {
	t.Helper()

	server, err := New(Config{
		Version:         "test",
		Principals:      fixedPrincipal("user:operator"),
		Resources:       seededStore(),
		Presets:         seededPresets(),
		Endpoints:       seededEndpoints(),
		Grants:          &fakeGrantStore{},
		Sessions:        sessions,
		Accounts:        &fakeAccounts{},
		Readiness:       &fakeReadiness{report: readyReport()},
		Devices:         noPrinters{},
		Stores:          noStorages{},
		Events:          noEvents{},
		InsecureCookies: insecureCookies,
	})
	require.NoError(t, err)

	return server
}

// serverWithReadiness is a server whose verdict is the caller's.
func serverWithReadiness(t *testing.T, probes Readiness) *Server {
	t.Helper()

	server, err := New(Config{
		Version:    "test",
		Principals: fixedPrincipal("user:operator"),
		Resources:  seededStore(),
		Presets:    seededPresets(),
		Endpoints:  seededEndpoints(),
		Grants:     &fakeGrantStore{},
		Sessions:   newFakeSessions(),
		Accounts:   &fakeAccounts{},
		Readiness:  probes,
		Devices:    noPrinters{},
		Stores:     noStorages{},
		Events:     noEvents{},
	})
	require.NoError(t, err)

	return server
}

func write(
	t *testing.T,
	handler http.Handler,
	method, path string,
	header http.Header,
	body string,
) *httptest.ResponseRecorder {
	t.Helper()

	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, path, nil)
	} else {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	}

	for key, values := range header {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	return rec
}

// The restart operation is the resource surface's operate_resource half, and
// this is the transport's share of it: the path exists, the store is handed the
// resolved principal, and the answer carries no body.
func TestRestartingAResourceReachesTheStoreAndAnswersNoContent(t *testing.T) {
	t.Parallel()

	store := seededStore()

	rec := call(t, serverWith(t, store).Handler(), http.MethodPost, resourcesPath+"/storage/local/restart", "")

	require.Equal(t, http.StatusNoContent, rec.Code, "body: %s", rec.Body.String())
	require.Empty(t, rec.Body.String(), "a 204 carries a body")
	require.Equal(t, []string{"storage/local"}, store.restarted)
	require.NotEmpty(t, store.principalsFor("Restart"),
		"the transport called the store without the resolved principal")
}

// An instance this caller may not see refuses as one that is not there, on this
// operation as on every other one here.
func TestRestartingAResourceTheCallerMayNotSeeIsTheAbsentAnswer(t *testing.T) {
	t.Parallel()

	store := seededStore()
	store.hide("storage", "local")

	rec := call(t, serverWith(t, store).Handler(), http.MethodPost, resourcesPath+"/storage/local/restart", "")

	body := requireProblem(t, rec, http.StatusNotFound)
	require.Equal(t, httpfault.CodeResourceNotFound(), body.Code)
	require.Empty(t, store.restarted, "the store restarted an instance for a caller it refused")
}

// A declared instance the plane is not running answers 409 rather than 404. It
// is readable, so hiding it would be a second answer to a question the caller
// can already ask, and the answer they need is that starting it is a write.
func TestRestartingAnInstanceNothingSupervisesIsAConflict(t *testing.T) {
	t.Parallel()

	store := seededStore()

	off := store.instances["storage/local"]
	off.Enabled = false
	store.instances["storage/local"] = off

	rec := call(t, serverWith(t, store).Handler(), http.MethodPost, resourcesPath+"/storage/local/restart", "")

	body := requireProblem(t, rec, http.StatusConflict)
	require.Equal(t, httpfault.CodeResourceNotRunning(), body.Code)
}

// A kind outside the closed set is refused at the path, before anything reads
// the configuration, exactly as it is on the read and the write.
func TestRestartingAnUnknownKindIsRefusedAtThePath(t *testing.T) {
	t.Parallel()

	store := seededStore()

	rec := call(t, serverWith(t, store).Handler(), http.MethodPost, resourcesPath+"/teapot/local/restart", "")

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, "body: %s", rec.Body.String())
	require.Empty(t, store.restarted)
}

// TestAModuleRowCarriesWhatItsManifestSaid is the old backlog on the wire. A card
// in the extensions screen shows the release, one line about the module and who
// answers for it, and the three are on the row beside the ui object rather than
// behind a second request.
//
// The release is under module rather than at the top level because version
// there is the fencing token a write quotes in If-Match.
func TestAModuleRowCarriesWhatItsManifestSaid(t *testing.T) {
	t.Parallel()

	store := seededStore()
	store.instances["asset/spools"] = exampleAssetResource()

	rec := call(t, serverWith(t, store).Handler(), http.MethodGet, resourcesPath+"/asset/spools", "")
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	var row ResourceInstance
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &row))
	require.NotNil(t, row.Module, "the row carries nothing a card could print")
	require.Equal(t, "1.4.0", row.Module.Version)
	require.Equal(t, "Counts what each print used and warns before a spool runs out.", row.Module.Summary)
	require.Equal(t, "acme", row.Module.Author)
	require.NotEqual(t, row.Module.Version, row.Version,
		"the release and the fencing token are two things and must not read as one")
}

// exampleSpoolsType is the type the bundled example module installs under: the
// namespace derived from its module path, a slash, and the name its manifest
// carries. It is 65 bytes, which is what made it the defect below.
const exampleSpoolsType = "com.github.aux.prusa3d.prusa-connect.examples.spools/spools"

// TestAQualifiedTypeLongerThanOneLabelIsAccepted is the defect the extensions
// page's enable switch hit. A module's type is a namespace and a name, the
// bundled example installs under one of 65 bytes, and the body was bounded at
// 63, which is the DNS label limit of one half applied to the whole. Every
// module the install path wrote was then a row a client could read and never
// write back.
func TestAQualifiedTypeLongerThanOneLabelIsAccepted(t *testing.T) {
	t.Parallel()

	for name, typeName := range map[string]string{
		"the bundled example, as its install writes it": exampleSpoolsType,
		"64 bytes, one over the label limit": strings.Repeat("a", 57) +
			extension.QualifiedSeparator + strings.Repeat("b", 6),
		"the longest name the grammar can derive": strings.Repeat("n", extension.MaxNamespaceLen) +
			extension.QualifiedSeparator + strings.Repeat("t", extension.MaxNameLen),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			store := seededStore()

			rec := write(t, serverWith(t, store).Handler(), http.MethodPut, resourcesPath+"/sink/spools",
				http.Header{"If-None-Match": {anyTag}},
				fmt.Sprintf(`{"type":%q,"enabled":true,"required":false,"config":{}}`, typeName))

			require.Equal(t, http.StatusCreated, rec.Code, "body: %s", rec.Body.String())
			require.Equal(t, typeName, store.instances["sink/spools"].Type)
		})
	}
}

// TestATypeLongerThanTheGrammarIsStillRefused is the other half. Raising the
// bound to the grammar's must not raise it to no bound at all: the body is the
// first thing that sees a type, and an unbounded one reaches the file.
func TestATypeLongerThanTheGrammarIsStillRefused(t *testing.T) {
	t.Parallel()

	store := seededStore()

	tooLong := strings.Repeat("n", MaxResourceTypeBytes+1)

	rec := write(t, serverWith(t, store).Handler(), http.MethodPut, resourcesPath+"/sink/spools",
		http.Header{"If-None-Match": {anyTag}},
		fmt.Sprintf(`{"type":%q,"enabled":true,"required":false,"config":{}}`, tooLong))

	requireProblem(t, rec, http.StatusUnprocessableEntity)
	require.NotContains(t, store.instances, "sink/spools")
}

// TestThePublishedTypeBoundIsTheQualifiedNameGrammar holds the number in the
// struct tag to the grammar it comes from. A tag cannot spell a constant, so
// nothing else keeps the published bound and the names the install path
// composes in agreement.
func TestThePublishedTypeBoundIsTheQualifiedNameGrammar(t *testing.T) {
	t.Parallel()

	field := fieldOf(t, reflect.TypeFor[ResourceSpec](), "Type")

	require.Equal(t, strconv.Itoa(MaxResourceTypeBytes), field.Tag.Get("maxLength"))
	require.Greater(t, MaxResourceTypeBytes, extension.MaxNameLen,
		"a qualified name is a namespace and a name, so one label's limit cannot bound the whole")
	require.GreaterOrEqual(t, MaxResourceTypeBytes, len(exampleSpoolsType),
		"the bundled example installs under a type this API would refuse")
}

// TestAnInstanceWithNoSettingsRoundTripsThroughListAndPut is the round trip the
// extensions page makes: read the row, flip enabled, put the same body back.
// The listing answered config: null for an instance that declares none, and the
// member is published as a required object, so the write refused a body the
// client had not touched.
func TestAnInstanceWithNoSettingsRoundTripsThroughListAndPut(t *testing.T) {
	t.Parallel()

	store := seededStore()

	bare := store.instances["storage/local"]
	bare.Name = "bare"
	bare.Config = nil
	store.instances["storage/bare"] = bare

	handler := serverWith(t, store).Handler()

	listed := call(t, handler, http.MethodGet, resourcesPath, "")
	require.Equal(t, http.StatusOK, listed.Code)

	var listing struct {
		Items []json.RawMessage `json:"items"`
	}

	require.NoError(t, json.Unmarshal(listed.Body.Bytes(), &listing))

	row := rowNamed(t, listing.Items, "bare")
	require.JSONEq(t, `{}`, string(row["config"]),
		"a member published as an object may not be answered as null")

	row["enabled"] = json.RawMessage(`false`)

	body, err := json.Marshal(row)
	require.NoError(t, err)

	rec := write(t, handler, http.MethodPut, resourcesPath+"/storage/bare",
		http.Header{"If-Match": {listed.Header().Get("ETag")}}, string(body))

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	require.False(t, store.instances["storage/bare"].Enabled)
}

// rowNamed returns the listed instance with this name, as its members.
func rowNamed(t *testing.T, items []json.RawMessage, name string) map[string]json.RawMessage {
	t.Helper()

	for _, item := range items {
		row := map[string]json.RawMessage{}
		require.NoError(t, json.Unmarshal(item, &row))

		if string(row["name"]) == `"`+name+`"` {
			return row
		}
	}

	require.FailNowf(t, "no such instance", "the listing carries no instance named %q", name)

	return nil
}

// TestAnInstanceThatIsNoModuleCarriesNoModuleBlock keeps the key absent where
// there is nothing to say, so a page tells a module from a storage by reading
// the row rather than by guessing.
func TestAnInstanceThatIsNoModuleCarriesNoModuleBlock(t *testing.T) {
	t.Parallel()

	rec := call(t, serverWith(t, seededStore()).Handler(),
		http.MethodGet, resourcesPath+"/storage/local", "")
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	require.NotContains(t, rec.Body.String(), `"module"`)
}

func (s *fakeResourceStore) ModuleMemory() ModuleMemoryPool {
	return ModuleMemoryPool{PoolBytes: new(int64(1 << 30)), CommittedBytes: 3 << 27, Source: "configured"}
}

// A reading the host could not take is a point without a value on the wire, and
// the log of an instance the caller may not see is the absent answer.
func TestTheMemoryHistoryAndTheLogAreServedUnderTheInstance(t *testing.T) {
	t.Parallel()

	store := seededStore()
	handler := serverWith(t, store).Handler()

	rec := call(t, handler, http.MethodGet, resourcesPath+"/storage/local/memory", "")
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	require.JSONEq(t,
		`{"items":[{"at":"1970-01-01T00:00:01Z","value":1048576},{"at":"1970-01-01T00:00:03Z"}],"stops":[]}`,
		rec.Body.String())

	rec = call(t, handler, http.MethodGet, resourcesPath+"/storage/local/log", "")
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	require.JSONEq(t, `{"lines":[{"at":"1970-01-01T00:00:01Z","text":"boom"}]}`, rec.Body.String())

	store.hide("storage", "local")

	requireProblem(t, call(t, handler, http.MethodGet, resourcesPath+"/storage/local/log", ""), http.StatusNotFound)
	requireProblem(t, call(t, handler, http.MethodGet, resourcesPath+"/storage/local/memory", ""), http.StatusNotFound)
}
