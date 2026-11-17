package extension

import (
	"errors"
	"maps"
	"slices"
	"strings"
	"testing"
)

func TestDeriveNamespaceReadsTheModuleGraph(t *testing.T) {
	t.Parallel()

	const main = "github.com/prusa3d/prusa-connect"

	modules := []string{
		"github.com/acme/pcl-mk4",
		"github.com/acme/pcl-mk4/extras",
		"gitlab.example.com/team/dialect/v2",
		"local-module/inner",
	}

	for _, testCase := range []struct {
		name       string
		pkg        string
		want       Namespace
		wantModule string
	}{
		{
			name:       "the main module is core",
			pkg:        main + "/adapters/connector/echo",
			want:       CoreNamespace,
			wantModule: main,
		},
		{
			name:       "a dependency is reverse-dns",
			pkg:        "github.com/acme/pcl-mk4",
			want:       "com.github.aux.acme.pcl-mk4",
			wantModule: "github.com/acme/pcl-mk4",
		},
		{
			name:       "the longest module wins",
			pkg:        "github.com/acme/pcl-mk4/extras/deep",
			want:       "com.github.aux.acme.pcl-mk4.extras",
			wantModule: "github.com/acme/pcl-mk4/extras",
		},
		{
			name:       "a major version suffix names the same owner",
			pkg:        "gitlab.example.com/team/dialect/v2/wire",
			want:       "com.example.gitlab.aux.team.dialect",
			wantModule: "gitlab.example.com/team/dialect/v2",
		},
		{
			name:       "a path with no host",
			pkg:        "local-module/inner",
			want:       "local-module.aux.inner",
			wantModule: "local-module/inner",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			module, got, err := deriveNamespace(testCase.pkg, main, modules)
			if err != nil {
				t.Fatalf("deriveNamespace: %v", err)
			}

			if got != testCase.want {
				t.Errorf("deriveNamespace = %q, want %q", got, testCase.want)
			}

			if module != testCase.wantModule {
				t.Errorf("deriveNamespace module = %q, want %q", module, testCase.wantModule)
			}
		})
	}
}

func TestDeriveNamespaceDoesNotMatchOnStringPrefixAlone(t *testing.T) {
	t.Parallel()

	// "example.com/st" must not swallow "example.com/store".
	_, got, err := deriveNamespace("example.com/store/wire", "example.com/main",
		[]string{"example.com/st", "example.com/store"})
	if err != nil {
		t.Fatalf("deriveNamespace: %v", err)
	}

	if got != "com.example.aux.store" {
		t.Errorf("deriveNamespace = %q, want com.example.aux.store", got)
	}
}

func TestDeriveNamespaceRefusesAPackageInNoModule(t *testing.T) {
	t.Parallel()

	_, _, err := deriveNamespace("example.com/somewhere/else", "example.com/main", []string{"example.com/dep"})
	if !errors.Is(err, ErrNoNamespace) {
		t.Errorf("a package in no module of this build was given a namespace: %v", err)
	}
}

// CoreNamespace is what the built-ins register under, and the whole point of
// deriving it is that nothing outside the main module can hold it. A single-label
// module path renders as that label, so a module named "core", reached through a
// replace directive, lands on it.
func TestOnlyTheMainModuleHoldsTheCoreNamespace(t *testing.T) {
	t.Parallel()

	for _, module := range []string{"core", "CORE"} {
		t.Run(module, func(t *testing.T) {
			t.Parallel()

			_, got, err := deriveNamespace(module+"/driver", "github.com/prusa3d/prusa-connect",
				[]string{module})
			if err == nil {
				t.Fatalf("module %q derived namespace %q, which only the main module may hold", module, got)
			}

			if !errors.Is(err, ErrNoNamespace) {
				t.Errorf("deriveNamespace(%q) = %v, want ErrNoNamespace", module, err)
			}
		})
	}
}

