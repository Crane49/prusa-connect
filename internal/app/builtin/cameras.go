package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/prusa3d/prusa-connect/internal/extension/host"
	"github.com/prusa3d/prusa-connect/internal/extension/registry"
	"github.com/prusa3d/prusa-connect/internal/extension/supervised"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/sdk/cameraapi"
	"github.com/prusa3d/prusa-connect/sdk/extension"
)

// printerKey is the one key in a camera's settings block the host reads. It is
// the authorization key, so it is host-owned: a driver that named it would be
// choosing who sees its frames.
const printerKey = "printer"

// HostKeys is the settings of one kind that the host reads and the type does
// not, so a block carrying one is not refused for an undeclared key.
//
// A camera is the only kind with any. Who may see its frames, how often a still
// image is kept and where it is kept are the deployment's decisions, so no
// camera driver declares them. Every other kind returns an empty list.
func HostKeys(kind ref.Kind) []extension.Option {
	if kind != ref.KindCamera {
		return nil
	}

	return []extension.Option{
		{
			Key:     printerKey,
			Type:    extension.OptionString,
			Summary: "The printer this camera watches. A grant on that printer is what lets somebody see the frames.",
		},
		{
			Key:     historyEveryKey,
			Type:    extension.OptionDuration,
			Summary: "How often a still image is kept. No key keeps none.",
		},
		{
			Key:     historyStorageKey,
			Type:    extension.OptionString,
			Default: historyStorageDefault,
			Summary: "The storage instance a kept image is written to.",
		},
	}
}

// cameraType is one camera driver as a resource type.
//
// It differs from the sink and identity bridges in one way. A camera's frames
// are authorized by a grant on the printer it watches, so the binding has to
// reach the host. It is read from the settings block here and travels with the
// instance as supervised.Bound, so the reconciler keeps no second copy of the
// declaration.
func cameraType(cameras *cameraapi.Registry, qualified string) (registry.Type, error) {
	registered, err := cameras.Lookup(qualified)
	if err != nil {
		return registry.Type{}, fmt.Errorf("adopt the %s camera: %w", qualified, err)
	}

	return adoptCamera(registered)
}

// adoptCamera declares one camera registration as a resource type. It is split
// from the lookup so a caller holding a registration can adopt it directly.
func adoptCamera(registered cameraapi.Registration) (registry.Type, error) {
	return adopt(ref.KindCamera, registered.Qualified(), registered.Descriptor(),
		func(_ context.Context, _ host.Handle, spec registry.Spec) (supervised.Bound, error) {
			settings, decodeErr := openSettings(spec)
			if decodeErr != nil {
				return supervised.Bound{}, decodeErr
			}

			printer, decodeErr := printerOf(spec, settings)
			if decodeErr != nil {
				return supervised.Bound{}, decodeErr
			}

			camera, buildErr := registered.Build(cameraapi.ProviderSpec{
				ID:      spec.Ref.ID(),
				Type:    spec.Type,
				Printer: printer,
				Config:  settings,
			})
			if buildErr != nil {
				return supervised.Bound{}, buildErr
			}

			return supervised.Bound{Camera: camera, Printer: printer}, nil
		})
}

// printerOf reads the printer this camera watches, and the empty string for one
// that watches none.
//
// A camera that names no printer is legal. A camera has a device row of its
// own, so one bound to nothing carries its own grants and is reachable. A host
// camera over V4L2 and an IP camera have no printer.
//
// A key that is there has to be a string and an identifier. A misspelt binding
// puts the camera under the wrong printer's grants, so it is refused when the
// file is read.
func printerOf(spec registry.Spec, settings map[string]any) (string, error) {
	value, declared := settings[printerKey]
	if !declared {
		return "", nil
	}

	printer, isString := value.(string)
	if !isString {
		return "", fmt.Errorf("%w: %s: config.%s is a %T and a printer id is a string",
			ErrAdapterConfig, spec.Ref, printerKey, value)
	}

	if err := ref.ValidateID(printer); err != nil {
		return "", fmt.Errorf("%w: %s: config.%s: %w", ErrAdapterConfig, spec.Ref, printerKey, err)
	}

	return printer, nil
}

