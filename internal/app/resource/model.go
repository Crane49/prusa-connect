// Package resource is the configuration store for resource instances: the
// operator's hand-written file, the machine-owned overlay the API writes, and
// the merge of the two that the supervision plane reconciles against.
//
// # Two inputs, one document
//
// The API never rewrites the operator's file. No Go YAML library writes a
// hand-written file back without changing its comments and layout, so a write
// would make the operator's own edits hard to see in a diff. Writes go to a
// JSON overlay this package owns, and Load merges the two.
//
// The operator's file wins. An instance declared in both is reported with
// source "file" and the API refuses to write it, as Grafana does with
// allowUiUpdates: false. An overlay entry shadowed by a file declaration stays
// in the overlay and is named in Document.Shadowed, so an operator can see it.
package resource

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/prusa3d/prusa-connect/internal/app/config/install"
	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/sdk/extension"
)

// Source names which of the two inputs declared an instance. It is on the wire,
// so a client can see that an instance is not writable before a write is
// refused.
type Source string

const (
	// SourceFile is the operator's hand-written configuration file.
	SourceFile Source = "file"
	// SourceOverlay is the machine-owned file the API writes.
	SourceOverlay Source = "overlay"
)

// String returns the source as it is rendered on the wire.
func (s Source) String() string { return string(s) }

// Spec is one instance's declaration: everything either input holds for it. It
// holds no state the supervision plane observes.
type Spec struct {
	// Type is the registered implementation this instance is built from. It is
	// fixed at creation.
	Type string
	// Enabled is the operator's switch.
	Enabled bool
	// Required says the process must not serve while this instance is not
	// ready.
	Required bool
	// Runtime is how the type is reached: compiled into this binary, launched
	// as a supervised child process, run as a script, or served as files.
	// internal/app/config/install defines the values and the rules for them,
	// because the build-tag detector reads the same declaration before a
	// binary exists to run this loader.
	//
	// The zero value reads as install.RuntimeBuiltin everywhere it is compared,
	// so a declaration without the key and one that names builtin have the
	// same digest.
	Runtime install.Runtime
	// Service is where the child process is found and the checksum that proves
	// it is the one that was installed. It is nil for every instance whose
	// Runtime is not service.
	Service *install.Service
	// Script is the expression the host compiles and runs over every event. It
	// is empty for every instance whose Runtime is not script.
	Script string
	// Asset is where a module that runs no process keeps its files and the
	// checksum that proves they are the ones installed. It is nil for every
	// instance whose Runtime is not asset.
	Asset *install.Asset
	// Module is what the module's own module.yaml said about itself when it
	// was installed, such as the release, a summary and the author. It is nil
	// for an instance that is not a module, and for a declaration whose
	// install recorded none.
	Module *install.ModuleEntry
	// Config is the implementation's own settings, in the canonical JSON form
	// canonicalJSON produces, so two declarations of the same settings compare
	// equal whichever input carried them.
	Config json.RawMessage
	// Reads is the query verbs this instance answers, for a kind where a read
	// has to pick one instance out of several.
	//
	// An empty list claims every verb the instance's capabilities carry. Two
	// instances of one kind with an empty list therefore conflict. It is a
	// host key and not a setting, because the host resolves a query, and an
	// adapter must not decide which instance of its kind answers.
	Reads []string
	// HiddenContributions are the module UI contributions hidden for every
	// viewer, each as <slot key>/<identity>. It only changes what is shown. It
	// is neither in Equal nor in Digest, so changing it neither restarts the
	// instance nor raises its generation.
	HiddenContributions []string
	// SwitchedOff is the host's record of switching a service module off
	// because it failed too often. It is set by the host only, kept in the
	// overlay, and cleared when the operator switches the module on. It is
	// neither in Equal nor in Digest: Enabled carries the change.
	SwitchedOff *SwitchOff
}

// Declaration is a Spec with the identity it was declared under and the input
// that carried it.
type Declaration struct {
	// Ref is the operator-chosen name.
	Ref ref.Ref
	// UID is the identity the host creates. It is zero until a load has
	// created one: after an operator adds a block to the file by hand and
	// before the next load.
	UID ref.UID
	// Spec is the declaration.
	Spec Spec
	// Source is the input that carried it.
	Source Source
	// Generation counts accepted changes to this declaration. The supervision
	// plane reports the generation it last acted on, so a caller can tell
	// whether the last change has taken effect.
	Generation int64
}

