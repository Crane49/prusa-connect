package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/prusa3d/fractal-link/bridge"
	"github.com/prusa3d/fractal-link/files"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
	fractalnode "github.com/prusa3d/prusa-connect/sdk/fractal/node"
	"github.com/prusa3d/prusa-connect/sdk/routesapi"
)

// The bounds one answer of these routes keeps to.
const (
	// pageSize is how many interfaces one answer lists when the caller asks
	// for no size.
	pageSize = 100
	// maxPageSize is the largest size a caller may ask for.
	maxPageSize = 500
	// bodyBudget is the largest answer these routes build. The host refuses a
	// larger one, so the listing stops before it and says it stopped.
	bodyBudget = routesapi.MaxModuleBody
	// callBudget is how long a method call may take. It is the deadline this
	// server writes into the request, so a server on the way that receives it
	// later does not run it.
	//
	// It is under the time the host waits for an answer on one call. A host
	// that gives up first counts the call as a failure of this module, and a
	// few of those in a row stop the host calling this module at all. So this
	// route answers its own error before the host gives up.
	callBudget = 8 * time.Second
	// listBudget is how many bytes of one answer the list may take. The rest
	// is the members around the list, which need far less.
	listBudget = bodyBudget - bodyBudget/4
	// propsBudget is the largest one interface may be in an answer, with its
	// property values. A larger one is answered without them.
	propsBudget = listBudget / 4
)

// caller sends one method call to a node below this one and waits for the
// answer. The Node of sdk/fractal/node is what serves it here.
type caller interface {
	Call(ctx context.Context, req fractalnode.CallRequest) (fractal.RES, error)
}

// tree holds what these routes read: the copy of the state of the servers
// below, the Fractal node that sends a method call down, and this server's own
// place in the tree, which here reads each time, because a server gets it
// only when it joins. build stores all three once the settings arrive, so a
// request that comes in before that is answered with TransportUnavailable.
type tree struct {
	mu     sync.RWMutex
	shadow *bridge.ShadowView
	node   caller
	here   func() fractal.NodePath
	shares *files.Shares
	// lister checks that a storage exists before a folder of it is shared.
	lister storageLister
	log    *slog.Logger
	// ancestors is the copy of the state above, and listings fetches a
	// listing from above. Both are nil when the file extension is off.
	ancestors *bridge.ShadowView
	listings  listingSource
}

// serving stores the parts the routes read. It is called once, when the node
// is built.
func (t *tree) serving(view *bridge.ShadowView, node caller, here func() fractal.NodePath) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.shadow, t.node, t.here = view, node, here
}

// sharing stores the shared folders the share routes change. build calls it
// before serving, and only when the file extension is on.
func (t *tree) sharing(shares *files.Shares, lister storageLister, log *slog.Logger) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.shares, t.lister, t.log = shares, lister, log
}

// view returns the copy of the state below, and nil before the settings
// arrived.
func (t *tree) view() *bridge.ShadowView {
	t.mu.RLock()
	defer t.mu.RUnlock()

	return t.shadow
}

// sender returns what sends a method call down, and nil before the settings
// arrived.
func (t *tree) sender() caller {
	t.mu.RLock()
	defer t.mu.RUnlock()

	return t.node
}

// routes returns the HTTP surface of this module. Every path is relative to
// the mount the host put this module under.
//
// A GET reads the copy this server keeps of the state below it and sends
// nothing over the network, so it answers within the host's own deadline. The
// POST sends one method call down the tree and waits for the answer.
func routes(state *tree) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /nodes", state.listNodes)
	mux.HandleFunc("GET /nodes/{path...}", state.listInterfaces)
	mux.HandleFunc("POST /nodes/{rest...}", state.callMethod)
	mux.HandleFunc("GET /files/shares", state.listShares)
	mux.HandleFunc("GET /files/stores", state.listStores)
	mux.HandleFunc("GET /files/stores/{rest...}", state.readStore)
	mux.HandleFunc("PUT /files/shares/{storage}/{path...}", state.changeShare)
	mux.HandleFunc("DELETE /files/shares/{storage}/{path...}", state.changeShare)

	return mux
}

