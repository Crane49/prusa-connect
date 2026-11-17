package resource

import (
	"context"
	"slices"
	"strconv"
	"strings"

	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpfault"
)

// The bounds of a stored preset. The number of steps and their length are the
// bounds of a gcode sequence on the wire. They are checked at the write,
// because a preset is stored before the printer it will run on is known, and
// a stored step over the bound could never be run.
const (
	// maxPresetSteps is the sequence bound. It is the transport's constant,
	// because a preset is a sequence and the two numbers must be the same.
	maxPresetSteps      = httpapi.MaxGCodeSteps
	maxPresetStepBytes  = 256
	maxPresetModels     = 16
	maxPresetModelBytes = 63
	maxSummaryBytes     = 256

	// The range of printable ASCII. One step must be one command. A stored
	// step with a line break would be two commands, and the host checks and
	// refuses commands one by one. The driver applies its own refusals again
	// at the wire.
	firstPrintable = 0x20
	lastPrintable  = 0x7E
)

var _ httpapi.GCodePresetStore = (*Store)(nil)

// presetKind is the authorization object every preset grant is written on.
// There is no object per preset: a grant on the kind covers every preset, as
// for every other resource kind.
const presetKind = ref.KindGCodePreset

// ListPresets returns every preset the caller may read, ordered by name.
func (s *Store) ListPresets(ctx context.Context, principal string) (httpapi.GCodePresetListing, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	who, err := s.subject(principal)
	if err != nil {
		return httpapi.GCodePresetListing{}, err
	}

	// The graph is asked before the disk is read, as in every other read of
	// this store, so the access answer cannot depend on the configuration.
	allowed, err := s.mayReadPresets(ctx, who)
	if err != nil {
		return httpapi.GCodePresetListing{}, err
	}

	read, err := s.readPresets(ctx, principal, "list presets")
	if err != nil {
		return httpapi.GCodePresetListing{}, err
	}

	listing := httpapi.GCodePresetListing{Items: []httpapi.GCodePreset{}}

	// A caller who may read no preset gets no token. The version covers the
	// whole overlay, so it changes whenever anybody writes a preset or a
	// resource instance. A caller with no grant must not learn from it when
	// the configuration changes. Such a caller also needs no token, because a
	// preset write needs a grant they do not hold.
	if !allowed {
		return listing, nil
	}

	listing.Version = read.version

	for _, stored := range read.presets {
		listing.Items = append(listing.Items, stored.project(read.version))
	}

	return listing, nil
}

// GetPreset returns one preset. A caller with no grant on the kind gets the
// same answer, byte for byte, as a caller naming a preset that does not
// exist.
func (s *Store) GetPreset(
	ctx context.Context, principal string, name string,
) (httpapi.GCodePreset, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	who, err := s.subject(principal)
	if err != nil {
		return httpapi.GCodePreset{}, err
	}

	allowed, err := s.mayReadPresets(ctx, who)
	if err != nil {
		return httpapi.GCodePreset{}, err
	}

	if !allowed {
		return httpapi.GCodePreset{}, presetAbsent(name)
	}

	read, err := s.readPresets(ctx, principal, "get preset")
	if err != nil {
		return httpapi.GCodePreset{}, err
	}

	stored, found := findPreset(read.presets, name)
	if !found {
		return httpapi.GCodePreset{}, presetAbsent(name)
	}

	return stored.project(read.version), nil
}

// LookupPreset reads a preset and checks no grant on the preset kind. The run
// path calls it. Running a preset needs send_gcode on the printer and no
// grant on the preset kind. The transport authorizes the printer before it
// calls this, so a caller with no grant does not learn whether a name
// exists.
func (s *Store) LookupPreset(ctx context.Context, name string) (httpapi.GCodePreset, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	read, err := s.readPresets(ctx, "", "run preset")
	if err != nil {
		return httpapi.GCodePreset{}, err
	}

	stored, found := findPreset(read.presets, name)
	if !found {
		return httpapi.GCodePreset{}, presetAbsent(name)
	}

	return stored.project(read.version), nil
}

