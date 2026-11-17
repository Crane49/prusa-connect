// Package uiapi is how a module hides entries of its own browser half while
// it runs.
//
// A module whose feature is switched off calls Hide, and the host takes the
// entries out of the listing the web app reads. An entry is a slot key, a
// slash and an identity, such as "pages/customers" or "file_action/plan".
// Each call sends the whole list, and an empty list shows every entry again.
//
// The host forgets the list when the instance stops, so call Hide again after
// a start. A module can hide only its own entries, and it cannot show an
// entry the operator hid.
package uiapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/prusa3d/prusa-connect/sdk/manifest"
	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

// MethodHide states which entries of the module's own interface are hidden.
// It takes HideParams and answers an empty object.
const MethodHide wirerpc.Method = "ui.hide"

// MaxEntries bounds the list of one call.
const MaxEntries = 64

// MaxEntryBytes bounds one entry.
const MaxEntryBytes = 200

// ErrInvalid reports a list outside the bounds of this contract.
var ErrInvalid = errors.New("uiapi: the list of hidden entries is outside the published bounds")

// HostFailed is the message a module gets when the call failed inside the
// host.
const HostFailed = "uiapi: the host could not take the list"

// HideParams is the params of MethodHide: every entry the module hides.
type HideParams struct {
	Hidden []string `json:"hidden"`
}

// SlotKeys are the slot keys an entry may start with: the key of every slot
// in sdk/manifest except cards.
func SlotKeys() []string {
	var keys []string

	for _, slot := range manifest.Slots() {
		if slot.Name != manifest.SlotDashboardCard {
			keys = append(keys, slot.Name.Key())
		}
	}

	return keys
}

// Validate checks the list: at most MaxEntries entries, each at most
// MaxEntryBytes, each a slot key from SlotKeys, a slash and an identity with
// no white space, and no entry twice. The host checks again.
func (p HideParams) Validate() error {
	if len(p.Hidden) > MaxEntries {
		return fmt.Errorf("%w: the list holds %d entries and the limit is %d",
			ErrInvalid, len(p.Hidden), MaxEntries)
	}

	keys := SlotKeys()

	for i, entry := range p.Hidden {
		if !validEntry(entry, keys) {
			return fmt.Errorf("%w: entry %d is %s, expected <slot key>/<identity> of at most %d bytes, "+
				"the slot key one of %s", ErrInvalid, i, strconv.Quote(entry), MaxEntryBytes,
				strings.Join(keys, ", "))
		}

		if slices.Contains(p.Hidden[:i], entry) {
			return fmt.Errorf("%w: %s is listed twice", ErrInvalid, strconv.Quote(entry))
		}
	}

	return nil
}

// validEntry reports whether one entry is a known slot key, a slash and an
// identity with no white space.
func validEntry(entry string, keys []string) bool {
	if len(entry) > MaxEntryBytes {
		return false
	}

	key, identity, found := strings.Cut(entry, "/")
	if !found || identity == "" || !slices.Contains(keys, key) {
		return false
	}

	return !strings.ContainsFunc(identity, unicode.IsSpace)
}

// Hider is the host's side of MethodHide. The host supplies it, bound to one
// instance, and a module never constructs one.
type Hider interface {
	Hide(ctx context.Context, hidden []string) error
}

// Caller is the part of a wirerpc session this contract needs. *wirerpc.Conn
// implements it.
type Caller interface {
	Call(ctx context.Context, method wirerpc.Method, params any) (json.RawMessage, error)
}

// Client calls MethodHide for a module.
type Client struct {
	conn Caller
}

// NewClient returns the client a module hides its entries through. A host
// that does not serve MethodHide answers CodeMethodNotFound on the first call.
func NewClient(conn Caller) *Client {
	return &Client{conn: conn}
}

// Hide states every entry the module hides. An entry that is not named is
// shown again, and no entry shows every one. An invalid list returns
// ErrInvalid, from this check or from the host.
func (c *Client) Hide(ctx context.Context, hidden ...string) error {
	params := HideParams{Hidden: hidden}
	if params.Hidden == nil {
		params.Hidden = []string{}
	}

	if err := params.Validate(); err != nil {
		return fmt.Errorf("uiapi: %s: %w", MethodHide, err)
	}

	if c.conn == nil {
		return fmt.Errorf("uiapi: %s: the client holds no session", MethodHide)
	}

	if _, err := c.conn.Call(ctx, MethodHide, params); err != nil {
		if coded, ok := errors.AsType[*wirerpc.Error](err); ok && coded.Code == wirerpc.CodeInvalidParams {
			return fmt.Errorf("uiapi: %s: %w: %s", MethodHide, ErrInvalid, coded.Message)
		}

		return fmt.Errorf("uiapi: %s: %w", MethodHide, err)
	}

	return nil
}

// Handlers is the host's MethodHide, bound to one instance. The set is empty
// for a nil hider.
func Handlers(hider Hider) wirerpc.Methods {
	if hider == nil {
		return wirerpc.Methods{}
	}

	return wirerpc.Methods{MethodHide: hideHandler(hider)}
}

// hideHandler answers MethodHide. An invalid list is CodeInvalidParams, and a
// failure of the host is CodeInternal with HostFailed.
func hideHandler(hider Hider) wirerpc.Handler {
	return func(ctx context.Context, call wirerpc.Call) (any, error) {
		var params HideParams

		if len(call.Params) == 0 {
			return nil, &wirerpc.Error{
				Code:    wirerpc.CodeInvalidParams,
				Message: fmt.Sprintf("%s takes a params object", call.Method),
			}
		}

		if err := json.Unmarshal(call.Params, &params); err != nil {
			return nil, &wirerpc.Error{
				Code:    wirerpc.CodeInvalidParams,
				Message: fmt.Sprintf("%s: the params object could not be read", call.Method),
			}
		}

		if err := params.Validate(); err != nil {
			return nil, &wirerpc.Error{Code: wirerpc.CodeInvalidParams, Message: err.Error()}
		}

		if err := hider.Hide(ctx, slices.Clone(params.Hidden)); err != nil {
			return nil, &wirerpc.Error{Code: wirerpc.CodeInternal, Message: HostFailed}
		}

		return struct{}{}, nil
	}
}
