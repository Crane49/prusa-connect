package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/prusa3d/fractal-link/bridge"
	"github.com/prusa3d/fractal-link/files"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
	fractalfiles "github.com/prusa3d/prusa-connect/sdk/fractal/files"
	fractalnode "github.com/prusa3d/prusa-connect/sdk/fractal/node"
)

// listingSource fetches an object from the node above, or reads a copy this
// server keeps. *files.Files implements it.
type listingSource interface {
	Listing(ctx context.Context, id fractalfiles.Identity, limit int) ([]byte, error)
}

// browsing stores the copy of the state above and what fetches a listing from
// above. build calls it only when the file extension is on.
func (t *tree) browsing(ancestors *bridge.ShadowView, listings listingSource) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.ancestors, t.listings = ancestors, listings
}

// browser returns what the store routes read, or answers the request itself
// and reports false: before the settings arrived, and on a server whose
// settings have no files block.
func (t *tree) browser(w http.ResponseWriter) (*bridge.ShadowView, listingSource, fractal.NodePath, bool) {
	t.mu.RLock()
	ancestors, listings, serving := t.ancestors, t.listings, t.shadow != nil
	t.mu.RUnlock()

	switch {
	case listings != nil:
		return ancestors, listings, t.ownPath(), true
	case !serving:
		notServingYet(w)
	default:
		problem(w, statusNotFound, FilesOff.Code().String(),
			"This server does not run the file extension",
			"Add a files block to the settings of this link and start it again.")
	}

	return nil, nil, "", false
}

// listStores answers GET /files/stores: every ContentStore1 of the nodes
// above this server and of the nodes below it, with its properties. The host
// takes view_resource on this link before it forwards the request.
func (t *tree) listStores(w http.ResponseWriter, r *http.Request) {
	ancestors, _, own, ok := t.browser(w)
	if !ok {
		return
	}

	var held []bridge.ShadowInterface

	if root, found := rootOf(own); found {
		held = append(held, ancestors.Subtree(root)...)
	}

	held = append(held, t.view().Subtree(own)...)
	held = slices.DeleteFunc(held, func(one bridge.ShadowInterface) bool {
		return one.Interface != fractalfiles.InterfaceName
	})

	limit, err := pageOf(r)
	if err != nil {
		problem(w, statusBadRequest, RouteLimitInvalid.Code().String(),
			"The page size is not a number this route accepts", err.Error())

		return
	}

	out := interfaceListing{Node: own}
	out.Items, out.Truncated = page(after(held, r.URL.Query().Get("after"), cursorOf), limit)
	out.Total = len(out.Items)

	if out.Truncated && out.Total > 0 {
		out.Next = cursorOf(out.Items[out.Total-1].ShadowInterface)
	}

	answer(w, out)
}

// rootOf returns the first node of path, the root of the tree.
func rootOf(path fractal.NodePath) (fractal.NodePath, bool) {
	for first := range path.Segments() {
		return fractal.NodePath(first), true
	}

	return "", false
}

// storeListing is the answer of GET /files/stores/{node}/{instance}/{identity}.
type storeListing struct {
	// Node is the node whose content store the listing belongs to.
	Node fractal.NodePath `json:"node"`
	// Instance is the instance of ContentStore1 on that node.
	Instance fractal.InstanceID `json:"instance"`
	// Identity is the identity of the listing.
	Identity string `json:"identity"`
	// Entries are the files and folders of the listing, sorted by name.
	Entries []storeEntry `json:"entries"`
}

// storeEntry is one file or folder of a listing.
type storeEntry struct {
	Name string `json:"name"`
	// Kind is "file" or "directory".
	Kind string `json:"kind"`
	// Size is the length of a file, and the length of the listing of a
	// folder.
	Size uint64 `json:"size"`
	// Identity names the bytes of a file, or the listing of a folder, which
	// this route reads next.
	Identity string `json:"identity"`
}

// storeAddress is what GET /files/stores/{node}/{instance}/{identity} names.
type storeAddress struct {
	node     fractal.NodePath
	instance fractal.InstanceID
	id       fractalfiles.Identity
}

// readStore answers GET /files/stores/{node}/{instance}/{identity}: one
// listing of a content store, decoded. A store above this server is read with
// Get over the binding, and one below with List over Fractal RPC. Any other
// node is out of reach.
func (t *tree) readStore(w http.ResponseWriter, r *http.Request) {
	ancestors, listings, own, ok := t.browser(w)
	if !ok {
		return
	}

	at, err := parseStoreAddress(r.PathValue("rest"))
	if err != nil {
		problem(w, statusBadRequest, StoreAddressInvalid.Code().String(),
			"The address does not name a listing of a content store", err.Error())

		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), callBudget)
	defer cancel()

	var data []byte

	switch {
	case at.node.IsAncestorOf(own) && !holdsStore(ancestors, at):
		err = errListingNotFound
	case at.node.IsAncestorOf(own):
		data, err = listings.Listing(ctx, at.id, listBudget)
	case own.IsAncestorOf(at.node):
		data, err = t.listBelow(ctx, at)
	default:
		problem(w, statusNotFound, StoreOutsideLine.Code().String(),
			"This server cannot read the stores of that node",
			fmt.Sprintf("%s is neither above nor below %s", at.node, own))

		return
	}

	t.answerListing(w, at, data, err)
}

