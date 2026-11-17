package extension

import (
	"encoding/json"
	"fmt"
	"slices"
	"sync"
)

// catalogue is the process-wide catalogue. Every Set records what it accepts.
//
//nolint:gochecknoglobals // registration happens before main; there is nowhere else for it to go.
var catalogue = &Catalogue{}

// Catalogue is every descriptor a binary registered, across every extension
// point. It keeps a qualified name unique across the process, because one
// extension point may have more than one gathering list.
//
// The zero value is empty and ready to use.
type Catalogue struct {
	mu          sync.Mutex
	descriptors []Descriptor
	taken       map[string]Descriptor
	// refused is why the host would not admit a registration, keyed the way
	// taken is. See Refuse.
	refused map[string]string
}

// Global returns the process-wide catalogue that every Set records into.
func Global() *Catalogue { return catalogue }

// All returns every descriptor, in the order the published reference lists
// them.
func (c *Catalogue) All() []Descriptor {
	c.mu.Lock()
	defer c.mu.Unlock()

	out := slices.Clone(c.descriptors)
	SortDescriptors(out)

	return out
}

// Refuse records that the host would not admit one registration, and why. The
// entry stays listed, with the reason. A refusal for a name nothing registered
// is kept too.
func (c *Catalogue) Refuse(kind Kind, qualified, reason string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.refused == nil {
		c.refused = make(map[string]string)
	}

	c.refused[kind.String()+" "+qualified] = reason
}

// Refusal returns why the host refused this descriptor's registration, and
// the empty string for one it admitted.
func (c *Catalogue) Refusal(d Descriptor) string {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.refused[d.String()]
}

// Snapshot serialises the catalogue as sorted, indented JSON ending in a
// newline. Identical registrations produce identical bytes.
func (c *Catalogue) Snapshot() ([]byte, error) {
	encoded, err := json.MarshalIndent(c.All(), "", "  ")
	if err != nil {
		return nil, fmt.Errorf("extension: snapshot catalogue: %w", err)
	}

	return append(encoded, '\n'), nil
}

// record adds one descriptor. It reports ErrDuplicate for a kind and qualified
// name already recorded.
func (c *Catalogue) record(d Descriptor) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if existing, taken := c.taken[d.String()]; taken {
		return fmt.Errorf("%w: %s is registered by %q and again by %q",
			ErrDuplicate, d, existing.module, d.module)
	}

	if c.taken == nil {
		c.taken = make(map[string]Descriptor)
	}

	c.taken[d.String()] = d
	c.descriptors = append(c.descriptors, d)

	return nil
}