// ApplyPreset writes one preset into the overlay. Every refusal happens before
// anything is written, so a rejected preset leaves the file unchanged.
func (s *Store) ApplyPreset(
	ctx context.Context,
	principal string,
	name string,
	desired httpapi.GCodePresetSpec,
	pre httpapi.Precondition,
) (httpapi.GCodePresetResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	write, err := s.beginPreset(ctx, principal, name, pre, true)
	if err != nil {
		return httpapi.GCodePresetResult{}, err
	}

	stored, err := validPreset(name, desired)
	if err != nil {
		return httpapi.GCodePresetResult{}, err
	}

	if pre.DryRun {
		return httpapi.GCodePresetResult{
			Preset: stored.project(write.doc.Version), Created: !write.exists,
		}, nil
	}

	next := write.overlay
	next.Presets = withPreset(write.overlay.Presets, stored)

	version, err := s.commit(ctx, next, write.read)
	if err != nil {
		return httpapi.GCodePresetResult{}, err
	}

	return httpapi.GCodePresetResult{Preset: stored.project(version), Created: !write.exists}, nil
}

// DeletePreset removes one preset from the overlay.
func (s *Store) DeletePreset(
	ctx context.Context, principal string, name string, pre httpapi.Precondition,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	write, err := s.beginPreset(ctx, principal, name, pre, false)
	if err != nil {
		return err
	}

	if pre.DryRun {
		return nil
	}

	next := write.overlay
	next.Presets = withoutPreset(write.overlay.Presets, name)

	_, err = s.commit(ctx, next, write.read)

	return err
}

// pendingPreset is one preset write that passed its refusals and is not
// committed yet.
type pendingPreset struct {
	read    inputs
	doc     Document
	overlay overlayFile
	exists  bool
}

// beginPreset authorizes the write and applies the two refusals of the store:
// a name that does not exist, and a token that does not match the current
// version. The graph is asked before the disk is read, so an access refusal
// cannot depend on the file. A caller who may not read presets therefore gets
// the same answer as for a preset that does not exist.
func (s *Store) beginPreset(
	ctx context.Context, principal string, name string, pre httpapi.Precondition, mayCreate bool,
) (pendingPreset, error) {
	s.log.DebugContext(ctx, "preset write",
		"op", writeName(mayCreate), "principal", principal, "preset", name)

	who, err := s.subject(principal)
	if err != nil {
		return pendingPreset{}, err
	}

	if err = s.authorizePresetWrite(ctx, who, name, mayCreate); err != nil {
		return pendingPreset{}, err
	}

	read, err := readInputs(s.paths)
	if err != nil {
		return pendingPreset{}, s.internal("read the configuration", err)
	}

	doc, err := document(read)
	if err != nil {
		return pendingPreset{}, s.internal("parse the configuration", err)
	}

	overlay, err := parseOverlay(read.overlay)
	if err != nil {
		return pendingPreset{}, s.internal("parse the overlay", err)
	}

	_, exists := findPreset(overlay.Presets, name)

	switch {
	case !exists && !pre.MustNotExist && len(pre.IfNoneMatch) == 0:
		return pendingPreset{}, presetAbsent(name)
	case !pre.Holds(exists, doc.Version):
		return pendingPreset{}, httpfault.VersionConflict(httpfault.Reference(presetKind.String(), name), doc.Version)
	}

	return pendingPreset{read: read, doc: doc, overlay: overlay, exists: exists}, nil
}