// ModuleNamespace is the namespace a declaration's module surface is served
// under, and false for a declaration that is not a module.
//
// It is derived from the type name. The type is what the operator installed,
// what the checksum is recorded for, and what the artefact file is named
// after. A module passes no namespace in any call, so no module can use
// another module's namespace.
//
// A type name with no namespace belongs to the core and is never a module. A
// qualified name whose namespace is not valid is refused. It is not rewritten,
// because a rewritten name could collide with another module's.
func (d Declaration) ModuleNamespace() (string, bool) {
	if !d.Spec.module() {
		return "", false
	}

	name := strings.TrimSpace(d.Spec.Type)
	if !extension.IsQualified(name) {
		return "", false
	}

	namespace, _, _ := strings.Cut(name, extension.QualifiedSeparator)
	if extension.Namespace(namespace).Validate() != nil {
		return "", false
	}

	return namespace, true
}

// ModuleMount is the root prefix an operator named for a module's routes, and
// empty for a declaration that is not a module or that is reached under its
// namespace. It is the operator's value as written. The supervision plane
// checks it against the router's rules before it mounts anything there.
func (d Declaration) ModuleMount() string {
	if _, isModule := d.ModuleNamespace(); !isModule || d.Spec.Service == nil {
		return ""
	}

	return d.Spec.Service.Mount
}

// Installed reports whether the server installed this declaration: a module
// in the overlay, which only `module add`, the install API and the web app
// write. A module the operator declares in their own file is not installed,
// because the operator chose it.
func (d Declaration) Installed() bool {
	return d.Spec.module() && d.Source == SourceOverlay
}

// Module reports whether this declaration is a module, whoever declared it.
func (d Declaration) Module() bool { return d.Spec.module() }

// module reports whether this declaration is a module: an artefact the
// operator installed and the host verifies, in a process of its own or as a
// directory of files it serves. Both are reached under a namespace derived
// from the type.
func (s Spec) module() bool {
	return s.Runtime == install.RuntimeService || s.Runtime == install.RuntimeAsset
}

// Failure is one declared instance that would not load, and why.
//
// A declaration the loader refuses fails only that instance. One bad block
// must not stop the server and must not stop the other blocks, of the same
// kind or of another.
type Failure struct {
	// Key is the path into the operator's file, such as resources.driver.bambu,
	// so a message points at a block the operator can find.
	Key string
	// Err is why, wrapping ErrConfigInvalid.
	Err error
}

// Document is one merged configuration: every declared instance, and the token
// a write against it must quote.
type Document struct {
	// Version is the fencing token: a sha256 over both inputs as they were
	// read, so an edit to either changes it. It also catches an operator who
	// saves the file from an editor while the API writes.
	Version string
	// Instances are the declared instances, ordered by kind then name so that
	// two loads of the same bytes produce the same listing.
	Instances []Declaration
	// Failures are the declared instances that would not load, ordered by key.
	// They are not in Instances, so they are not supervised.
	Failures []Failure
	// Shadowed names the overlay entries that a file declaration of the same
	// instance overrides. They stay in the overlay, so an operator who removes
	// the block from the file gets them back.
	Shadowed []ref.Ref
	// Endpoints are the addresses this server dials, ordered by name. The
	// operator's file declares none: the overlay is the only input that carries
	// one, because the address is something the server writes.
	Endpoints []Endpoint
}

// Find returns the declaration of one instance. The scan is linear because a
// deployment has tens of instances, not thousands. A slice keeps Document a
// plain value, so two loads can be compared field by field.
func (d Document) Find(instance ref.Ref) (Declaration, bool) {
	for _, declared := range d.Instances {
		if declared.Ref == instance {
			return declared, true
		}
	}

	return Declaration{}, false
}

// Equal reports whether two specs declare the same thing. Config is compared
// by bytes. That works because both inputs are canonicalised at load: without
// that, a key reordered by an editor would count as a change and restart the
// instance.
func (s Spec) Equal(other Spec) bool {
	return s.Type == other.Type &&
		s.Enabled == other.Enabled &&
		s.Required == other.Required &&
		s.installMode() == other.installMode() &&
		s.memoryMiB() == other.memoryMiB() &&
		s.OnOverMemory() == other.OnOverMemory() &&
		s.switchOff().Equal(other.switchOff()) &&
		bytes.Equal(s.Config, other.Config)
}

