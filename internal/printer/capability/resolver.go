// Package capability resolves what one printer can actually do, and holds the
// host to it.
//
// Three tiers are merged by intersection and never by union: the
// driver's compiled baseline is the ceiling, the embedded profile narrows it,
// and the device's own probe narrows it again. The worst a wrong or stale
// profile can produce is a missing capability.
//
// A printer no profile covers gets the baseline bounded by what this driver's
// profiles cover between them. A driver that ships no profiles keeps its whole
// baseline. That is the one case this resolver cannot narrow.
//
// The firmware version reaches two places: this resolver, to select a profile,
// and telemetry, to be displayed. connectorapi.FirmwareLabel is a struct so
// that < and > do not compile against it. String is exported for the two
// readers above, and a comparison of two String results would order "6.4.0"
// above "6.10.0". A forbidigo rule on that method and two gates in
// internal/arch refuse such a comparison. This package selects a profile
// through profile.Registry, which parses the components as numbers.
package capability

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/prusa3d/prusa-connect/internal/kernel/condition"
	"github.com/prusa3d/prusa-connect/internal/printer/profile"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// TypeCapabilitiesResolved reports whether a printer's capability set was
// resolved against a profile or fell back to the driver's baseline.
const TypeCapabilitiesResolved condition.Type = "CapabilitiesResolved"

// The reasons TypeCapabilitiesResolved carries.
const (
	// ReasonProfileMatched means a profile covered the reported identity.
	ReasonProfileMatched = "ProfileMatched"
	// ReasonProfileUnmatched means none did, so what the driver's profiles cover
	// between them is the answer.
	ReasonProfileUnmatched = "ProfileUnmatched"
	// ReasonNoProfiles means the driver ships no profiles at all, so its
	// baseline stands unnarrowed.
	ReasonNoProfiles = "NoProfiles"
)

// Basis names the tier that bounded a resolution below the driver's baseline.
type Basis uint8

// The bases a resolution can have, from the widest answer to the narrowest. The
// zero value is BasisBaseline, so a Resolution nothing filled in does not claim
// a profile it never had.
const (
	// BasisBaseline means nothing narrowed the driver's baseline, which happens
	// only when the driver ships no profiles.
	BasisBaseline Basis = iota
	// BasisCoverage means no profile covered the identity, so what the driver's
	// profiles cover between them bounded the set.
	BasisCoverage
	// BasisProfile means a profile covered the identity.
	BasisProfile
)

// String returns the basis as the condition reason that carries it.
func (b Basis) String() string {
	switch b {
	case BasisProfile:
		return ReasonProfileMatched
	case BasisCoverage:
		return ReasonProfileUnmatched
	case BasisBaseline:
		return ReasonNoProfiles
	default:
		return "Unknown"
	}
}

// Tiers are the sources of a capability description, in the order they narrow.
//
// A nil tier is absent and narrows nothing. An empty non-nil tier declares
// nothing and narrows everything away. Nil means no profile covers this
// firmware, which is the common case. Empty means this firmware exposes
// nothing, which is a deliberate refusal.
type Tiers struct {
	// Baseline is what the driver's code can do at all. It is the ceiling.
	Baseline connectorapi.Set
	// Profile is the data tier, or nil when nothing matched.
	Profile connectorapi.Set
	// Coverage stands in for Profile when nothing matched: what this driver's
	// profiles describe between them, from profile.Registry.Coverage.
	//
	// It is read only while Profile is nil, so what other models declare never
	// widens a matched profile. It is nil for a driver that ships no profiles,
	// and the baseline then stays whole.
	Coverage connectorapi.Set
	// Probe is what this unit answered about itself, or nil when it was not
	// asked or said nothing.
	Probe connectorapi.Set
}

// Resolve merges the tiers. The result is bounded by Baseline whatever the
// others contain.
func Resolve(tiers Tiers) connectorapi.Set {
	resolved := tiers.Baseline.Clone()
	if resolved == nil {
		resolved = connectorapi.Set{}
	}

	data := tiers.Profile
	if data == nil {
		data = tiers.Coverage
	}

	if data != nil {
		resolved = resolved.Intersect(data)
	}

	if tiers.Probe != nil {
		resolved = resolved.Intersect(tiers.Probe)
	}

	return resolved
}

