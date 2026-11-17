package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpfault"
	"github.com/prusa3d/prusa-connect/sdk/extension"
)

// The addresses one module's settings are kept at. They are literal segments
// under the namespace, so chi prefers them to the module's own catch-all and a
// module cannot serve its own answer at them.
//
// The family is under the module's mount and not under /api/v1/state, so
// the permission is decided on the module's instance the way every other path
// under the mount is, and a namespace this deployment does not run answers as
// an unrouted path.
const (
	moduleStatePath    = modulesPathPrefix + "/{" + namespaceParam + "}/state"
	moduleStateKeyPath = moduleStatePath + "/{" + stateKeyParam + "}"
)

// stateKeyParam is the segment naming one record.
const stateKeyParam = "key"

// maxStateKeyLength is the longest key this surface accepts. It is one
// DNS-1123 label, the same grammar as a page path and a contributed id, so a
// module author writes one kind of name.
const maxStateKeyLength = 63

// defaultMaxStateValueBytes is what one record holds where a deployment names
// no bound. It is four kilobytes: a settings pane keeps a few choices, and a
// document that does not fit is a file and not a setting.
//
// sdk/stateapi.MaxValueBytes is the hard ceiling behind it at 64 KiB, and this
// bound is the transport's own, so raising the key raises what a browser may
// write without raising what a module's own process may.
const defaultMaxStateValueBytes int64 = 4 << 10

// ErrNoStateRecord reports a key the namespace does not hold. A store returns
// it, and this transport answers 404.
var ErrNoStateRecord = errors.New("httpapi: the namespace holds no record under that key")

// ModuleStateStore is one module's settings as this transport reads and writes
// them: per namespace, per signed-in principal, and opaque.
//
// It is separate from StateStore because the two answer different questions.
// StateStore publishes the accounting an operator may read and returns no
// value at all. This one returns a value, and only the person who wrote it may
// read it back.
type ModuleStateStore interface {
	// Keys answers every key this principal holds under the namespace, sorted.
	Keys(ctx context.Context, namespace, principal string) ([]string, error)
	// Value answers the record this principal holds under key, and
	// ErrNoStateRecord when there is none.
	Value(ctx context.Context, namespace, principal, key string) ([]byte, error)
	// Write stores value under key for this principal, whatever is there.
	Write(ctx context.Context, namespace, principal, key string, value []byte) error
	// Remove deletes the record, and answers ErrNoStateRecord when there is
	// none.
	Remove(ctx context.Context, namespace, principal, key string) error
}

// ModuleStateResolver answers the settings store this request reads, per call.
// It is a resolver for the reason StateResolver is: the backend behind
// sdk/stateapi is a supervised instance, and the plane that supervises it is
// built after the transport.
type ModuleStateResolver func() (ModuleStateStore, error)

// resolveModuleState is the settings store this request reads, and nil for a
// deployment running no record backend.
func resolveModuleState(cfg Config) (ModuleStateStore, error) {
	if cfg.ModuleState == nil {
		return nil, nil //nolint:nilnil // no store and no failure is the shape the caller branches on.
	}

	return cfg.ModuleState()
}

// StateValue is one stored record on the wire: a JSON document this server
// keeps whole and reads no member of.
//
// It is raw JSON and not a string, so a pane storing an object stores an
// object. It has no declared schema because the shape belongs to the module.
// The core promises that the bytes come back as they went in.
type StateValue []byte

// MarshalJSON writes the document through unchanged. A zero value is null,
// which is the record of a key holding nothing.
func (v StateValue) MarshalJSON() ([]byte, error) {
	if len(v) == 0 {
		return []byte("null"), nil
	}

	return v, nil
}

// UnmarshalJSON keeps the bytes the caller sent, so the value stored is the
// value written and not a re-encoding of it.
func (v *StateValue) UnmarshalJSON(data []byte) error {
	*v = append((*v)[:0], data...)

	return nil
}

// Schema describes the value as any JSON document, because the core reads no
// member of it.
func (StateValue) Schema(huma.Registry) *huma.Schema {
	return &huma.Schema{
		Description: "The stored document. Any JSON is accepted and the same bytes are returned, " +
			"because the shape belongs to the module and this server reads no member of it.",
	}
}