// memoryMiB is the operator's memory budget. It is in Equal and not in the
// install mode. restartOrPolicy restarts the instance for a budget edit.
func (s Spec) memoryMiB() int {
	if s.Service == nil {
		return 0
	}

	return s.Service.MemoryMiB
}

// OnOverMemory is what the host does when the module is over its memory
// budget. It is stop when the operator set nothing. Like memoryMiB it is in
// Equal and not in the install mode.
func (s Spec) OnOverMemory() string {
	if s.Service == nil || s.Service.OnOverMemory == "" {
		return install.OnOverMemoryStop
	}

	return s.Service.OnOverMemory
}

// switchOff is the operator's switch-off. Like memoryMiB it is in Equal and
// not in the install mode.
func (s Spec) switchOff() install.SwitchOff {
	if s.Service == nil {
		return install.SwitchOff{}
	}

	return s.Service.SwitchOff
}

// Digest is the declaration as one comparable string. It tells an accepted
// change from a reload of the same bytes, so a restart of the process does not
// count as an edit and raise every instance's generation.
func (s Spec) Digest() string {
	sum := sha256.Sum256(fmt.Appendf(nil, "%s\x00%t\x00%t\x00%s%s%s%s",
		s.Type, s.Enabled, s.Required, s.Config,
		s.installMode(), s.readsMode(), s.moduleFacts()))

	return hex.EncodeToString(sum[:])
}

// moduleFacts renders the release, the summary and the author from the
// manifest. An absent block renders as the empty string, so a declaration
// without the key keeps its digest.
//
// It is in the digest and not in Equal. A module reinstalled at a new release
// is an accepted change and the generation has to count it. These three
// fields alone do not restart the instance.
func (s Spec) moduleFacts() string {
	if s.Module == nil {
		return ""
	}

	return fmt.Sprintf("\x00module\x00%s\x00%s\x00%s",
		s.Module.Version, s.Module.Summary, s.Module.Author)
}

// readsMode renders the declared read verbs. The default renders as the empty
// string, so a declaration without the key keeps its digest.
func (s Spec) readsMode() string {
	if len(s.Reads) == 0 {
		return ""
	}

	return "\x00reads\x00" + strings.Join(s.Reads, ",")
}

// installMode renders how the type is reached. The default renders as the
// empty string.
//
// Two declarations that differ only in runtime are run in different ways, so
// this is in the digest: without it the plane would see no change and would
// never rebuild. Builtin renders as the empty string, so a declaration without
// the key keeps its digest, and an upgrade of the server does not raise every
// instance's generation.
func (s Spec) installMode() string {
	if s.Runtime == install.RuntimeScript {
		return fmt.Sprintf("\x00%s\x00%s", install.RuntimeScript, s.Script)
	}

	// The path and the checksum are both in the digest, so a bundle
	// reinstalled under a new checksum is rebuilt and verified again.
	if s.Runtime == install.RuntimeAsset {
		if s.Asset == nil {
			return "\x00" + install.RuntimeAsset.String()
		}

		return fmt.Sprintf("\x00%s\x00%s\x00%s", install.RuntimeAsset, s.Asset.Path, s.Asset.Checksum)
	}

	if s.Runtime != install.RuntimeService {
		return ""
	}

	if s.Service == nil {
		return "\x00" + install.RuntimeService.String()
	}

	// The mount is in the digest, so when an operator moves a module from its
	// namespace to a root prefix, or back, the instance is rebuilt and its
	// document rebased. A declaration with no mount renders nothing here.
	//
	// Every grant a module receives when it starts is in the digest too. The
	// module learns its grants only at start, so a changed grant has to
	// rebuild the instance. The database grant is left out because its DSN is
	// a credential.
	return fmt.Sprintf("\x00%s\x00%s\x00%s\x00%d\x00%s%s%s%s%s%s",
		install.RuntimeService, s.Service.Path, s.Service.Checksum, s.Service.ContractMajor,
		declaredEvents(s.Service.Events), declaredMount(s.Service.Mount),
		declaredAssets(s.Service.Assets), declaredListen(s.Service.Listen),
		declaredData(s.Service.Data), declaredGrants(s.Service))
}

