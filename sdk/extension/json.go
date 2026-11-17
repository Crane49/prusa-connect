package extension

import (
	"encoding/json"
	"fmt"
)

// wire is the JSON shape of a Descriptor: one flat record per entry.
type wire struct {
	Doc

	Kind      Kind      `json:"kind"`
	Name      string    `json:"name"`
	Namespace Namespace `json:"namespace"`
	Module    string    `json:"module"`
}

// MarshalJSON writes the descriptor as one flat record.
func (d Descriptor) MarshalJSON() ([]byte, error) {
	encoded, err := json.Marshal(wire{
		Kind:      d.kind,
		Name:      d.name,
		Namespace: d.namespace,
		Module:    d.module,
		Doc:       d.doc,
	})
	if err != nil {
		return nil, fmt.Errorf("extension: encode %s: %w", d, err)
	}

	return encoded, nil
}

// UnmarshalJSON reads a descriptor back and applies the same grammar a
// registration does.
func (d *Descriptor) UnmarshalJSON(data []byte) error {
	var record wire
	if err := json.Unmarshal(data, &record); err != nil {
		return fmt.Errorf("extension: decode descriptor: %w", err)
	}

	if !record.Kind.Valid() {
		return fmt.Errorf("%w: %q", ErrInvalidKind, record.Kind)
	}

	if err := ValidateName(record.Name); err != nil {
		return err
	}

	if record.Namespace.IsZero() {
		return fmt.Errorf("%w: %s %s has no namespace", ErrNoNamespace, record.Kind, record.Name)
	}

	if err := record.Doc.validateText(record.Kind.String() + " " + record.Name); err != nil {
		return err
	}

	*d = Descriptor{
		kind:      record.Kind,
		name:      record.Name,
		namespace: record.Namespace,
		module:    record.Module,
		doc:       record.Doc.clone(),
	}

	return nil
}