// ModuleStateRecord is one key and what it holds.
type ModuleStateRecord struct {
	Key   string     `json:"key"   doc:"The key the record is stored under."`
	Value StateValue `json:"value" doc:"The stored document."`
}

type listModuleStateInput struct {
	Namespace string `path:"namespace" maxLength:"253" doc:"The module's namespace, derived from its type." example:"com.github.aux.acme.planner"`
}

type listModuleStateOutput struct {
	Body struct {
		Items []string `json:"items" doc:"The keys the signed-in person holds under this module, sorted."`
	}
}

type getModuleStateInput struct {
	Namespace string `path:"namespace" maxLength:"253" doc:"The module's namespace, derived from its type."                              example:"com.github.aux.acme.planner"`
	Key       string `path:"key"       maxLength:"63"  doc:"The record's key: one label of lowercase letters, digits and inner hyphens." example:"units"`
}

type getModuleStateOutput struct {
	Body ModuleStateRecord
}

type putModuleStateInput struct {
	Namespace string `path:"namespace" maxLength:"253" doc:"The module's namespace, derived from its type."                              example:"com.github.aux.acme.planner"`
	Key       string `path:"key"       maxLength:"63"  doc:"The record's key: one label of lowercase letters, digits and inner hyphens." example:"units"`

	Body moduleStateBody
}

// moduleStateBody is what a write sends: the document and nothing else. The key
// is in the path, so a body that named one would be a second way to choose
// where the write lands.
type moduleStateBody struct {
	Value StateValue `json:"value" doc:"The document to store."`
}

type putModuleStateOutput struct {
	Body ModuleStateRecord
}

type deleteModuleStateInput struct {
	Namespace string `path:"namespace" maxLength:"253" doc:"The module's namespace, derived from its type."                              example:"com.github.aux.acme.planner"`
	Key       string `path:"key"       maxLength:"63"  doc:"The record's key: one label of lowercase letters, digits and inner hyphens." example:"units"`
}

type deleteModuleStateOutput struct {
	Status int
}

// moduleStateErrors are the refusals every operation on this surface answers
// with. 501 is the deployment running no record backend, and 404 covers both a
// namespace no running module claims and a key nothing is stored under.
var moduleStateErrors = []int{
	http.StatusUnauthorized,
	http.StatusForbidden,
	http.StatusNotFound,
	http.StatusNotImplemented,
}

// The sentence every operation here repeats about where a record lives, so the
// four descriptions agree.
const moduleStateScope = "A record is kept per module namespace and per signed-in person, so one " +
	"person's settings for a module are theirs and another's are not visible here. It survives a " +
	"restart and an upgrade of the module.\n\n" +
	"A namespace no running module claims answers 404, the same as an unrouted path, so a caller " +
	"cannot read which modules a deployment runs from this surface."

// registerModuleState publishes the four operations a module's settings pane
// reads and writes through.
//
// They are registered whatever a deployment wired, like every other operation
// under the module group: the document is a contract, and a build running no
// record backend answers the same addresses with STATE_NOT_CONFIGURED.
func registerModuleState(api huma.API, cfg Config) {
	registerListModuleState(api, cfg)
	registerGetModuleState(api, cfg)
	registerPutModuleState(api, cfg)
	registerDeleteModuleState(api, cfg)
}

func registerListModuleState(api huma.API, cfg Config) {
	register(api, huma.Operation{
		OperationID: "list-module-state",
		Method:      http.MethodGet,
		Path:        moduleStatePath,
		Summary:     "The keys a module holds for the caller",
		Description: "Every key the signed-in person has stored under this module, sorted, and no " +
			"value. Read one value at a time from the key path below.\n\n" +
			"It takes view_resource on the module's instance, the same permission as reading the " +
			"module's own surface.\n\n" + moduleStateScope,
		Tags:   []string{"resources"},
		Errors: moduleStateErrors,
	}, func(ctx context.Context, in *listModuleStateInput) (*listModuleStateOutput, error) {
		store, who, err := openModuleState(
			ctx, cfg, http.MethodGet, in.Namespace, authz.ActionViewResource)
		if err != nil {
			return nil, err
		}

		keys, err := store.Keys(ctx, in.Namespace, who)
		if err != nil {
			return nil, stateFailure(err, http.MethodGet, in.Namespace, "", "list a module's records")
		}

		out := &listModuleStateOutput{}
		out.Body.Items = keys

		if out.Body.Items == nil {
			out.Body.Items = []string{}
		}

		return out, nil
	})
}

