package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/routesapi"
	"github.com/prusa3d/prusa-connect/sdk/storageapi"

	"github.com/prusa3d/threemf/internal/analysis"
	"github.com/prusa3d/threemf/internal/projects"
	"github.com/prusa3d/threemf/internal/storagefake"
)

// The statuses a route may answer for a file somebody uploaded. Anything else,
// a panic or an answer the host refuses, is a defect.
//
//nolint:gochecknoglobals // a fixed set.
var fileStatuses = []int{
	http.StatusOK, http.StatusNotFound, http.StatusUnprocessableEntity, http.StatusServiceUnavailable,
}

// drive asks every route about one file and answers the problems found.
func drive(t *testing.T, serves func(routesapi.Request) routesapi.Response, path string) []string {
	t.Helper()

	var problems []string

	query := "storage=local&path=" + path

	for _, ask := range []struct{ route, query string }{
		{"/project", query},
		{"/project/thumbnail", query + "&index=0"},
		{"/project/mesh", query + "&object=0"},
	} {
		response := serves(routesapi.Request{Method: "GET", Path: ask.route, Query: ask.query, Principal: alice})
		if !slices.Contains(fileStatuses, response.Status) {
			problems = append(problems, ask.route+" answered "+http.StatusText(response.Status)+": "+
				string(response.Body))
		}

		if response.Status != http.StatusOK && !json.Valid(response.Body) {
			problems = append(problems, ask.route+" refused with a body that is not JSON")
		}
	}

	return problems
}

// hostile is one file and the copies of it a careless or a hostile uploader
// makes: cut short at a few places, and with bytes flipped inside the zip.
func hostile(raw []byte) map[string][]byte {
	out := map[string][]byte{"whole": raw}

	for _, cut := range []int{10, len(raw) / 3, len(raw) / 2, len(raw) - 22, len(raw) - 1} {
		if cut > 0 && cut < len(raw) {
			out["cut-"+itoa(cut)] = raw[:cut]
		}
	}

	for _, at := range []int{30, len(raw) / 2, len(raw) - 40} {
		if at > 0 && at < len(raw) {
			flipped := bytes.Clone(raw)
			flipped[at] ^= 0xFF
			out["flip-"+itoa(at)] = flipped
		}
	}

	return out
}

func itoa(n int) string { return strconv.Itoa(n) }

// TestTheCorpusAndItsDamagedCopiesNeverBreakTheModule reads every project of
// a corpus, and copies of each cut short or with bytes flipped, through the
// routes and the answer to the core. THREEMF_CORPUS names the folders, joined
// by the list separator. Without it the test reads the module's own fixtures.
func TestTheCorpusAndItsDamagedCopiesNeverBreakTheModule(t *testing.T) {
	t.Parallel()

	var paths []string

	for _, dir := range filepath.SplitList(os.Getenv("THREEMF_CORPUS")) {
		found, err := filepath.Glob(filepath.Join(dir, "*.3mf"))
		if err != nil {
			t.Fatal(err)
		}

		paths = append(paths, found...)
	}

	if len(paths) == 0 {
		found, err := filepath.Glob(filepath.Join("threemf", "testdata", "*.3mf"))
		if err != nil || len(found) == 0 {
			t.Fatalf("no fixture found: %v", err)
		}

		paths = found
	}

	for _, source := range paths {
		raw, err := os.ReadFile(source)
		if err != nil {
			t.Fatal(err)
		}

		copies := hostile(raw)
		if len(raw) > 64<<20 {
			// The damaged copies of a large file prove nothing the small ones
			// do not, and each costs a whole read.
			copies = map[string][]byte{"whole": raw}
		}

		files := &storagefake.Files{}
		serves := methods(storageapi.NewClient(nobody{}), projects.New(files))

		for kind, data := range copies {
			name := filepath.Base(source) + "." + kind + ".3mf"
			files.Put(storage, name, data, 1)

			started := time.Now()

			for _, problem := range drive(t, func(r routesapi.Request) routesapi.Response {
				return call(t, serves, r)
			}, name) {
				t.Errorf("%s: %s", name, problem)
			}

			project, readErr := projects.New(nil).Analyze(context.Background(), sized{bytes.NewReader(data)})
			if readErr == nil {
				if err = analysis.Of(project).Validate(); err != nil {
					t.Errorf("%s: the host would refuse the answer to the core: %v", name, err)
				}
			}

			// Each copy is read whole four times: the project, the mesh, and
			// the answer to the core twice over a damaged copy. The largest
			// projects of the corpus take some seconds a read.
			if took := time.Since(started); took > 2*time.Minute {
				t.Errorf("%s took %v", name, took)
			}
		}
	}
}

// Many people opening one project at once cost one read, and many projects
// opened at once are read two at a time without a failure.
func TestConcurrentRequestsShareOneRead(t *testing.T) {
	t.Parallel()

	files := &storagefake.Files{}
	raw := fixture(t, "prusaslicer-2.9.6-multibed.3mf")

	for i := range 6 {
		files.Put(storage, "p"+itoa(i)+".3mf", raw, 1)
	}

	serves := methods(storageapi.NewClient(nobody{}), projects.New(files))

	var (
		wait   sync.WaitGroup
		mu     sync.Mutex
		failed []string
	)

	for i := range 48 {
		wait.Go(func() {
			path := "p" + itoa(i%6) + ".3mf"
			route := []string{"/project", "/project/thumbnail", "/project/mesh"}[i%3]

			response := call(t, serves, routesapi.Request{
				Method: "GET", Path: route, Query: "storage=local&path=" + path + "&object=0", Principal: alice,
			})
			if response.Status != http.StatusOK {
				mu.Lock()
				failed = append(failed, route+" "+path+": "+string(response.Body))
				mu.Unlock()
			}
		})
	}

	wait.Wait()

	if len(failed) > 0 {
		t.Fatalf("%d requests failed: %v", len(failed), failed)
	}

	// Each file is opened once for its project and the thumbnails, and once
	// more when a mesh of it was asked for.
	reads := map[string]bool{}
	for i := range 48 {
		kind := "project"
		if i%3 == 2 {
			kind = "mesh"
		}

		reads[itoa(i%6)+kind] = true
	}

	if opened := files.Opens.Load(); opened != int64(len(reads)) {
		t.Errorf("the files were opened %d times, want %d", opened, len(reads))
	}
}
