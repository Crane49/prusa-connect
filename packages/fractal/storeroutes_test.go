package main

import (
	"context"
	"net/http"
	"testing"

	"github.com/prusa3d/fractal-link/bridge"
	"github.com/prusa3d/fractal-link/files"

	fractalfiles "github.com/prusa3d/prusa-connect/sdk/fractal/files"
)

// fixedListing is a server above that answers every listing with data, or
// with err.
type fixedListing struct {
	data []byte
	err  error
}

func (f fixedListing) Listing(context.Context, fractalfiles.Identity, int) ([]byte, error) {
	return f.data, f.err
}

// browsingTree is a tree of the server hq/brno whose server above answers
// every listing as above does.
func browsingTree(t *testing.T, above fixedListing) *tree {
	t.Helper()

	state := &tree{}
	state.serving(shadowOf(t), stubCaller{}, pathOf("hq/brno"))
	ancestors := newShadow(t)
	report(t, ancestors, "hq", fractalfiles.InterfaceName, "library", `{"rootHash":null}`)
	state.browsing(bridge.NewShadowView(ancestors), above)

	return state
}

// TestStoreRouteRefusals: an address that names no listing, a listing the
// server above does not publish, an answer that is not a listing, a server
// without a files block and one that is not serving yet are each refused with
// their own code.
func TestStoreRouteRefusals(t *testing.T) {
	t.Parallel()

	off := &tree{}
	off.serving(shadowOf(t), stubCaller{}, pathOf("hq/brno"))

	root := "/files/stores/hq/library/" + fractalfiles.EmptyListing.Text()

	for _, row := range []struct {
		name   string
		state  *tree
		path   string
		status int
		code   string
	}{
		{"no identity", browsingTree(t, fixedListing{}), "/files/stores/hq/library", statusBadRequest,
			StoreAddressInvalid.Code().String()},
		{"a bad identity", browsingTree(t, fixedListing{}), "/files/stores/hq/library/sha256:nope",
			statusBadRequest, StoreAddressInvalid.Code().String()},
		{"not published above", browsingTree(t, fixedListing{err: files.ErrNotAvailable}), root,
			statusNotFound, ListingNotFound.Code().String()},
		{"not a listing", browsingTree(t, fixedListing{data: []byte("G28")}), root,
			statusUnavailable, ListingUnavailable.Code().String()},
		{"a store above the shadow does not hold", browsingTree(t, fixedListing{}),
			"/files/stores/hq/usb/" + fractalfiles.EmptyListing.Text(), statusNotFound, ListingNotFound.Code().String()},
		{"no files block", off, root, statusNotFound, FilesOff.Code().String()},
		{"not serving", &tree{}, "/files/stores", statusUnavailable, TransportUnavailable.Code().String()},
	} {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()

			recorded := askAs(t, routes(row.state), reader, http.MethodGet, row.path)
			got := answered{status: recorded.Code, body: refusalCode(t, recorded.Body.Bytes())}

			if want := (answered{status: row.status, body: row.code}); got != want {
				t.Errorf("the route answered %+v, want %+v", got, want)
			}
		})
	}
}