func registerGetModuleState(api huma.API, cfg Config) {
	register(api, huma.Operation{
		OperationID: "get-module-state",
		Method:      http.MethodGet,
		Path:        moduleStateKeyPath,
		Summary:     "One record a module holds for the caller",
		Description: "The document stored under this key, returned as it was written. This server " +
			"reads no member of it.\n\n" +
			"It takes view_resource on the module's instance. A key nothing is stored under answers " +
			"404.\n\n" + moduleStateScope,
		Tags:   []string{"resources"},
		Errors: moduleStateErrors,
	}, func(ctx context.Context, in *getModuleStateInput) (*getModuleStateOutput, error) {
		if err := validStateKey(in.Key); err != nil {
			return nil, err
		}

		store, who, err := openModuleState(
			ctx, cfg, http.MethodGet, in.Namespace, authz.ActionViewResource)
		if err != nil {
			return nil, err
		}

		value, err := store.Value(ctx, in.Namespace, who, in.Key)
		if err != nil {
			return nil, stateFailure(err, http.MethodGet, in.Namespace, in.Key, "read a module's record")
		}

		return &getModuleStateOutput{Body: ModuleStateRecord{Key: in.Key, Value: value}}, nil
	})
}

func registerPutModuleState(api huma.API, cfg Config) {
	register(api, huma.Operation{
		OperationID: "put-module-state",
		Method:      http.MethodPut,
		Path:        moduleStateKeyPath,
		Summary:     "Store one record for the caller",
		Description: "Writes the document under this key, replacing whatever was there. The value is " +
			"any JSON and this server keeps it whole.\n\n" +
			"It takes configure_resource on the module's instance, because a write changes what the " +
			"module shows the person the next time they open it. A value over the deployment's " +
			"bound answers 413 core.validation/STATE_VALUE_TOO_LARGE, which names the bound in " +
			"limit_bytes, so a settings pane can say which setting was refused.\n\n" + moduleStateScope,
		Tags:   []string{"resources"},
		Errors: append([]int{http.StatusRequestEntityTooLarge}, moduleStateErrors...),
	}, func(ctx context.Context, in *putModuleStateInput) (*putModuleStateOutput, error) {
		if err := validStateKey(in.Key); err != nil {
			return nil, err
		}

		// Before the store is opened and before the permission is asked: the
		// bound is on what this transport accepts, so a caller who may write
		// learns the same thing about the value as one who may not.
		if int64(len(in.Body.Value)) > cfg.MaxStateValueBytes {
			return nil, httpfault.StateValueTooLarge(cfg.MaxStateValueBytes)
		}

		store, who, err := openModuleState(
			ctx, cfg, http.MethodPut, in.Namespace, authz.ActionConfigureResource)
		if err != nil {
			return nil, err
		}

		value := []byte(in.Body.Value)
		if len(value) == 0 {
			value = []byte("null")
		}

		if err = store.Write(ctx, in.Namespace, who, in.Key, value); err != nil {
			return nil, stateFailure(err, http.MethodPut, in.Namespace, in.Key, "write a module's record")
		}

		return &putModuleStateOutput{Body: ModuleStateRecord{Key: in.Key, Value: value}}, nil
	})
}

func registerDeleteModuleState(api huma.API, cfg Config) {
	register(api, huma.Operation{
		OperationID: "delete-module-state",
		Method:      http.MethodDelete,
		Path:        moduleStateKeyPath,
		Summary:     "Remove one record of the caller's",
		Description: "Removes the document stored under this key. It takes configure_resource on the " +
			"module's instance, the same permission as writing one. A key nothing is stored under " +
			"answers 404.\n\n" + moduleStateScope,
		Tags:          []string{"resources"},
		DefaultStatus: http.StatusNoContent,
		Errors:        moduleStateErrors,
	}, func(ctx context.Context, in *deleteModuleStateInput) (*deleteModuleStateOutput, error) {
		if err := validStateKey(in.Key); err != nil {
			return nil, err
		}

		store, who, err := openModuleState(
			ctx, cfg, http.MethodDelete, in.Namespace, authz.ActionConfigureResource)
		if err != nil {
			return nil, err
		}

		if err = store.Remove(ctx, in.Namespace, who, in.Key); err != nil {
			return nil, stateFailure(
				err, http.MethodDelete, in.Namespace, in.Key, "remove a module's record")
		}

		return &deleteModuleStateOutput{Status: http.StatusNoContent}, nil
	})
}

