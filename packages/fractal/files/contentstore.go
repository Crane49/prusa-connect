package files

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
	fractalfiles "github.com/prusa3d/prusa-connect/sdk/fractal/files"
	fractalnode "github.com/prusa3d/prusa-connect/sdk/fractal/node"
	"github.com/prusa3d/prusa-connect/sdk/fractal/schema"
)

// Reporter sends a state report for one registered instance. The handle the
// Fractal node returns from Register implements it.
type Reporter interface {
	Report(ctx context.Context, props fractal.Props, at fractal.Timestamp) error
}

// Registrar registers an interface instance with the Fractal node.
type Registrar interface {
	Register(inst fractalnode.Instance) (Reporter, error)
}

// ContentStores keeps one ContentStore1 instance per storage registered with
// the node, and reports its root hash whenever it changes. An instance is
// never withdrawn while the module runs.
type ContentStores struct {
	publisher  *Publisher
	node       Registrar
	definition *schema.Definition
	log        *slog.Logger
	handles    map[string]Reporter
	// sent is the root hash each instance last reported.
	sent map[string]json.RawMessage
}

// NewContentStores reads the definition of ContentStore1 and returns the
// instances that publisher fills.
func NewContentStores(publisher *Publisher, node Registrar, log *slog.Logger) (*ContentStores, error) {
	registry, err := schema.Load(fractalfiles.Definitions)
	if err != nil {
		return nil, fmt.Errorf("load the definition of %s: %w", fractalfiles.InterfaceName, err)
	}

	definition, ok := registry.Definition(fractalfiles.InterfaceName)
	if !ok {
		return nil, fmt.Errorf("the definitions hold no %s", fractalfiles.InterfaceName)
	}

	return &ContentStores{
		publisher:  publisher,
		node:       node,
		definition: definition,
		log:        log,
		handles:    map[string]Reporter{},
		sent:       map[string]json.RawMessage{},
	}, nil
}

// Run registers an instance for every storage the publisher announces, and
// reports the instance each time it is announced again, until ctx ends.
func (c *ContentStores) Run(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case storage := <-c.publisher.Updated():
			c.update(ctx, storage)
		}
	}
}

// update registers the instance of one storage the first time and reports
// every property until one report succeeds. Later it reports only a changed
// root hash.
func (c *ContentStores) update(ctx context.Context, storage string) {
	store := contentStore{storage: storage, publisher: c.publisher}
	props := store.props(ctx)

	handle, ok := c.handles[storage]
	if !ok {
		registered, err := c.node.Register(fractalnode.Instance{
			Definition: c.definition,
			Owner:      store,
			Name:       fractal.InterfaceName(fractalfiles.InterfaceName),
			Instance:   instanceOf(storage),
		})
		if err != nil {
			c.log.ErrorContext(ctx, "a storage cannot be published as a content store",
				"storage", storage, "error", err)

			return
		}

		c.handles[storage] = registered
		handle = registered
	}

	if sent, known := c.sent[storage]; known {
		if bytes.Equal(sent, props[rootHash]) {
			return
		}

		props = fractal.Props{rootHash: props[rootHash]}
	}

	if err := handle.Report(ctx, props, fractal.TimestampOf(time.Now())); err != nil {
		c.log.WarnContext(ctx, "report a content store", "storage", storage, "error", err)

		return
	}

	c.sent[storage] = props[rootHash]
}

// instanceOf names the instance of a storage. A storage name is a DNS label
// and has no underscore, so the hyphen maps to one and back without loss.
func instanceOf(storage string) fractal.InstanceID {
	return fractal.InstanceID(strings.ReplaceAll(storage, "-", "_"))
}

// rootHash is the property of ContentStore1 that changes.
const rootHash fractal.PropertyName = "rootHash"

// contentStore is the owner of the ContentStore1 instance of one storage.
type contentStore struct {
	storage   string
	publisher *Publisher
}

var _ fractalnode.Owner = contentStore{}

// props is every property of the instance. A server storage is always
// present and never removable, and the module reports no capacity.
func (c contentStore) props(ctx context.Context) fractal.Props {
	root := json.RawMessage(`null`)
	if id, ok := c.publisher.RootHash(ctx, c.storage); ok {
		root = json.RawMessage(`"` + id.Text() + `"`)
	}

	return fractal.Props{
		rootHash:       root,
		"mediaPresent": json.RawMessage(`true`),
		"removable":    json.RawMessage(`false`),
		"capacity":     json.RawMessage(`null`),
		"free":         json.RawMessage(`null`),
	}
}

// Snapshot returns every property as it is now.
func (c contentStore) Snapshot(ctx context.Context) (fractal.Props, fractal.Timestamp, error) {
	return c.props(ctx), 0, nil
}

// Desire refuses every change. The definition makes every property read-only,
// so the node refuses a change before it gets here.
func (c contentStore) Desire(context.Context, fractal.Props) (fractal.Props, error) {
	return nil, &fractal.Error{Code: fractal.CodeDenied}
}

// Invoke answers List: the listing with the given identity, as unpadded
// base64url text, when this instance's storage publishes it.
func (c contentStore) Invoke(ctx context.Context, call fractalnode.Call) (json.RawMessage, error) {
	if call.Method != "List" {
		return nil, &fractal.Error{Code: fractal.CodeObjectNotFound, Message: "the interface has no such method"}
	}

	if len(call.Params) != 1 {
		return nil, &fractal.Error{Code: fractal.CodeInvalidParams, Message: "List takes one identity"}
	}

	var text string
	if err := json.Unmarshal(call.Params[0], &text); err != nil {
		return nil, &fractal.Error{Code: fractal.CodeInvalidParams, Message: "the identity is not text"}
	}

	id, err := fractalfiles.ParseText(text)
	if err != nil {
		return nil, &fractal.Error{Code: fractal.CodeInvalidParams, Message: "the identity is malformed"}
	}

	listing, ok := c.publisher.ListingOf(ctx, c.storage, id)
	if !ok {
		return nil, &fractal.Error{Code: fractal.CodeObjectNotFound}
	}

	encoded, err := json.Marshal(base64.RawURLEncoding.EncodeToString(listing))
	if err != nil {
		return nil, fmt.Errorf("encode the listing: %w", err)
	}

	return encoded, nil
}