// nodeListing is the answer of GET /nodes.
type nodeListing struct {
	// Node is this server's own place in the tree.
	Node fractal.NodePath `json:"node"`
	// Items is one page of the servers and printers below this one that the
	// copy holds state for.
	Items []bridge.ShadowNode `json:"items"`
	// Total is how many entries Items has.
	Total int `json:"total"`
	// Truncated says the copy holds more nodes than Items lists.
	Truncated bool `json:"truncated"`
	// Next is what to send as the after parameter to read the page that
	// follows. It is empty on the last page.
	Next string `json:"next,omitempty"`
}

// listNodes answers GET /nodes. It lists the servers and printers below this
// one that the copy holds interfaces for, with the place of each in the tree
// and whether the values it reported are current.
//
// The list is one page. limit says how many entries to answer and after is the
// place in the tree of the last entry of the page before, so a tree with more
// nodes than one answer holds is read page by page.
func (t *tree) listNodes(w http.ResponseWriter, r *http.Request) {
	view := t.view()
	if view == nil {
		notServingYet(w)

		return
	}

	limit, err := pageOf(r)
	if err != nil {
		problem(w, statusBadRequest, RouteLimitInvalid.Code().String(),
			"The page size is not a number this route accepts", err.Error())

		return
	}

	held := after(view.Nodes(), r.URL.Query().Get("after"), func(one bridge.ShadowNode) string {
		return string(one.Path)
	})

	out := nodeListing{Node: t.ownPath(), Items: []bridge.ShadowNode{}}

	used := 0

	for index, one := range held {
		encoded, marshalErr := json.Marshal(one)
		if marshalErr != nil {
			continue
		}

		if index >= limit || (used+len(encoded) > listBudget && len(out.Items) > 0) {
			out.Truncated = true

			break
		}

		used += len(encoded)
		out.Items = append(out.Items, one)
	}

	out.Total = len(out.Items)

	if out.Truncated && out.Total > 0 {
		out.Next = string(out.Items[out.Total-1].Path)
	}

	answer(w, out)
}

// listedInterface is one interface of one node below, as this route answers
// it. PropsOmitted is the one member the copy does not hold.
type listedInterface struct {
	bridge.ShadowInterface

	// PropsOmitted says the property values were left out, because they alone
	// are larger than one answer of this route may be. Read that interface on
	// the server that owns it.
	PropsOmitted bool `json:"props_omitted,omitempty"`
}

// interfaceListing is the answer of GET /nodes/{path...}.
type interfaceListing struct {
	// Node is the node the caller asked about.
	Node fractal.NodePath `json:"node"`
	// Items are the interfaces of that node and of the nodes below it, each
	// with its instance, its status, when it was last updated and its property
	// values.
	Items []listedInterface `json:"items"`
	// Total is how many entries Items has.
	Total int `json:"total"`
	// Truncated says that the copy holds more interfaces than Items lists,
	// because the page filled or the answer reached its size limit.
	Truncated bool `json:"truncated"`
	// Next is what to send as the after parameter to read the page that
	// follows. It is empty on the last page.
	Next string `json:"next,omitempty"`
}

// listInterfaces answers GET /nodes/{path...}. It lists one page of the
// interfaces the copy holds for one node and for the nodes below it.
//
// limit says how many interfaces to answer and after is the cursor of the last
// interface of the page before, which the answer of that page carries as next.
func (t *tree) listInterfaces(w http.ResponseWriter, r *http.Request) {
	view := t.view()
	if view == nil {
		notServingYet(w)

		return
	}

	path, err := fractal.ParseNodePath(strings.Trim(r.PathValue("path"), "/"))
	if err != nil {
		problem(w, statusBadRequest, RouteNodeInvalid.Code().String(),
			"The address does not name a place in the tree",
			fmt.Sprintf("%q is not a node path: %v", r.PathValue("path"), err))

		return
	}

	limit, err := pageOf(r)
	if err != nil {
		problem(w, statusBadRequest, RouteLimitInvalid.Code().String(),
			"The page size is not a number this route accepts", err.Error())

		return
	}

	held := after(view.Subtree(path), r.URL.Query().Get("after"), cursorOf)

	out := interfaceListing{Node: path}
	out.Items, out.Truncated = page(held, limit)
	out.Total = len(out.Items)

	if out.Truncated && out.Total > 0 {
		out.Next = cursorOf(out.Items[out.Total-1].ShadowInterface)
	}

	answer(w, out)
}

// cursorOf is the place of one interface in the listing: the node, the
// interface name and the instance, joined by a character none of the three may
// hold.
func cursorOf(one bridge.ShadowInterface) string {
	return string(one.Node) + "|" + string(one.Interface) + "|" + string(one.Instance)
}