// declaredGrants renders the permissions, the people and call grants and the
// record limit into the digest, each list sorted, and nothing for a module
// granted none.
func declaredGrants(service *install.Service) string {
	var out string

	if len(service.Permissions) > 0 {
		names := authz.ActionNames(service.Permissions)
		slices.Sort(names)
		out += "\x00permissions=" + strings.Join(names, ",")
	}

	if len(service.Checks) > 0 {
		names := authz.ActionNames(service.Checks)
		slices.Sort(names)
		out += "\x00checks=" + strings.Join(names, ",")
	}

	if len(service.Calls) > 0 {
		out += "\x00calls=" + strings.Join(slices.Sorted(slices.Values(service.Calls)), ",")
	}

	if service.CheckAbsentPeople {
		out += "\x00absent"
	}

	if service.ListPeople {
		out += "\x00people"
	}

	if service.RecordsMiB > 0 {
		out += "\x00records=" + strconv.Itoa(service.RecordsMiB)
	}

	return out
}

// declaredListen renders the granted ports into the digest, sorted by name, and
// nothing for a module granted none.
func declaredListen(listen map[string]install.ListenPort) string {
	if len(listen) == 0 {
		return ""
	}

	names := slices.Sorted(maps.Keys(listen))

	rendered := make([]string, 0, len(names))
	for _, name := range names {
		rendered = append(rendered, name+"="+listen[name].String())
	}

	return "\x00listen=" + strings.Join(rendered, ",")
}

// declaredData renders the writable-directory grant into the digest, and
// nothing for a module granted none.
func declaredData(data bool) string {
	if !data {
		return ""
	}

	return "\x00data"
}

// declaredAssets renders the assets block into the digest, and nothing for a
// module that ships none. A bundle reinstalled under a new checksum rebuilds
// the instance, so the files served are the files the declaration records.
func declaredAssets(assets *install.Asset) string {
	if assets == nil {
		return ""
	}

	return "\x00assets=" + assets.Path + "\x00" + assets.Checksum
}

// declaredMount renders the root prefix into the digest, and nothing for a
// module reached under its namespace.
func declaredMount(mount string) string {
	if mount == "" {
		return ""
	}

	return "\x00mount=" + mount
}

// declaredEvents renders the endpoint table into the digest, sorted, so two
// declarations of the same table have the same digest and a changed table
// rebuilds the instance. Without it an added endpoint would not count as a
// change, and the module would still read CodeMethodNotFound.
//
// An empty table renders as the empty string, so a declaration with no events
// key keeps its digest.
func declaredEvents(events map[string]string) string {
	if len(events) == 0 {
		return ""
	}

	names := make([]string, 0, len(events))
	for name := range events {
		names = append(names, name)
	}

	slices.Sort(names)

	rendered := make([]string, 0, len(names))
	for _, name := range names {
		rendered = append(rendered, name+"="+events[name])
	}

	return strings.Join(rendered, ",")
}

// merge combines the two inputs into one document, ordered by ref.Compare:
// by kind, then by name.
func merge(fileEntries, overlayEntries []Declaration, version string) Document {
	doc := Document{Version: version}
	seen := make(map[ref.Ref]bool, len(fileEntries)+len(overlayEntries))

	for _, declared := range fileEntries {
		if seen[declared.Ref] {
			continue
		}

		seen[declared.Ref] = true

		doc.Instances = append(doc.Instances, declared)
	}

	for _, declared := range overlayEntries {
		if seen[declared.Ref] {
			doc.Shadowed = append(doc.Shadowed, declared.Ref)

			continue
		}

		seen[declared.Ref] = true

		doc.Instances = append(doc.Instances, declared)
	}

	slices.SortFunc(doc.Instances, func(a, b Declaration) int { return ref.Compare(a.Ref, b.Ref) })
	slices.SortFunc(doc.Shadowed, ref.Compare)

	return doc
}

// canonicalJSON rewrites a settings block into the one form this package
// stores: object members sorted, whitespace removed, numbers left exactly as
// they were written.
//
// json.Number keeps an int64 above 2^53 exact. Decoding into the default any
// makes it a float64, and encoding that again changes the digits an operator
// typed.
func canonicalJSON(raw []byte) (json.RawMessage, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, nil
	}

	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()

	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}

	if decoder.More() {
		return nil, errTrailingJSON
	}

	if value == nil {
		return nil, nil
	}

	canonical, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}

	return canonical, nil
}
