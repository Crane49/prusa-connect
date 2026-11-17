package install

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeHdiutil answers each attach from a list, and every detach with success.
type fakeHdiutil struct {
	mu       sync.Mutex
	attaches []error
	calls    int
}

func (f *fakeHdiutil) run(_ context.Context, args ...string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if args[0] != "attach" {
		return nil, nil
	}

	answer := f.attaches[min(f.calls, len(f.attaches)-1)]
	f.calls++

	if answer != nil {
		return []byte("hdiutil: attach failed - " + answer.Error()), answer
	}

	return []byte("/dev/disk6s1 Apple_HFS /mount"), nil
}

// The attach is the one place a test replaces hdiutil, so the cases share the
// variable one after another rather than at once.
func withHdiutil(t *testing.T, fake *fakeHdiutil) {
	t.Helper()

	held := hdiutil
	hdiutil = fake.run

	t.Cleanup(func() { hdiutil = held })
}

func TestAnAttachIsTriedAgainWhileTheServiceIsAway(t *testing.T) {
	away := errors.New("Device not configured")

	cases := map[string]struct {
		attaches []error
		calls    int
		ok       bool
		says     string
	}{
		"away twice, then attached": {[]error{away, away, nil}, 3, true, ""},
		"away every time":           {[]error{away}, attachTries, false, "disk image service could not be reached"},
		"another refusal":           {[]error{errors.New("image not recognized")}, 1, false, "image not recognized"},
	}

	for name, held := range cases {
		t.Run(name, func(t *testing.T) {
			fake := &fakeHdiutil{attaches: held.attaches}
			withHdiutil(t, fake)

			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()

			start := attachPauseFor(t, 0)
			defer start()

			err := attach(ctx, "/image.dmg", "/mount")

			if held.ok != (err == nil) || fake.calls != held.calls {
				t.Fatalf("answered %v after %d attaches", err, fake.calls)
			}

			if !held.ok && (!errors.Is(err, ErrArchive) || !strings.Contains(err.Error(), held.says)) {
				t.Fatalf("the refusal reads %v", err)
			}
		})
	}
}

// attachPauseFor sets the pause between attaches, and returns what puts it
// back.
func attachPauseFor(t *testing.T, pause time.Duration) func() {
	t.Helper()

	held := attachPause
	attachPause = pause

	return func() { attachPause = held }
}

// The 2.9.6 image holds a link to /Applications beside its folder. On a
// machine with PrusaSlicer installed, the link leads to an application too,
// and the copy would take all of /Applications.
func TestTheApplicationIsNotFoundThroughALink(t *testing.T) {
	mount, elsewhere := t.TempDir(), t.TempDir()

	for _, path := range []string{
		filepath.Join(elsewhere, "PrusaSlicer.app", "Contents", "MacOS", "PrusaSlicer"),
		filepath.Join(mount, "Original Prusa Drivers", "PrusaSlicer.app", "Contents", "MacOS", "PrusaSlicer"),
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	if err := os.Symlink(elsewhere, filepath.Join(mount, "Applications")); err != nil {
		t.Fatal(err)
	}

	found, err := findApplication(mount)
	if err != nil || found != "Original Prusa Drivers/PrusaSlicer.app/Contents/MacOS/PrusaSlicer" {
		t.Fatalf("the application is %q, %v", found, err)
	}

	if err = os.RemoveAll(filepath.Join(mount, "Original Prusa Drivers")); err != nil {
		t.Fatal(err)
	}

	if found, err = findApplication(mount); err == nil {
		t.Fatalf("the application through the link was taken: %q", found)
	}
}