// after drops the entries up to and including the one the cursor names. An
// empty cursor keeps every entry. A cursor no entry matches keeps none, so a
// caller reading a tree that changed under it reaches the end of the listing
// rather than reading the first page again.
func after[T any](held []T, cursor string, key func(T) string) []T {
	if cursor == "" {
		return held
	}

	for index, one := range held {
		if key(one) == cursor {
			return held[index+1:]
		}
	}

	return nil
}

// page returns the entries of held that fit both the page size and the size
// limit of one answer, and whether it left any out.
//
// The size is measured on the encoded entry, because one interface carries the
// property values the node below reported and nothing here bounds how large
// those are. An interface whose values alone are over the limit is answered
// without them and marked, so a page is never empty because of one interface.
func page(held []bridge.ShadowInterface, limit int) (items []listedInterface, truncated bool) {
	items = make([]listedInterface, 0, min(len(held), limit))

	used := 0

	for index, one := range held {
		if index >= limit {
			return items, true
		}

		listed, size, ok := fitted(one)
		if !ok {
			continue
		}

		if used+size > listBudget && len(items) > 0 {
			return items, true
		}

		used += size
		items = append(items, listed)
	}

	return items, false
}

// fitted returns one interface as this route answers it, with its encoded
// size. An interface whose property values take more than propsBudget is
// returned without them.
func fitted(one bridge.ShadowInterface) (listed listedInterface, size int, ok bool) {
	encoded, err := json.Marshal(one)
	if err != nil {
		return listedInterface{}, 0, false
	}

	if len(encoded) <= propsBudget {
		return listedInterface{ShadowInterface: one}, len(encoded), true
	}

	one.Props = nil

	encoded, err = json.Marshal(one)
	if err != nil {
		return listedInterface{}, 0, false
	}

	return listedInterface{ShadowInterface: one, PropsOmitted: true}, len(encoded), true
}

// pageOf reads the limit parameter. An absent parameter gives pageSize, and a
// value over maxPageSize gives maxPageSize.
func pageOf(r *http.Request) (int, error) {
	text := r.URL.Query().Get("limit")
	if text == "" {
		return pageSize, nil
	}

	held, err := strconv.Atoi(text)
	if err != nil || held < 1 {
		return 0, fmt.Errorf("limit is %q, and this route takes a whole number from 1 to %d",
			text, maxPageSize)
	}

	return min(held, maxPageSize), nil
}

// callAnswer is the answer of the POST. It carries the result the node below
// returned, or the error with the protocol's own code.
type callAnswer struct {
	// Node is the node that ran the method.
	Node fractal.NodePath `json:"node"`
	// Result is what the method returned. It is absent when the call failed.
	Result json.RawMessage `json:"result,omitempty"`
	// Error is the failure, with the code of the Fractal specification and the
	// English sentence the node below sent. It is absent when the call ran.
	Error *callError `json:"error,omitempty"`
}

// callError is one failed method call, as a caller of this route reads it.
type callError struct {
	// Code is the number of the Fractal specification, refined by the table in
	// bridge/codes.go on a server that runs this module.
	Code fractal.Code `json:"code"`
	// Message is the English sentence the node that refused the call sent.
	Message string `json:"message"`
}

// callMethod answers POST /nodes/{path}/interfaces/{interface}/{instance}/methods/{method}.
// It sends one method call to a node below this one and answers the result or
// the error.
//
// The body is the parameters as a JSON array, in the order the method declares
// them. An empty body is a call with no parameters.
//
// The host decides who is calling and takes operate_resource on this link
// before it makes this call, so a person who may not operate this link never
// reaches this handler. The node that receives the call checks its own
// permissions again, for the server directly above it.
func (t *tree) callMethod(w http.ResponseWriter, r *http.Request) {
	node := t.sender()
	if node == nil {
		notServingYet(w)

		return
	}

	target, err := parseCall(r.PathValue("rest"))
	if err != nil {
		problem(w, statusBadRequest, RouteCallInvalid.Code().String(),
			"The address does not name a method of an interface of a node",
			err.Error())

		return
	}

	params, err := paramsOf(r)
	if err != nil {
		problem(w, statusBadRequest, RouteParamsInvalid.Code().String(),
			"The parameters are not a JSON array",
			err.Error())

		return
	}

	// The call ends when the caller goes away and after callBudget in any
	// case, so a node below that never answers does not hold this process.
	ctx, cancel := context.WithTimeout(r.Context(), callBudget)
	defer cancel()

	res, err := node.Call(ctx, fractalnode.CallRequest{
		Target:    target.node,
		Interface: target.name,
		Instance:  target.instance,
		Method:    target.method,
		Params:    params,
		Expires:   fractal.TimestampOf(time.Now().Add(callBudget)),
	})
	if err != nil {
		problem(w, statusUnavailable, CallFailed.Code().String(),
			"This server could not send the call to the node below",
			err.Error())

		return
	}

	if len(res.Result) > listBudget {
		problem(w, statusInternal, ResultTooLarge.Code().String(),
			"The node below returned more than this route may answer",
			fmt.Sprintf("the result is %d bytes and this route answers at most %d",
				len(res.Result), listBudget))

		return
	}

	out := callAnswer{Node: target.node, Result: res.Result}
	if res.Error != nil {
		out.Error = &callError{Code: res.Error.Code, Message: res.Error.Message}
	}

	answer(w, out)
}