// authorizePresetWrite is the access check for a preset write. It needs
// configure_resource on the preset kind. A caller who can neither read nor
// configure presets gets the refusal for a preset that does not exist.
func (s *Store) authorizePresetWrite(
	ctx context.Context, who authz.Subject, name string, mayCreate bool,
) error {
	seen, err := s.mayReadPresets(ctx, who)
	if err != nil {
		return err
	}

	writes, err := s.writable(ctx, who, presetKind)
	if err != nil {
		return err
	}

	instance, err := ref.New(presetKind, name)
	if err != nil {
		// The name is checked after the grants. A caller with no grant gets
		// the not-found answer for a malformed name too, and learns nothing
		// from it.
		if !seen && !writes {
			return presetAbsent(name)
		}

		return httpfault.RequestInvalid("path.name",
			"expected a DNS-1123 label: lowercase letters, digits and hyphens", err.Error())
	}

	switch {
	case writes && (seen || mayCreate):
		return nil
	case seen:
		return authz.Denied(authz.ActionConfigureResource, instance)
	default:
		return presetAbsent(name)
	}
}

// mayReadPresets is the access check for a read: view_resource on the preset
// kind.
func (s *Store) mayReadPresets(ctx context.Context, who authz.Subject) (bool, error) {
	object, err := authz.NewObject(authz.TypeResourceKind, presetKind.String())
	if err != nil {
		return false, s.internal("name the preset kind", err)
	}

	allowed, err := s.graph.Can(ctx, who, authz.ActionViewResource, object)
	if err != nil {
		return false, s.internal("read what the caller may see", err)
	}

	return allowed, nil
}

// presetRead is the overlay as one read saw it.
type presetRead struct {
	presets []overlayPreset
	version string
}

// readPresets serves what is on disk and not what was last reconciled, for
// the same reason as reread: a client whose write was refused for an old
// token must be able to read the current token.
func (s *Store) readPresets(ctx context.Context, principal, op string) (presetRead, error) {
	s.log.DebugContext(ctx, "preset read", "op", op, "principal", principal)

	read, err := readInputs(s.paths)
	if err != nil {
		return presetRead{}, s.internal("read the configuration", err)
	}

	overlay, err := parseOverlay(read.overlay)
	if err != nil {
		return presetRead{}, s.internal("parse the overlay", err)
	}

	presets := slices.Clone(overlay.Presets)
	slices.SortFunc(presets, func(a, b overlayPreset) int { return strings.Compare(a.Name, b.Name) })

	return presetRead{presets: presets, version: read.version}, nil
}

// validPreset checks a declared preset and returns it in the stored form. It
// is the only check at write time. It does not check what a printer accepts,
// because the printer the preset will run on may not be known yet. Every step
// is checked again against that printer's capability set at run time.
func validPreset(name string, desired httpapi.GCodePresetSpec) (overlayPreset, error) {
	if _, err := ref.New(presetKind, name); err != nil {
		return overlayPreset{}, httpfault.RequestInvalid("path.name",
			"expected a DNS-1123 label: lowercase letters, digits and hyphens", err.Error())
	}

	if len(desired.Summary) > maxSummaryBytes {
		return overlayPreset{}, httpfault.RequestInvalid("body.summary",
			"expected at most "+strconv.Itoa(maxSummaryBytes)+" bytes",
			"the summary is "+strconv.Itoa(len(desired.Summary))+" bytes")
	}

	steps, err := validSteps(desired.Steps)
	if err != nil {
		return overlayPreset{}, err
	}

	models, err := validModels(desired.Models)
	if err != nil {
		return overlayPreset{}, err
	}

	return overlayPreset{
		Name:    name,
		Summary: desired.Summary,
		Models:  models,
		Steps:   steps,
		Force:   desired.Force,
	}, nil
}

func validSteps(declared []string) ([]string, error) {
	if len(declared) == 0 || len(declared) > maxPresetSteps {
		return nil, httpfault.RequestInvalid("body.steps",
			"expected between 1 and "+strconv.Itoa(maxPresetSteps)+" steps",
			"steps: "+strconv.Itoa(len(declared)))
	}

	steps := make([]string, 0, len(declared))

	for index, step := range declared {
		at := "body.steps[" + strconv.Itoa(index) + "]"

		trimmed := strings.TrimSpace(step)
		if trimmed == "" {
			return nil, httpfault.RequestInvalid("body.steps",
				"expected one gcode command", at+" is empty")
		}

		if len(trimmed) > maxPresetStepBytes {
			return nil, httpfault.RequestInvalid("body.steps",
				"expected at most "+strconv.Itoa(maxPresetStepBytes)+" bytes per step",
				at+" is "+strconv.Itoa(len(trimmed))+" bytes")
		}

		if offending, ok := unprintable(trimmed); ok {
			return nil, httpfault.RequestInvalid("body.steps",
				"expected printable ASCII, so that one step is one command",
				at+" contains "+strconv.QuoteRune(offending))
		}

		steps = append(steps, trimmed)
	}

	return steps, nil
}