// Resolution is one printer's resolved capability set and where it came from.
type Resolution struct {
	// Capabilities is the enabled set for this epoch.
	Capabilities connectorapi.Set
	// Epoch changes whenever Capabilities does, so a cached copy can be told
	// apart from a current one.
	Epoch uint64
	// Digest is the stable hash of Capabilities, carried in the birth event and
	// in the log so "which capabilities were in force" is a lookup.
	Digest string
	// Model and Firmware are the identity a profile was looked for under.
	Model    string
	Firmware connectorapi.FirmwareLabel
	// ProfileMatched is whether one covered that identity. False means a
	// reduced capability set and not a refusal. CoverageApplied says what
	// bounded it.
	ProfileMatched bool
	// CoverageApplied is whether what this driver's profiles cover between them
	// bounded the set, which is the answer for an identity no profile covers.
	// Both flags false means the driver ships no profiles at all and its
	// baseline stands whole, which Basis names.
	CoverageApplied bool
	// Pruned names the keys the merge produced that the descriptor rules refuse.
	// They are removed from Capabilities. A correct baseline and profile leave
	// it empty.
	Pruned []connectorapi.Capability
	// ProfileRange is the range that matched, empty when none did.
	ProfileRange profile.Range
}

// Basis reports which tier bounded the driver's baseline. It is derived from
// the two flags and not stored, so a reader and the condition cannot disagree
// about one resolution.
func (r Resolution) Basis() Basis {
	switch {
	case r.ProfileMatched:
		return BasisProfile
	case r.CoverageApplied:
		return BasisCoverage
	default:
		return BasisBaseline
	}
}

// Condition reports the resolution in the shape a printer's status carries.
func (r Resolution) Condition(now time.Time, generation int64) condition.Condition {
	basis := r.Basis()

	c := condition.Condition{
		Type:               TypeCapabilitiesResolved,
		Status:             condition.StatusTrue,
		Reason:             basis.String(),
		Message:            "resolved " + strconv.Itoa(len(r.Capabilities)) + " capabilities, digest " + r.Digest,
		LastTransitionTime: now,
		ObservedGeneration: generation,
	}

	switch basis {
	case BasisProfile:
	case BasisCoverage:
		c.Status = condition.StatusFalse
		c.Message = "no profile covers " + r.Model + " firmware " + r.Firmware.String() +
			", so this driver's profiles bound it between them"
	case BasisBaseline:
		c.Status = condition.StatusFalse
		c.Message = "this driver ships no capability profiles, so its whole baseline is in force for " +
			r.Model + " firmware " + r.Firmware.String()
	}

	if len(r.Pruned) > 0 {
		c.Message += ", after removing " + strconv.Itoa(len(r.Pruned)) +
			" merged capabilities the descriptor rules refuse"
	}

	return c
}

// Resolver turns a reported identity and a driver baseline into a resolution.
//
// The registry it is built over is one driver's profiles, because they bound a
// device the registry does not cover. No identity names a driver and no
// profile records one, so the host sets the scope by the registry it hands in.
//
// It also holds each unit's capability epoch. The epoch tells a caller that
// the set it cached belongs to an activation that has ended, so it must
// outlive the session. A counter kept in the session would start again at the
// number the caller is holding.
type Resolver struct {
	profiles profile.Registry
	// coverage is what those profiles describe between them, computed once. A
	// registry is immutable, and computing it on every unmatched arrival would
	// union the whole file each time.
	coverage connectorapi.Set
	log      *slog.Logger

	mu     sync.Mutex
	epochs map[string]uint64
}

// coveredModelsLogged bounds the model list the unmatched warning carries. A
// driver's profile file can list any number of models.
const coveredModelsLogged = 32

// coveredModels is the models a warning names. A model reported with a
// spelling no profile carries is the usual way to reach this warning, and the
// two spellings side by side show the cause.
func coveredModels(profiles profile.Registry) []string {
	models := profiles.Models()
	if len(models) <= coveredModelsLogged {
		return models
	}

	out := make([]string, 0, coveredModelsLogged+1)
	out = append(out, models[:coveredModelsLogged]...)

	return append(out, "and "+strconv.Itoa(len(models)-coveredModelsLogged)+" more")
}

// NewResolver returns a resolver over one driver's profile registry.
func NewResolver(profiles profile.Registry, log *slog.Logger) *Resolver {
	return &Resolver{
		profiles: profiles,
		coverage: profiles.Coverage(),
		log:      log,
		epochs:   map[string]uint64{},
	}
}

// Arrive resolves a printer that has just connected, on an epoch of its own.
// It is what a host calls when a device is admitted; Resolve is the same merge
// at an epoch the caller already has.
//
// The epoch moves on every arrival, also when the merged set is the same. A
// disconnection this host did not watch can carry a firmware update, a
// different probe answer or a corrected profile, so the host cannot promise
// that nothing changed. A caller that asks whether the set itself changed
// compares Resolution.Digest.
func (r *Resolver) Arrive(
	ctx context.Context,
	baseline connectorapi.Set,
	identity connectorapi.Identity,
	probe connectorapi.Set,
) Resolution {
	return r.Resolve(ctx, baseline, identity, probe, r.nextEpoch(identity))
}

