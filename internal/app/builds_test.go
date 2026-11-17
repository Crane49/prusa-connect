package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

// The example module the module cases launch is built once per test binary
// and shared. A build is a link of the whole tree, and building it again for
// every case that installs it was most of what those cases cost on a two-core
// runner: seven links, in a row for the cases that do not run in parallel.
//
// Sharing is safe because nothing writes to the artefact once it is built. The
// host verifies its checksum, launches it, and never touches the directory.

// buildsDir is where the shared artefacts live for the life of the test
// binary. TestMain owns it, because no single case may own a directory every
// other case launches from.
var buildsDir string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "prusa-connect-app-builds-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "create the shared build directory:", err)
		os.Exit(1)
	}

	buildsDir = dir

	// The suite is handed to goleak rather than run here, because goleak exits
	// the process itself and nothing written after it would run. The shim is
	// how the build directory is still removed afterwards.
	//
	// No goroutine is excused. This package is the composition root, so what it
	// starts it also owns: a hub, a supervisor, a listener and a module's child
	// process all end when the run that opened them ends, and one that does not
	// is the defect this check exists to find. Add an exception here only with
	// the reason the goroutine is nobody's to stop.
	goleak.VerifyTestMain(suiteThenClean{suite: m, clean: func() {
		// Best effort: on Windows a child the test did not manage to kill
		// keeps its binary open, and a directory left in the temporary tree is
		// not worth failing a green run over.
		_ = os.RemoveAll(dir)
	}})
}

// suiteThenClean runs the suite and then does one thing, so that goleak owns
// the exit and the shared build directory is still removed.
type suiteThenClean struct {
	suite *testing.M
	clean func()
}

func (s suiteThenClean) Run() int {
	code := s.suite.Run()

	s.clean()

	return code
}

// artefact is one binary built the first time a case asks for it and handed to
// every case after. The build error is remembered too, so a tree that will not
// build fails every case that needs it rather than only the first.
type artefact struct {
	// pkg is what go build is given, relative to the repository root.
	pkg string
	// name is the file name the host looks for, before the platform suffix.
	name string
	// variant names a build of the same package with files added through
	// go build -overlay, and is empty for the package as it is.
	variant string
	// overlay is the files the variant adds, by their path under pkg. An
	// empty content deletes the file from the build.
	overlay map[string]string
	// ldflags is handed to go build, such as the version a release stamps.
	ldflags string

	once     sync.Once
	dir      string
	checksum string
	err      error
}

// install builds the artefact once and returns the directory it is in and its
// checksum, which is how an operator's declaration names it.
func (a *artefact) install(t *testing.T) (dir, checksum string) {
	t.Helper()

	a.once.Do(a.build)

	require.NoError(t, a.err)

	return a.dir, a.checksum
}

// build links the artefact into its own directory under buildsDir. It is built
// rather than committed because the host launches <name>_<GOOS>_<GOARCH>, and
// a committed binary would be one platform's.
func (a *artefact) build() {
	toolchain, err := exec.LookPath("go")
	if err != nil {
		a.err = err

		return
	}

	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		a.err = err

		return
	}

	dir := filepath.Join(buildsDir, a.name+a.variant)
	if err = os.MkdirAll(dir, 0o750); err != nil {
		a.err = err

		return
	}

	args := []string{"build"}

	if len(a.overlay) > 0 {
		overlay, overlayErr := writeOverlay(root, dir, a.pkg, a.overlay)
		if overlayErr != nil {
			a.err = overlayErr

			return
		}

		args = append(args, "-overlay", overlay)
	}

	if a.ldflags != "" {
		args = append(args, "-ldflags", a.ldflags)
	}

	file := a.name + "_" + runtime.GOOS + "_" + runtime.GOARCH
	if runtime.GOOS == "windows" {
		file += ".exe"
	}

	out := filepath.Join(dir, file)

	build := exec.Command(toolchain, append(args, "-o", out, a.pkg)...)
	build.Dir = root

	said, err := build.CombinedOutput()
	if err != nil {
		a.err = fmt.Errorf("build %s: %w: %s", a.pkg, err, said)

		return
	}

	body, err := os.ReadFile(out)
	if err != nil {
		a.err = err

		return
	}

	sum := sha256.Sum256(body)

	a.dir = dir
	a.checksum = hex.EncodeToString(sum[:])
}

// writeOverlay writes the files a variant adds and the overlay file go build
// reads, and answers the overlay file's path.
func writeOverlay(root, dir, pkg string, files map[string]string) (string, error) {
	replace := map[string]string{}

	for name, content := range files {
		target := filepath.Join(root, filepath.FromSlash(pkg), filepath.FromSlash(name))

		if content == "" {
			replace[target] = ""

			continue
		}

		source := filepath.Join(dir, "overlay", filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(source), 0o750); err != nil {
			return "", err
		}

		if err := os.WriteFile(source, []byte(content), 0o600); err != nil {
			return "", err
		}

		replace[target] = source
	}

	encoded, err := json.Marshal(map[string]any{"Replace": replace})
	if err != nil {
		return "", err
	}

	path := filepath.Join(dir, "overlay.json")

	return path, os.WriteFile(path, encoded, 0o600)
}