// cameraSettingsOf is printerOf over an encoded block, for a caller validating
// a declaration before anything is constructed.
func cameraSettingsOf(spec registry.Spec) (string, error) {
	settings, err := openSettings(spec)
	if err != nil {
		return "", err
	}

	return printerOf(spec, settings)
}

// CameraPrinter reports the printer a camera declaration binds itself to, and
// the empty string for one that binds itself to none.
//
// It is exported because the binding is the parent edge in the authorization
// graph, and the reconcile that writes that edge runs before anything is
// constructed. The host reads the key for the reason printerKey gives: a driver
// that named it would be choosing who sees its frames.
func CameraPrinter(instance ref.Ref, config json.RawMessage) (string, error) {
	return cameraSettingsOf(registry.Spec{Ref: instance, Config: config})
}

// The two host-owned keys a camera's snapshot history is written under. They
// are the host's for the reason printerKey is: what is recorded and where it
// lands are the deployment's decisions, not the driver's.
const (
	historyEveryKey   = "snapshot_every"
	historyStorageKey = "snapshot_storage"
)

// historyStorageDefault is the storage a recording is written to when the
// declaration names none. It is the name the shipped configuration gives the
// deployment's own disk, so the common case needs one key.
const historyStorageDefault = "local"

// CameraHistory is the periodic capture one camera declares: how often a still
// image is stored, and which storage instance it is stored in.
//
// The zero value is off, and off is the default. A recording fills a disk for
// as long as the deployment runs, so an operator switches it on per camera.
type CameraHistory struct {
	// Interval is the gap between two stored snapshots. Zero is off.
	Interval time.Duration
	// Storage is the instance name the images are written into.
	Storage string
}

// On reports whether this declaration asks for a recording at all.
func (h CameraHistory) On() bool { return h.Interval > 0 }

// CameraHistoryOf reads a camera declaration's snapshot history.
//
// It is exported because the recorder that runs the capture is in the host and
// not in the driver.
//
// A cadence that is not a duration is refused. An operator who wrote "10"
// meaning ten seconds has written something Go reads as ten nanoseconds, and a
// recording at that cadence fills the disk.
func CameraHistoryOf(instance ref.Ref, config json.RawMessage) (CameraHistory, error) {
	spec := registry.Spec{Ref: instance, Config: config}

	settings, err := openSettings(spec)
	if err != nil {
		return CameraHistory{}, err
	}

	every, err := historyIntervalOf(spec, settings)
	if err != nil || every <= 0 {
		return CameraHistory{}, err
	}

	storage := historyStorageDefault

	if value, declared := settings[historyStorageKey]; declared {
		named, isString := value.(string)
		if !isString {
			return CameraHistory{}, fmt.Errorf("%w: %s: config.%s is a %T and a storage name is a string",
				ErrAdapterConfig, spec.Ref, historyStorageKey, value)
		}

		if named != "" {
			storage = named
		}
	}

	return CameraHistory{Interval: every, Storage: storage}, nil
}

// historyIntervalOf reads the cadence, and zero for a declaration that names
// none. A number is refused: the only spelling this accepts is a Go duration,
// the spelling every other cadence in the configuration takes.
func historyIntervalOf(spec registry.Spec, settings map[string]any) (time.Duration, error) {
	value, declared := settings[historyEveryKey]
	if !declared {
		return 0, nil
	}

	written, isString := value.(string)
	if !isString {
		return 0, fmt.Errorf("%w: %s: config.%s is a %T and a cadence is a duration such as \"30s\"",
			ErrAdapterConfig, spec.Ref, historyEveryKey, value)
	}

	if written == "" {
		return 0, nil
	}

	every, err := time.ParseDuration(written)
	if err != nil {
		return 0, fmt.Errorf("%w: %s: config.%s: %w", ErrAdapterConfig, spec.Ref, historyEveryKey, err)
	}

	if every < 0 {
		return 0, fmt.Errorf("%w: %s: config.%s is %s and a cadence cannot be negative",
			ErrAdapterConfig, spec.Ref, historyEveryKey, written)
	}

	return every, nil
}
