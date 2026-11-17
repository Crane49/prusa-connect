package runner_test

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prusa3d/server-slicing/internal/runner"
)

// fake is the stand-in slicer under testdata.
func fake(t *testing.T) string {
	t.Helper()

	if runtime.GOOS == "windows" {
		t.Skip("the stand-in slicer is a shell script")
	}

	path, err := filepath.Abs("../../testdata/fake-slicer.sh")
	if err != nil {
		t.Fatal(err)
	}

	return path
}

// project writes a project whose first line tells the stand-in what to do.
func project(t *testing.T, mode string) runner.Spec {
	t.Helper()

	dir := t.TempDir()
	input := filepath.Join(dir, "project.3mf")

	if err := os.WriteFile(input, []byte("mode:"+mode+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	return runner.Spec{Binary: fake(t), Input: input, Dir: filepath.Join(dir, "work"), Timeout: 20 * time.Second}
}

func TestARunAnswersTheFileTheSlicerWrote(t *testing.T) {
	t.Parallel()

	var (
		mu    sync.Mutex
		steps []int
	)

	result, err := runner.Run(t.Context(), project(t, "binary"), func(percent int, _ string) {
		mu.Lock()
		steps = append(steps, percent)
		mu.Unlock()
	})
	if err != nil {
		t.Fatal(err)
	}

	if result.Format != runner.Binary || result.Size != 10 {
		t.Fatalf("the result is %+v", result)
	}

	mu.Lock()
	defer mu.Unlock()

	if len(steps) < 2 || steps[0] != 10 || steps[1] != 90 {
		t.Fatalf("the progress was %v", steps)
	}
}

// The format is read from the file and not from what was asked for, because a
// slicer may write text when binary was asked for.
func TestTheFormatIsReadFromTheFile(t *testing.T) {
	t.Parallel()

	spec := project(t, "text")
	spec.Format = runner.Binary

	result, err := runner.Run(t.Context(), spec, nil)
	if err != nil {
		t.Fatal(err)
	}

	if result.Format != runner.Text {
		t.Fatalf("a text file read as %s", result.Format)
	}
}

func TestARunThatWritesNothingFails(t *testing.T) {
	t.Parallel()

	cases := map[string]error{
		"nothing": runner.ErrNothingToPrint,
		"empty":   runner.ErrNoOutput,
		"fail":    runner.ErrFailed,
	}

	for mode, want := range cases {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()

			_, err := runner.Run(t.Context(), project(t, mode), nil)
			if !errors.Is(err, want) {
				t.Fatalf("answered %v, want %v", err, want)
			}

			var failure *runner.RunError
			if mode != "empty" && (!errors.As(err, &failure) || len(failure.Log) == 0) {
				t.Fatalf("the failure carries no log: %v", err)
			}
		})
	}
}

func TestARunIsStoppedAtItsBounds(t *testing.T) {
	t.Parallel()

	timed := project(t, "hang")
	timed.Timeout = 300 * time.Millisecond

	started := time.Now()

	if _, err := runner.Run(t.Context(), timed, nil); !errors.Is(err, runner.ErrTimeout) {
		t.Fatalf("a run past its time answered %v", err)
	}

	if took := time.Since(started); took > 10*time.Second {
		t.Fatalf("the stopped run took %s to return", took)
	}

	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		return
	}

	held := project(t, "hang")
	held.MemoryBytes = 1

	if _, err := runner.Run(t.Context(), held, nil); !errors.Is(err, runner.ErrMemory) {
		t.Fatalf("a run over its memory answered %v", err)
	}
}

// The arguments are a list. A project path with spaces and shell characters
// reaches the slicer as one argument, and nothing of it is run.
func TestTheArgumentsReachTheSlicerAsAList(t *testing.T) {
	t.Parallel()

	spec := project(t, "args")
	odd := filepath.Join(filepath.Dir(spec.Input), "my part; touch pwned $(id).3mf")

	if err := os.Rename(spec.Input, odd); err != nil {
		t.Fatal(err)
	}

	spec.Input = odd
	spec.Threads = 2
	spec.Format = runner.Binary

	result, err := runner.Run(t.Context(), spec, nil)
	if err != nil {
		t.Fatal(err)
	}

	written, err := os.ReadFile(result.Output)
	if err != nil {
		t.Fatal(err)
	}

	line := string(written)
	for _, want := range []string{"--export-gcode", "--datadir", "--binary-gcode", "--threads 2", odd} {
		if !strings.Contains(line, want) {
			t.Errorf("the slicer was not given %q: %s", want, line)
		}
	}

	if _, err = os.Stat(filepath.Join(spec.Dir, "pwned")); err == nil {
		t.Fatal("a shell ran part of the project's name")
	}
}
