package httpapi

// The capability set one device resolved to, with the type and the bounds of
// every argument each verb takes.
//
// The device record lists its capabilities as keys. That says which verbs a
// device has and not what to send them. A client building a control needs the
// argument's name, its type and the range the host will accept. This answer is
// the descriptor the host validates every command against, so a value inside
// it passes the host's check and a value outside it is refused before the
// driver is reached.

import (
	"context"
	"net/http"
	"slices"

	"github.com/danielgtaylor/huma/v2"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

const deviceCapabilitiesPath = "/api/v1/devices/{device_id}/capabilities"

// DeviceCapabilitySet is what the device plane hands this package: the
// resolved descriptor and the two facts the device record already carries
// about it. The projection onto the wire is here, so the plane names no wire
// type.
type DeviceCapabilitySet struct {
	// Set is the resolved, enabled set, empty for a device whose session has
	// ended.
	Set connectorapi.Set
	// Epoch moves on every arrival, as Device.CapabilityEpoch does.
	Epoch uint64
	// ProfileMatched is whether a capability profile covered the device.
	ProfileMatched bool
}

// DeviceCapabilities is one device's resolved capability set on the wire.
//
//nolint:lll // the huma tags are each field's whole contract and do not wrap.
type DeviceCapabilities struct {
	ID              string             `json:"device_id"        doc:"The device these capabilities belong to."`
	CapabilityEpoch uint64             `json:"capability_epoch" doc:"The epoch the device record carries. A set read under an older epoch describes a session that has ended."`
	ProfileMatched  bool               `json:"profile_matched"  doc:"Whether a capability profile covered this model and firmware. False means the driver baseline is in force."`
	Capabilities    []DeviceCapability `json:"capabilities"     doc:"Every verb and feature the device has, sorted by key. Empty for a device whose session has ended."`
}

// DeviceCapability is one verb or feature and the arguments it takes.
//
//nolint:lll // the huma tags are each field's whole contract and do not wrap.
type DeviceCapability struct {
	Key       string           `json:"key"       doc:"The capability key, as the device record lists it and a command names it under verb."`
	Arguments []DeviceArgument `json:"arguments" doc:"Every argument the verb accepts, sorted by name. Empty for a verb that takes none and for a feature."`
}

// DeviceArgument is one argument of a verb, as the host checks it.
//
// The descriptor bounds the magnitude. Minimum and maximum are written out
// from it, so a client reads one range and does not have to know that a bound
// of 300 with no floor also refuses -301.
//
//nolint:lll // the huma tags are each field's whole contract and do not wrap.
type DeviceArgument struct {
	Name      string   `json:"name"                 doc:"The name a command carries the value under, in args."`
	Key       string   `json:"key"                  doc:"The capability key of this argument: the verb, a hash sign and the name."`
	Type      string   `json:"type"                 doc:"How the value is read. Every value travels as text in args; this is what that text must parse as." enum:"string,integer,number,boolean,opaque"`
	Required  bool     `json:"required"             doc:"Whether a command must carry it."`
	Default   string   `json:"default,omitempty"    doc:"What the host sends when a command leaves it out. Absent when there is none."`
	Minimum   *float64 `json:"minimum,omitempty"    doc:"The lowest value accepted, inclusive, for a number. Absent when there is no lower bound."`
	Maximum   *float64 `json:"maximum,omitempty"    doc:"The highest value accepted, inclusive, for a number. Absent when there is no upper bound."`
	MaxLength *int     `json:"max_length,omitempty" doc:"The longest value accepted, in bytes, for a string. Absent when there is no bound."`
	Unit      string   `json:"unit,omitempty"       doc:"What a number counts in. Absent for a value with no unit, such as a count or an index."            enum:"celsius,percent,mm,mm/min,s,byte"`
	Enum      []string `json:"enum,omitempty"       doc:"The only values accepted. Absent when any value of the type is accepted."`
}

type deviceCapabilitiesOutput struct {
	Body DeviceCapabilities
}

func registerDeviceCapabilities(api huma.API, fleet DeviceFleet) {
	register(api, huma.Operation{
		OperationID: "get-device-capabilities",
		Method:      http.MethodGet,
		Path:        deviceCapabilitiesPath,
		Summary:     "The capabilities of one device, with the arguments of each verb",
		Description: "Every verb and feature the device resolved to, and for each verb the arguments it " +
			"takes: the name, the type, whether it is required, the default, the range, the unit and " +
			"the closed set of values where there is one. It is the set the host checks a command " +
			"against before the driver is reached, so a value inside it passes that check. The names " +
			"are the driver's own and can differ between two devices that share a verb. It takes the " +
			"action reading the device takes.",
		Tags:   []string{"devices"},
		Errors: deviceErrors,
	}, func(ctx context.Context, in *getDeviceInput) (*deviceCapabilitiesOutput, error) {
		resolved, err := fleet.Capabilities(ctx, principalFrom(ctx), in.DeviceID)
		if err != nil {
			return nil, err
		}

		return &deviceCapabilitiesOutput{Body: deviceCapabilitiesOf(in.DeviceID, resolved)}, nil
	})
}

// deviceCapabilitiesOf is the projection, separate from the read so a test can
// run it over a set it built.
func deviceCapabilitiesOf(id string, resolved DeviceCapabilitySet) DeviceCapabilities {
	out := DeviceCapabilities{
		ID:              id,
		CapabilityEpoch: resolved.Epoch,
		ProfileMatched:  resolved.ProfileMatched,
		Capabilities:    []DeviceCapability{},
	}

	verbs := resolved.Set.Verbs()
	index := make(map[connectorapi.Capability]int, len(verbs))

	for _, verb := range verbs {
		index[verb] = len(out.Capabilities)
		out.Capabilities = append(out.Capabilities, DeviceCapability{
			Key:       verb.String(),
			Arguments: []DeviceArgument{},
		})
	}

	// Keys come back sorted, so the arguments of one verb arrive sorted by
	// name. An argument whose verb the set does not carry describes nothing a
	// command can reach, and Problems refuses one, so it is skipped.
	for _, key := range resolved.Set.Keys() {
		name, isArgument := key.Argument()
		if !isArgument || !resolved.Set[key].Flag {
			continue
		}

		at, found := index[key.Verb()]
		if !found {
			continue
		}

		out.Capabilities[at].Arguments = append(out.Capabilities[at].Arguments,
			deviceArgumentOf(key, name, resolved.Set[key]))
	}

	return out
}

// deviceArgumentOf writes one argument's descriptor as a range a client reads
// directly.
func deviceArgumentOf(key connectorapi.Capability, name string, value connectorapi.Value) DeviceArgument {
	argument := DeviceArgument{
		Name:     name,
		Key:      key.String(),
		Type:     value.Kind.String(),
		Required: value.Required,
		Default:  value.Default,
		Unit:     value.Unit.String(),
		Enum:     slices.Clone(value.Supported),
	}

	switch {
	case value.Kind.IsNumeric():
		// The limit bounds the magnitude, so the value must be at least the
		// negative of the limit and at most the limit. A minimum of its own
		// applies only when it is the higher of the two lower bounds.
		switch {
		case value.HasMin && value.HasLimit:
			argument.Minimum = new(max(value.Min, -value.Limit))
		case value.HasMin:
			argument.Minimum = new(value.Min)
		case value.HasLimit:
			argument.Minimum = new(-value.Limit)
		}

		if value.HasLimit {
			argument.Maximum = new(value.Limit)
		}
	case value.Kind == connectorapi.KindString && value.HasLimit:
		argument.MaxLength = new(int(value.Limit))
	}

	return argument
}
