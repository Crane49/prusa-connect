package extension

import (
	"errors"
	"fmt"
	"runtime"
	"runtime/debug"
	"slices"
	"strings"
)

// CoreNamespace is the namespace of everything registered from the
// application's own module. No other module can have it.
const CoreNamespace = Namespace("core")

// CoreModulePath is the module the built-ins live in, and the only module whose
// registrations render as CoreNamespace. It is a constant, so the built-ins
// keep their names in a binary where the core is a dependency.
const CoreModulePath = "github.com/prusa3d/prusa-connect"

// SdkModulePath is the contracts module. A registration from it is in
// CoreNamespace too.
const SdkModulePath = CoreModulePath + "/sdk"

// coreModules is every module whose registrations render as CoreNamespace.
func coreModules() []string { return []string{CoreModulePath, SdkModulePath} }

// ErrNoNamespace reports a registration with no namespace of its own: a module
// missing from the build graph, a module path that is not a legal namespace,
// or a path that would render as CoreNamespace.
var ErrNoNamespace = errors.New("extension: cannot derive a namespace")

const (
	namespaceSeparator = "."
	pathSeparator      = "/"
	majorVersionPrefix = "v"

	// hostBoundary is the label between the reversed host and the rest of the
	// module path. Without it "github.com/acme/widget" and
	// "acme.github.com/widget" would share a namespace. A module path with
	// this label is refused.
	hostBoundary = "aux"

	// minNamespaceLabels makes a namespace a legal error domain.
	// fault.NewDomain refuses fewer than two labels.
	minNamespaceLabels = 2

	// MaxNamespaceLen leaves 32 bytes inside sdk/event's 128-byte type limit,
	// so a namespace and an event name fit in one event type.
	MaxNamespaceLen = 96

	// walkDepth bounds the frames the namespace walk reads.
	walkDepth = 64
)

// Namespace is the collision domain of a registered name: the module path of
// the package that registered it, with the host reversed, the label "aux" where
// the host ends, and the rest of the path after it. github.com/acme/widget is
// com.github.aux.acme.widget. The host derives it, so two different module
// paths never share a namespace. Every label is a DNS-1123 label.
type Namespace string

// IsZero reports whether the namespace names nothing.
func (n Namespace) IsZero() bool { return n == "" }

// IsCore reports whether the namespace is the application's own module.
func (n Namespace) IsCore() bool { return n == CoreNamespace }

// String returns the reverse-DNS form, empty for the zero value.
func (n Namespace) String() string { return string(n) }

// KnownWrappers returns the packages a registration is routed through on its
// way to Declare: this package, the published contract packages, and the
// application's own resource registry. The namespace walk skips them, so a
// package listed here cannot register on its own behalf.
func KnownWrappers() []string {
	return []string{
		CoreModulePath + "/sdk/extension",
		CoreModulePath + "/sdk/authapi",
		CoreModulePath + "/sdk/cameraapi",
		CoreModulePath + "/sdk/connectorapi",
		CoreModulePath + "/sdk/eventsapi",
		CoreModulePath + "/sdk/historyapi",
		CoreModulePath + "/sdk/routesapi",
		CoreModulePath + "/sdk/stateapi",
		CoreModulePath + "/sdk/storageapi",
		CoreModulePath + "/internal/extension/registry",
	}
}

// callerModule returns the module path of the package that registered and the
// namespace derived from it.
func callerModule() (string, Namespace, error) {
	pkg, ok := registrantPackage()
	if !ok {
		return "", "", fmt.Errorf("%w: no frame outside the registration wrappers", ErrNoNamespace)
	}

	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "", "", fmt.Errorf("%w: %q has no build info", ErrNoNamespace, pkg)
	}

	modules := make([]string, 0, len(info.Deps)+1)
	for _, dep := range info.Deps {
		modules = append(modules, dep.Path)
	}

	return deriveNamespace(pkg, info.Main.Path, modules)
}

// registrantPackage returns the import path of the package that registered,
// read from the live stack.
func registrantPackage() (string, bool) {
	pcs := make([]uintptr, walkDepth)

	n := runtime.Callers(1, pcs)
	if n == 0 {
		return "", false
	}

	frames := runtime.CallersFrames(pcs[:n])

	packages := make([]string, 0, n)

	for {
		frame, more := frames.Next()
		if frame.Function != "" {
			packages = append(packages, packageOf(frame.Function))
		}

		if !more {
			break
		}
	}

	return registrantOf(packages)
}

// registrantOf returns the first package that is not a wrapper, given the
// packages of a stack innermost first.
func registrantOf(packages []string) (string, bool) {
	wrappers := KnownWrappers()

	for _, pkg := range packages {
		if slices.Contains(wrappers, pkg) {
			continue
		}

		return pkg, true
	}

	return "", false
}

// deriveNamespace maps a package path onto the namespace of the module that
// contains it. The longest module path that prefixes it wins.
func deriveNamespace(pkg, mainModule string, modules []string) (string, Namespace, error) {
	best := ""
	if covers(mainModule, pkg) {
		best = mainModule
	}

	for _, module := range modules {
		if covers(module, pkg) && len(module) > len(best) {
			best = module
		}
	}

	if best == "" {
		return "", "", fmt.Errorf(
			"%w: the registration is attributed to %q, which is in no module of this build. "+
				"Register from the package that owns the name rather than through a dispatcher",
			ErrNoNamespace, pkg)
	}

	if slices.Contains(coreModules(), best) {
		return best, CoreNamespace, nil
	}

	namespace, err := NamespaceOf(best)
	if err != nil {
		return "", "", err
	}

	return best, namespace, nil
}