// openModuleState resolves the namespace, the caller and the permission, and
// answers the store beside the principal a record is kept under.
//
// The order is the one moduleHandler uses, and for the same reason. A namespace
// no module claims is an unrouted path rather than a refusal naming a module,
// so this surface never says which modules a deployment runs. Identity and the
// permission come next, and the store is opened last.
func openModuleState(
	ctx context.Context, cfg Config, method, namespace string, action authz.Action,
) (ModuleStateStore, string, error) {
	absent := httpfault.RouteNotFound(method, ModuleMount(namespace)+moduleStateSegment)

	if extension.Namespace(namespace).Validate() != nil || cfg.Modules == nil || cfg.ModuleAccess == nil {
		return nil, "", absent
	}

	surface, claimed := cfg.Modules.Module(namespace)
	if !claimed || surface.Instance.IsZero() {
		return nil, "", absent
	}

	who, err := caller(ctx)
	if err != nil {
		return nil, "", err
	}

	if err = cfg.ModuleAccess.RequireResource(ctx, who, action, surface.Instance); err != nil {
		return nil, "", moduleFault(err)
	}

	// The principal is rendered from the parsed subject rather than taken from
	// the header, so the string a record is keyed by is the one the graph
	// decided the permission against.
	principal := who.String()
	if strings.Contains(principal, recordKeySeparator) {
		return nil, "", httpfault.Unexpected("key a record by this caller")
	}

	store, err := resolveModuleState(cfg)
	if err != nil {
		return nil, "", httpfault.Unexpected("open the module record store").Wrap(err)
	}

	if store == nil {
		return nil, "", httpfault.StateNotConfigured()
	}

	return store, principal, nil
}

// moduleStateSegment is the last segment of moduleStatePath, for a refusal to
// name the address it refused.
const moduleStateSegment = "/state"

// recordKeySeparator is the character a record key writes a hierarchy with. A
// principal carrying one could name another person's record, so a principal
// that carries one is refused rather than escaped.
const recordKeySeparator = "/"

// validStateKey holds a key to one DNS-1123 label: lowercase letters, digits
// and inner hyphens, at most maxStateKeyLength of them.
//
// It is the grammar of a page path and a contributed id, so a module author
// writes one kind of name. The grammar has no slash, so a key cannot name a
// record below its own.
func validStateKey(key string) error {
	expected := fmt.Sprintf(
		"expected one label: lowercase letters, digits and inner hyphens, at most %d of them",
		maxStateKeyLength)

	invalid := func(reason string) error {
		return httpfault.RequestInvalid("path.key", expected, reason)
	}

	if key == "" {
		return invalid("the key is empty")
	}

	if len(key) > maxStateKeyLength {
		return invalid(fmt.Sprintf("the key is %d characters and one label holds %d",
			len(key), maxStateKeyLength))
	}

	for index := range len(key) {
		char := key[index]

		switch {
		case char >= 'a' && char <= 'z', char >= '0' && char <= '9':
			continue
		case char == '-' && index > 0 && index < len(key)-1:
			continue
		default:
			return invalid(fmt.Sprintf("the key carries %q, which one label does not hold",
				string(key[index])))
		}
	}

	return nil
}

// stateFailure renders what the store reported. A key that is not there is the
// ordinary outcome of a read on a pane nobody has saved yet, so it is a 404 and
// not a server failure; anything else is this deployment's own and is answered
// as one, with the cause in the log rather than in the body.
func stateFailure(err error, method, namespace, key, doing string) error {
	if errors.Is(err, ErrNoStateRecord) {
		return httpfault.RouteNotFound(method, ModuleMount(namespace)+moduleStateSegment+"/"+key)
	}

	return httpfault.Unexpected(doing).Wrap(err)
}