// answerListing decodes a listing and answers it, or answers why there is
// none.
func (t *tree) answerListing(w http.ResponseWriter, at storeAddress, data []byte, err error) {
	var listing fractalfiles.Listing
	if err == nil {
		listing, err = fractalfiles.DecodeListing(data)
	}

	switch {
	case err == nil:
	case errors.Is(err, files.ErrNotAvailable), errors.Is(err, errListingNotFound):
		problem(w, statusNotFound, ListingNotFound.Code().String(),
			"The node does not publish this listing", err.Error())

		return
	default:
		problem(w, statusUnavailable, ListingUnavailable.Code().String(),
			"This server could not read the listing now", err.Error())

		return
	}

	out := storeListing{Node: at.node, Instance: at.instance, Identity: at.id.Text(), Entries: []storeEntry{}}

	for _, entry := range listing.Entries {
		kind := "file"
		if entry.Kind == fractalfiles.KindDirectory {
			kind = "directory"
		}

		out.Entries = append(out.Entries, storeEntry{
			Name: entry.Name, Kind: kind, Size: entry.Size, Identity: entry.Hash.Text(),
		})
	}

	answer(w, out)
}

// errListingNotFound is a List the node below answered with
// ERR_OBJECT_NOT_FOUND, or a store above the ancestor shadow does not hold.
var errListingNotFound = errors.New("the node publishes no such listing")

// holdsStore reports whether the ancestor shadow holds the ContentStore1 the
// address names. A listing from above comes through the parent, which does
// not know the store, so the address is checked here.
func holdsStore(ancestors *bridge.ShadowView, at storeAddress) bool {
	return slices.ContainsFunc(ancestors.Subtree(at.node), func(one bridge.ShadowInterface) bool {
		return one.Node == at.node && one.Interface == fractalfiles.InterfaceName && one.Instance == at.instance
	})
}

// listBelow calls List on the content store of a node below and returns the
// listing it answered, checked against its identity.
func (t *tree) listBelow(ctx context.Context, at storeAddress) ([]byte, error) {
	node := t.sender()
	if node == nil {
		return nil, errors.New("this server is not serving yet")
	}

	res, err := node.Call(ctx, fractalnode.CallRequest{
		Target:    at.node,
		Interface: fractalfiles.InterfaceName,
		Instance:  at.instance,
		Method:    "List",
		Params:    []json.RawMessage{json.RawMessage(strconv.Quote(at.id.Text()))},
		Expires:   fractal.TimestampOf(time.Now().Add(callBudget)),
	})
	if err != nil {
		return nil, fmt.Errorf("call List: %w", err)
	}

	if res.Error != nil {
		if res.Error.Code == fractal.CodeObjectNotFound {
			return nil, errListingNotFound
		}

		return nil, fmt.Errorf("List answered %d: %s", res.Error.Code, res.Error.Message)
	}

	var text string
	if err = json.Unmarshal(res.Result, &text); err != nil {
		return nil, fmt.Errorf("List answered no text: %w", err)
	}

	data, err := base64.RawURLEncoding.DecodeString(text)
	if err != nil {
		return nil, fmt.Errorf("List answered no base64url: %w", err)
	}

	if fractalfiles.Sum(data) != at.id {
		return nil, files.ErrMismatch
	}

	return data, nil
}

// parseStoreAddress reads the part of the address after /files/stores/. A
// node path holds slashes, so the instance and the identity are the last two
// segments.
func parseStoreAddress(rest string) (storeAddress, error) {
	rest = strings.Trim(rest, "/")

	head, text, found := cutLast(rest)
	if !found {
		return storeAddress{}, fmt.Errorf("the address names no identity: %q", rest)
	}

	node, instance, found := cutLast(head)
	if !found {
		return storeAddress{}, fmt.Errorf("the address names no instance: %q", rest)
	}

	var (
		at  storeAddress
		err error
	)

	if at.node, err = fractal.ParseNodePath(node); err != nil {
		return storeAddress{}, fmt.Errorf("%q is not a node path: %w", node, err)
	}

	if at.instance, err = fractal.ParseInstanceID(instance); err != nil {
		return storeAddress{}, fmt.Errorf("%q is not an instance id: %w", instance, err)
	}

	if at.id, err = fractalfiles.ParseText(text); err != nil {
		return storeAddress{}, fmt.Errorf("%q is not an identity: %w", text, err)
	}

	return at, nil
}

// cutLast splits s around its last slash.
func cutLast(s string) (before, after string, found bool) {
	at := strings.LastIndex(s, "/")
	if at < 0 {
		return "", "", false
	}

	return s[:at], s[at+1:], true
}