// A namespace is a collision domain, so two module paths that are not the same
// path may not land in one. Two renderings put more than one module in a domain:
// folding a character that is illegal in a label onto a hyphen, and joining the
// reversed host to the rest of the path with the separator the host's own labels
// use.
func TestDistinctModulePathsDoNotShareANamespace(t *testing.T) {
	t.Parallel()

	seen := map[Namespace]string{}
	accepted := 0

	record := func(module string) {
		_, got, err := deriveNamespace(module+"/wire", "example.com/main", []string{module})
		if err != nil {
			return
		}

		accepted++

		if other, taken := seen[got]; taken && other != module {
			t.Errorf("%q and %q both derive namespace %q, so one collision domain holds two modules",
				other, module, got)
		}

		seen[got] = module
	}

	// Rewriting a label that is not one folds these three together. Two are
	// refused, so only "example.com/a-b" reaches the map.
	for _, module := range []string{"example.com/a_b", "example.com/a-b", "example.com/A-B"} {
		record(module)
	}

	// Every module path one namespace could be cut from, which is the property
	// rather than a list of inputs somebody thought of. Each cut moves a label
	// between the host and the path, so each is a different module, and
	// "github.com/acme/mk4" is the one a squatter impersonates.
	target := []string{"com", "github", "acme", "mk4"}
	for cut := 1; cut <= len(target); cut++ {
		record(moduleCutAt(target, cut))
	}

	// One of the fold's three, and every cut. A path refused rather than
	// rendered would leave the map too small to hold a collision at all.
	if want := 1 + len(target); accepted != want {
		t.Errorf("%d module paths were accepted, want %d: %v", accepted, want, seen)
	}

	// Closing the loop: every namespace produced above is itself cut at every
	// position, which enumerates every module path that could render onto it,
	// boundary label included. A cut that is accepted and lands on a namespace
	// another module already holds is the collision, and record reports it.
	// This is where a boundary a module path can carry would show up, because
	// "mk4.aux.acme.com" is one cut of "com.acme.aux.mk4".
	for _, namespace := range slices.Sorted(maps.Keys(seen)) {
		labels := strings.Split(string(namespace), namespaceSeparator)
		for cut := 1; cut <= len(labels); cut++ {
			record(moduleCutAt(labels, cut))
		}
	}
}

// moduleCutAt builds the module path whose host is the first cut labels of a
// namespace, reversed, and whose remaining segments are the rest of it.
func moduleCutAt(namespace []string, cut int) string {
	host := slices.Clone(namespace[:cut])
	slices.Reverse(host)

	module := strings.Join(host, namespaceSeparator)
	if rest := namespace[cut:]; len(rest) > 0 {
		module += pathSeparator + strings.Join(rest, pathSeparator)
	}

	return module
}

// NamespaceOf is what a tool asks before the module it is asking about exists,
// so it answers what the build graph will answer or it is worth nothing. The
// scaffold refuses a -module on it, and a scaffolded driver prints the qualified
// name an operator writes from it.
func TestNamespaceOfAnswersWhatTheBuildGraphWould(t *testing.T) {
	t.Parallel()

	for _, module := range []string{
		"github.com/acme/mk4", "acme.github.com/mk4", "com/github/acme/mk4",
		"example.com/prusa-driver-acme", "gopkg.in/yaml.v3", "example.com",
		"core", "example.com/A-B", "github.com/Masterminds/sprig",
		"example.com/aux", "onelabel", "github.com/99designs/mk4",
	} {
		t.Run(module, func(t *testing.T) {
			t.Parallel()

			got, err := NamespaceOf(module)

			_, derived, deriveErr := deriveNamespace(module+"/wire", "example.com/main", []string{module})

			if (err == nil) != (deriveErr == nil) {
				t.Fatalf("NamespaceOf(%q) = %v and the build graph derived %v", module, err, deriveErr)
			}

			if got != derived {
				t.Errorf("NamespaceOf(%q) = %q, and the build graph derives %q", module, got, derived)
			}
		})
	}
}

func TestAModulePathThatIsNotAlreadyALegalNamespaceIsRefused(t *testing.T) {
	t.Parallel()

	for _, module := range []string{
		"example.com/a_b", "example.com/A-B", "gitlab.example.com/team/Dialect/v2",
		"example.com/-leading", "example.com/trailing-", "example.com/a.b", "example.com/über",
		"example.com/-",

		// The boundary label. Nothing in a module path may be one, or the
		// rendering stops being reversible: "mk4.aux.example.com" would take
		// the namespace "example.com/mk4" holds.
		"example.com/aux", "aux.example.com/driver", "example.aux.com/driver",
		"example.com/driver/aux/v2",

		// A namespace of one label is not an error domain, and a rendering past
		// the length limit is not an event type (ADR-20260826-6x5 rules 4 and 5).
		"onelabel", "gitlab.example.com/" + strings.Repeat("a", 63) + "/" + strings.Repeat("b", 63),
	} {
		t.Run(module, func(t *testing.T) {
			t.Parallel()

			_, got, err := deriveNamespace(module+"/wire", "example.com/main", []string{module})
			if err == nil {
				t.Fatalf("module %q was rewritten into namespace %q instead of being refused", module, got)
			}

			if !errors.Is(err, ErrNoNamespace) {
				t.Errorf("deriveNamespace(%q) = %v, want ErrNoNamespace", module, err)
			}
		})
	}
}