// target is the node, the interface, the instance and the method one POST
// names.
type target struct {
	node     fractal.NodePath
	name     fractal.InterfaceName
	instance fractal.InstanceID
	method   fractal.MethodName
}

// parseCall reads the part of the address after /nodes/. A node path holds
// slashes, so the address is split on the two fixed words rather than by
// counting segments.
func parseCall(rest string) (target, error) {
	node, after, found := strings.Cut(strings.Trim(rest, "/"), "/interfaces/")
	if !found {
		return target{}, fmt.Errorf("the address names no interface: %q", rest)
	}

	named, method, found := strings.Cut(after, "/methods/")
	if !found {
		return target{}, fmt.Errorf("the address names no method: %q", rest)
	}

	name, instance, found := strings.Cut(named, "/")
	if !found {
		return target{}, fmt.Errorf("the address names no instance of the interface: %q", rest)
	}

	var (
		held target
		err  error
	)

	if held.node, err = fractal.ParseNodePath(node); err != nil {
		return target{}, fmt.Errorf("%q is not a node path: %w", node, err)
	}

	if held.name, err = fractal.ParseInterfaceName(name); err != nil {
		return target{}, fmt.Errorf("%q is not an interface name: %w", name, err)
	}

	if held.instance, err = fractal.ParseInstanceID(instance); err != nil {
		return target{}, fmt.Errorf("%q is not an instance id: %w", instance, err)
	}

	if held.method, err = fractal.ParseMethodName(method); err != nil {
		return target{}, fmt.Errorf("%q is not a method name: %w", method, err)
	}

	return held, nil
}

// paramsOf reads the body as a JSON array of parameters. An empty body is a
// call with no parameters.
func paramsOf(r *http.Request) ([]json.RawMessage, error) {
	body, err := io.ReadAll(io.LimitReader(r.Body, bodyBudget))
	if err != nil {
		return nil, fmt.Errorf("read the body: %w", err)
	}

	if len(strings.TrimSpace(string(body))) == 0 {
		return nil, nil
	}

	var params []json.RawMessage
	if err = json.Unmarshal(body, &params); err != nil {
		return nil, fmt.Errorf("read the parameters: %w", err)
	}

	return params, nil
}

// ownPath returns this server's own place in the tree, and an empty path
// before the server above granted one.
func (t *tree) ownPath() fractal.NodePath {
	t.mu.RLock()
	here := t.here
	t.mu.RUnlock()

	if here == nil {
		return ""
	}

	return here()
}

// answer writes one JSON body with 200 OK.
func answer(w http.ResponseWriter, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	_ = json.NewEncoder(w).Encode(body)
}

// notServingYet is the answer for a request that arrives before the settings
// did, or after the node stopped.
func notServingYet(w http.ResponseWriter) {
	problem(w, statusUnavailable, TransportUnavailable.Code().String(),
		"This link is not serving yet",
		"Wait for the link to finish starting and send the request again.")
}

// problem writes one refusal, in the shape RFC 9457 names. The code is this
// module's own, so a page in another language keys its sentence by it.
func problem(w http.ResponseWriter, status int, code, title, detail string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)

	_ = json.NewEncoder(w).Encode(map[string]any{
		"type":      "about:blank",
		"status":    status,
		"code":      code,
		"title":     title,
		"detail":    detail,
		"params":    map[string]any{},
		"retryable": status == statusUnavailable,
	})
}