// covers reports whether module contains pkg. A prefix match on the string
// alone would put "example.com/store" inside "example.com/st".
func covers(module, pkg string) bool {
	return module != "" && (pkg == module || strings.HasPrefix(pkg, module+pathSeparator))
}

// NamespaceOf returns the namespace a module path registers under. It reports
// ErrNoNamespace for a label that is not DNS-1123, a label that is the host
// boundary, a result equal to CoreNamespace, and a result that is too long or
// too short.
func NamespaceOf(module string) (Namespace, error) {
	namespace, err := reverseDNS(module)
	if err != nil {
		return "", err
	}

	// A single-label path renders as that label, so a module named "core"
	// would take the built-ins' namespace.
	if namespace == CoreNamespace {
		return "", fmt.Errorf("%w: %q renders as %q, which is the main module's alone",
			ErrNoNamespace, module, CoreNamespace)
	}

	if labels := strings.Count(string(namespace), namespaceSeparator) + 1; labels < minNamespaceLabels {
		return "", fmt.Errorf(
			"%w: %q renders as %q, and a namespace of one label is not an error domain. "+
				"Give the module path a host, as github.com/acme/mk4 has",
			ErrNoNamespace, module, namespace)
	}

	if len(namespace) > MaxNamespaceLen {
		return "", fmt.Errorf(
			"%w: %q renders as %q, which is %d bytes and the limit is %d, "+
				"because a namespace and the name of an event compose one event type",
			ErrNoNamespace, module, namespace, len(namespace), MaxNamespaceLen)
	}

	return namespace, nil
}

// reverseDNS renders a module path as dotted labels, host first and reversed, so
// the namespace sorts by owner. A label that is not a DNS-1123 label is refused.
func reverseDNS(module string) (Namespace, error) {
	segments := strings.Split(module, pathSeparator)
	if last := len(segments) - 1; last > 0 && isMajorVersion(segments[last]) {
		segments = segments[:last]
	}

	host := strings.Split(segments[0], namespaceSeparator)
	rest := segments[1:]

	for _, label := range slices.Concat(host, rest) {
		if ValidateName(label) != nil {
			return "", fmt.Errorf("%w: module %q is not one, because %q is not a DNS-1123 label",
				ErrNoNamespace, module, label)
		}

		if label == hostBoundary {
			return "", fmt.Errorf(
				"%w: module %q is not one, because %q is the label that marks where the host ends. "+
					"Go refuses it as a path element, so rename the label that carries it",
				ErrNoNamespace, module, label)
		}
	}

	labels := make([]string, 0, len(host)+len(rest)+1)
	for _, h := range slices.Backward(host) {
		labels = append(labels, h)
	}

	if len(rest) > 0 {
		labels = append(labels, hostBoundary)
		labels = append(labels, rest...)
	}

	return Namespace(strings.Join(labels, namespaceSeparator)), nil
}

// isMajorVersion reports whether segment is a /vN module suffix, which is not
// part of the namespace.
func isMajorVersion(segment string) bool {
	if len(segment) < 2 || !strings.HasPrefix(segment, majorVersionPrefix) {
		return false
	}

	for i := 1; i < len(segment); i++ {
		if segment[i] < '0' || segment[i] > '9' {
			return false
		}
	}

	return true
}

// packageOf extracts the import path from a runtime function name, such as
// github.com/acme/widget.init.0. Type arguments are cut first.
func packageOf(function string) string {
	if i := strings.IndexByte(function, '['); i >= 0 {
		function = function[:i]
	}

	dir, symbol := "", function
	if i := strings.LastIndex(function, pathSeparator); i >= 0 {
		dir, symbol = function[:i+1], function[i+1:]
	}

	if i := strings.IndexByte(symbol, '.'); i >= 0 {
		symbol = symbol[:i]
	}

	return dir + symbol
}

// Validate checks a namespace against the form NamespaceOf produces: dotted
// DNS-1123 labels, at least two of them, at most MaxNamespaceLen bytes, and
// never CoreNamespace. Run it on a namespace read from a request path or a
// file.
func (n Namespace) Validate() error {
	if n.IsZero() {
		return fmt.Errorf("%w: the namespace is empty", ErrNoNamespace)
	}

	if len(n) > MaxNamespaceLen {
		return fmt.Errorf("%w: %q is %d bytes and the limit is %d",
			ErrNoNamespace, n, len(n), MaxNamespaceLen)
	}

	if n.IsCore() {
		return fmt.Errorf("%w: %q is the main module's alone", ErrNoNamespace, n)
	}

	labels := strings.Split(string(n), namespaceSeparator)
	if len(labels) < minNamespaceLabels {
		return fmt.Errorf("%w: %q is one label, which is not a collision domain. "+
			"Give the module path a host, as github.com/acme/mk4 has", ErrNoNamespace, n)
	}

	for _, label := range labels {
		if err := ValidateName(label); err != nil {
			return fmt.Errorf("%w: %q carries %q, which is not a DNS-1123 label: %w",
				ErrNoNamespace, n, label, err)
		}
	}

	return nil
}