// Resolve selects a profile for the reported identity and merges the tiers, at
// the epoch the caller names. A printer that has just connected is resolved with
// Arrive, which is what moves the epoch on.
//
// An identity nothing covers is not an error. It is held to what this driver's
// profiles cover between them, which is never wider than the baseline. The
// identity is logged as a warning, because it tells a maintainer to record a
// transcript and add a profile. That is a change to data and not to code.
func (r *Resolver) Resolve(
	ctx context.Context,
	baseline connectorapi.Set,
	identity connectorapi.Identity,
	probe connectorapi.Set,
	epoch uint64,
) Resolution {
	resolution := Resolution{
		Epoch:    epoch,
		Model:    identity.Model,
		Firmware: identity.Firmware,
	}

	tiers := Tiers{Baseline: baseline, Probe: probe}

	matched, ok := r.profiles.Match(identity.Model, identity.Firmware)

	switch {
	case ok:
		tiers.Profile = matched.Capabilities
		resolution.ProfileMatched = true
		resolution.ProfileRange = matched.Firmware

		// Both spellings go on one line. A printer that reports a device type
		// code or its own spacing is matched, and the person at the machine
		// must still see which profile it landed on.
		if !strings.EqualFold(matched.Model, identity.Model) {
			r.log.InfoContext(ctx,
				"this printer reported a model spelling the profile file does not use",
				slog.String("model", identity.Model),
				slog.String("profile_model", matched.Model),
				slog.String("firmware", identity.Firmware.String()),
				slog.String("serial", identity.Serial))
		}
	case r.coverage != nil:
		tiers.Coverage = r.coverage
		resolution.CoverageApplied = true

		r.log.WarnContext(ctx,
			"no capability profile covers this printer, holding it to what this driver's profiles cover",
			slog.String("model", identity.Model),
			slog.String("firmware", identity.Firmware.String()),
			slog.String("serial", identity.Serial),
			slog.Any("covered", coveredModels(r.profiles)))
	default:
		r.log.WarnContext(ctx,
			"this driver ships no capability profiles, so its whole baseline is in force for this printer",
			slog.String("model", identity.Model),
			slog.String("firmware", identity.Firmware.String()),
			slog.String("serial", identity.Serial))
	}

	resolution.Capabilities, resolution.Pruned = r.prune(ctx, identity, Resolve(tiers))
	resolution.Digest = resolution.Capabilities.Digest()

	return resolution
}

// nextEpoch returns the epoch for one unit's next activation. Epochs are per
// unit and only grow. A unit nothing has seen yet starts at one, so zero is
// never a live epoch and a zero-valued copy is always stale.
//
// Nothing forgets a unit. A forgotten epoch would be handed out a second time,
// and a set cached under the first one would look current again. The table
// costs one counter per unit the host has ever admitted.
func (r *Resolver) nextEpoch(identity connectorapi.Identity) uint64 {
	key := unitKey(identity)

	r.mu.Lock()
	defer r.mu.Unlock()

	r.epochs[key]++

	return r.epochs[key]
}

// unitKey names the device an epoch counts for. UnitUID is the identifier the
// handshake carries where the firmware has one; everything else reports a
// serial, and the vendor and model keep two dialects' serial numbering apart.
func unitKey(identity connectorapi.Identity) string {
	if identity.UnitUID != "" {
		return identity.UnitUID
	}

	return identity.Vendor + "\x00" + identity.Model + "\x00" + identity.Serial
}

// prune runs the merged set through the descriptor rules and removes what they
// refuse. It returns the set the host will command against and the keys it
// removed.
//
// Each tier is checked against those rules on its own: a profile when the file
// loads, a driver's baseline in the conformance suite. Nothing checks what
// they merge to, and a merge can subtract a key another key depends on.
//
// A key the rules refuse is removed and not only logged, because an API route
// could still reach a key that stays in the set. A merge may subtract and may
// never add, so the pruned set is never wider than the merge. Refusing the
// whole set would leave a printer that accepts no command because of one
// unusable key.
func (r *Resolver) prune(
	ctx context.Context, identity connectorapi.Identity, merged connectorapi.Set,
) (connectorapi.Set, []connectorapi.Capability) {
	sound, removed, problems := merged.Prune()
	if len(removed) == 0 {
		return sound, nil
	}

	r.log.ErrorContext(ctx, "the merged capability set breaks the descriptor rules, and those keys were removed",
		slog.String("model", identity.Model),
		slog.String("firmware", identity.Firmware.String()),
		slog.String("serial", identity.Serial),
		slog.Int("removed", len(removed)),
		slog.Any("problems", errors.Join(problems...)))

	return sound, removed
}