func TestPackageOfReadsARuntimeFunctionName(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name     string
		function string
		want     string
	}{
		{name: "an init function", function: "github.com/acme/mk4.init.0", want: "github.com/acme/mk4"},
		{name: "a method", function: "github.com/acme/mk4.(*Driver).Baseline", want: "github.com/acme/mk4"},
		{name: "main", function: "main.main", want: "main"},
		{
			name:     "a generic function, whose type arguments carry dots",
			function: "github.com/acme/mk4.Register[go.shape.string]",
			want:     "github.com/acme/mk4",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := packageOf(testCase.function); got != testCase.want {
				t.Errorf("packageOf = %q, want %q", got, testCase.want)
			}
		})
	}
}

// The namespace is read from the first frame outside the wrappers, and the walk
// is the host's. The case that matters is the one a fixed skip count gets wrong:
// a plugin function the host called, whose own frame sits below a host frame.
// The plugin is the registrant, whichever host package invoked it.
func TestTheRegistrantIsTheFirstFrameOutsideTheWrappers(t *testing.T) {
	t.Parallel()

	const (
		self    = "github.com/prusa3d/prusa-connect/sdk/extension"
		storage = "github.com/prusa3d/prusa-connect/sdk/storageapi"
		types   = "github.com/prusa3d/prusa-connect/internal/extension/registry"
		plugin  = "example.com/plugin/mk4"
		wiring  = "github.com/prusa3d/prusa-connect/internal/app"
	)

	for _, testCase := range []struct {
		name   string
		frames []string
		want   string
	}{
		{
			name:   "a plugin the host called is still the registrant",
			frames: []string{self, self, storage, storage, plugin, wiring, "testing"},
			want:   plugin,
		},
		{
			name:   "a built-in registering through the resource registry",
			frames: []string{self, types, types, wiring, "testing"},
			want:   wiring,
		},
		{
			name:   "a registration made straight against this package",
			frames: []string{self, plugin, "runtime"},
			want:   plugin,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got, ok := registrantOf(testCase.frames)
			if !ok {
				t.Fatalf("registrantOf(%v) found no frame outside the wrappers", testCase.frames)
			}

			if got != testCase.want {
				t.Errorf("registrantOf = %q, want %q", got, testCase.want)
			}
		})
	}
}

func TestAStackOfNothingButWrappersHasNoRegistrant(t *testing.T) {
	t.Parallel()

	if got, ok := registrantOf(KnownWrappers()); ok {
		t.Errorf("registrantOf resolved a stack of wrappers to %q", got)
	}
}

// A wrapper package cannot register on its own behalf, because its frames are
// the ones the walk steps over. This test runs inside one, so the live walk
// reaches the test runner rather than this package.
func TestAWrapperPackageIsNotItsOwnRegistrant(t *testing.T) {
	t.Parallel()

	pkg, ok := registrantPackage()
	if !ok {
		t.Fatal("the live walk found no frame outside the wrappers")
	}

	if pkg == "github.com/prusa3d/prusa-connect/sdk/extension" {
		t.Error("the live walk stopped inside a wrapper package")
	}
}

// TestTheCoreKeepsItsNamespaceWhenItIsNotTheMainModule is ADR-20260901-7t4 rule 7's
// gate. In a composed binary the main module is the composition module and the
// core is a dependency; if CoreNamespace were keyed on the main module, every
// shipped qualified name would be renamed and every configuration naming
// core/amqp would stop resolving.
func TestTheCoreKeepsItsNamespaceWhenItIsNotTheMainModule(t *testing.T) {
	t.Parallel()

	const composition = "example.com/operator/my-connect-build"

	modules := []string{CoreModulePath, SdkModulePath, "github.com/acme/shelf"}

	for _, testCase := range []struct {
		name string
		pkg  string
		want Namespace
	}{
		{
			name: "a core package still renders core",
			pkg:  CoreModulePath + "/adapters/events/amqp",
			want: CoreNamespace,
		},
		{
			name: "an sdk package still renders core",
			pkg:  SdkModulePath + "/plugintest",
			want: CoreNamespace,
		},
		{
			name: "a composed adapter renders its own module",
			pkg:  "github.com/acme/shelf/driver",
			want: "com.github.aux.acme.shelf",
		},
		{
			name: "the composition module renders its own path",
			pkg:  composition,
			want: "com.example.aux.operator.my-connect-build",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			_, got, err := deriveNamespace(testCase.pkg, composition, modules)
			if err != nil {
				t.Fatalf("deriveNamespace(%q): %v", testCase.pkg, err)
			}

			if got != testCase.want {
				t.Errorf("deriveNamespace(%q) = %q, want %q", testCase.pkg, got, testCase.want)
			}
		})
	}
}