func validModels(declared []string) ([]string, error) {
	if len(declared) > maxPresetModels {
		return nil, httpfault.RequestInvalid("body.models",
			"expected at most "+strconv.Itoa(maxPresetModels)+" models",
			"models: "+strconv.Itoa(len(declared)))
	}

	if len(declared) == 0 {
		return nil, nil
	}

	models := make([]string, 0, len(declared))
	seen := make(map[string]struct{}, len(declared))

	for index, model := range declared {
		at := "body.models[" + strconv.Itoa(index) + "]"

		trimmed := strings.TrimSpace(model)
		if trimmed == "" || len(trimmed) > maxPresetModelBytes {
			return nil, httpfault.RequestInvalid("body.models",
				"expected a model name of 1 to "+strconv.Itoa(maxPresetModelBytes)+" bytes",
				at+" is empty or oversized")
		}

		if offending, ok := unprintable(trimmed); ok {
			return nil, httpfault.RequestInvalid("body.models",
				"expected printable ASCII", at+" contains "+strconv.QuoteRune(offending))
		}

		folded := strings.ToLower(trimmed)
		if _, taken := seen[folded]; taken {
			return nil, httpfault.RequestInvalid("body.models",
				"expected each model once; the comparison ignores case",
				at+" repeats "+strconv.Quote(trimmed))
		}

		seen[folded] = struct{}{}

		models = append(models, trimmed)
	}

	return models, nil
}

// unprintable returns the first character outside printable ASCII.
func unprintable(value string) (rune, bool) {
	for _, r := range value {
		if r < firstPrintable || r > lastPrintable {
			return r, true
		}
	}

	return 0, false
}

// withPreset returns the list with one preset written. It replaces an entry
// with the same name.
func withPreset(presets []overlayPreset, stored overlayPreset) []overlayPreset {
	next := make([]overlayPreset, 0, len(presets)+1)
	replaced := false

	for _, existing := range presets {
		if existing.Name == stored.Name {
			next = append(next, stored)
			replaced = true

			continue
		}

		next = append(next, existing)
	}

	if !replaced {
		next = append(next, stored)
	}

	return next
}

// withoutPreset returns the list without the preset of one name.
func withoutPreset(presets []overlayPreset, name string) []overlayPreset {
	next := make([]overlayPreset, 0, len(presets))

	for _, existing := range presets {
		if existing.Name == name {
			continue
		}

		next = append(next, existing)
	}

	return next
}

func findPreset(presets []overlayPreset, name string) (overlayPreset, bool) {
	for _, stored := range presets {
		if stored.Name == name {
			return stored, true
		}
	}

	return overlayPreset{}, false
}

// project renders one stored preset as the API returns it.
func (p overlayPreset) project(version string) httpapi.GCodePreset {
	return httpapi.GCodePreset{
		GCodePresetSpec: httpapi.GCodePresetSpec{
			Summary: p.Summary,
			Models:  slices.Clone(p.Models),
			Steps:   slices.Clone(p.Steps),
			Force:   p.Force,
		},
		Name:    p.Name,
		Version: version,
	}
}

// presetAbsent is the refusal for a preset that does not exist and for a
// caller who may not read presets. Both cases call this function, so the two
// answers are always the same.
func presetAbsent(name string) error {
	return httpfault.ResourceNotFound(httpfault.Reference(presetKind.String(), name))
}

func writeName(mayCreate bool) string {
	if mayCreate {
		return "apply"
	}

	return "delete"
}
