package diagnostics

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/kernel/machinetext"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/sdk/diagnosticsapi"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// pages answers the pages in order, the cursor being the index of the next.
func pages(answers ...diagnosticsapi.Page) func(context.Context, string) (diagnosticsapi.Page, error) {
	return func(_ context.Context, cursor string) (diagnosticsapi.Page, error) {
		at := 0
		if cursor != "" {
			at, _ = strconv.Atoi(cursor)
		}

		page := answers[at]
		if at+1 < len(answers) {
			page.Cursor = strconv.Itoa(at + 1)
		}

		return page, nil
	}
}

// text is each file's text by name, in the order the module first named them.
func text(held *collected) [][2]string {
	out := make([][2]string, 0, len(held.files))
	for _, file := range held.files {
		out = append(out, [2]string{file.name, file.text.String()})
	}

	return out
}

// Text a module sends under one name on two pages lands in one file, and the
// files keep the order the module first named them in.
func TestAModulesPagesBecomeItsFiles(t *testing.T) {
	t.Parallel()

	held := collectExtension(t.Context(), pages(
		diagnosticsapi.Page{Files: []diagnosticsapi.File{{Name: "syslog.log", Text: "newest\n"}}},
		diagnosticsapi.Page{Files: []diagnosticsapi.File{
			{Name: "metrics.lp", Text: "temp_bed v=60\n"}, {Name: "syslog.log", Text: "older\n"},
		}},
	))

	require.Empty(t, held.reason)
	require.Equal(t, [][2]string{{"syslog.log", "newest\nolder\n"}, {"metrics.lp", "temp_bed v=60\n"}}, text(held))
}

// The host takes at most 8 MiB from one module, the file's name included,
// cut after a whole line, and says so (ADR-20261019-nrq rule 5).
func TestAModuleIsHeldToTheSizeLimit(t *testing.T) {
	t.Parallel()

	line := strings.Repeat("x", 1023) + "\n"
	page := diagnosticsapi.Page{Files: []diagnosticsapi.File{{Name: "a.log", Text: strings.Repeat(line, 256)}}}

	answers := make([]diagnosticsapi.Page, 40)
	for i := range answers {
		answers[i] = page
	}

	held := collectExtension(t.Context(), pages(answers...))

	require.Equal(t, extensionTruncated, held.reason)
	require.Equal(t, limitSize, held.limit)
	require.LessOrEqual(t, held.files[0].text.Len(), maxExtensionBytes-len("a.log")-fileCost)
	require.Greater(t, held.files[0].text.Len(), maxExtensionBytes-len("a.log")-fileCost-len(line))
	require.True(t, strings.HasSuffix(held.files[0].text.String(), "\n"))
}

// A module that never says it is done is stopped after 64 pages.
func TestAModuleIsHeldToThePageLimit(t *testing.T) {
	t.Parallel()

	asked := 0
	endless := func(context.Context, string) (diagnosticsapi.Page, error) {
		asked++

		return diagnosticsapi.Page{Files: []diagnosticsapi.File{{Name: "a.log", Text: "x\n"}}, Cursor: "more"}, nil
	}

	held := collectExtension(t.Context(), endless)

	require.Equal(t, extensionTruncated, held.reason)
	require.Equal(t, limitPages, held.limit)
	require.Equal(t, maxExtensionPages, asked)
}

// many is one page of MaxPageFiles entries with names no page used before.
func many(page int, text string) diagnosticsapi.Page {
	files := make([]diagnosticsapi.File, diagnosticsapi.MaxPageFiles)
	for i := range files {
		files[i] = diagnosticsapi.File{Name: fmt.Sprintf("p%d/f%d", page, i), Text: text}
	}

	return diagnosticsapi.Page{Files: files, Cursor: strconv.Itoa(page + 1)}
}

// Entries with no text add no file, so a module cannot fill memory with
// names alone, and a module that names more files than the limit is cut
// there.
func TestAModuleIsHeldToTheFileLimit(t *testing.T) {
	t.Parallel()

	t.Run("empty entries", func(t *testing.T) {
		t.Parallel()

		held := collectExtension(t.Context(), func(_ context.Context, cursor string) (diagnosticsapi.Page, error) {
			at, _ := strconv.Atoi(cursor)

			return many(at, ""), nil
		})

		require.Empty(t, held.files)
		require.Zero(t, held.size)
		require.Empty(t, held.index)
		require.Equal(t, limitPages, held.limit)
	})

	t.Run("one line each", func(t *testing.T) {
		t.Parallel()

		held := collectExtension(t.Context(), func(_ context.Context, cursor string) (diagnosticsapi.Page, error) {
			at, _ := strconv.Atoi(cursor)

			return many(at, "x\n"), nil
		})

		require.Len(t, held.files, maxExtensionFiles)
		require.Equal(t, extensionTruncated, held.reason)
		require.Equal(t, limitFiles, held.limit)
	})
}

// A page with more entries than a page may hold is a failure, even when the
// module answered it without going through diagnosticsapi.Serve.
func TestAnOversizedPageFailsTheModule(t *testing.T) {
	t.Parallel()

	page := many(0, "x\n")
	page.Files = append(page.Files, diagnosticsapi.File{Name: "one-more", Text: "x\n"})
	page.Cursor = ""

	held := collectExtension(t.Context(), pages(page))

	require.Equal(t, extensionFailed, held.reason)
	require.Empty(t, held.files)
}

// A name that differs from an earlier one only in case, or that uses an
// earlier file as a folder, or the other way round, would overwrite a file
// when the zip is opened. It is refused and counted (ADR-20261019-nrq rule 3).
func TestANameThatClashesIsRefused(t *testing.T) {
	t.Parallel()

	held := collectExtension(t.Context(), pages(diagnosticsapi.Page{Files: []diagnosticsapi.File{
		{Name: "a.log", Text: "1\n"},
		{Name: "A.LOG", Text: "2\n"},
		{Name: "a.log/b", Text: "3\n"},
		{Name: "dir/x.log", Text: "4\n"},
		{Name: "DIR", Text: "5\n"},
		{Name: "a.log", Text: "6\n"},
	}}))

	require.Equal(t, [][2]string{{"a.log", "1\n6\n"}, {"dir/x.log", "4\n"}}, text(held))
	require.Equal(t, 3, held.refused)
}

// A module that fails keeps what it gave and names its fault code, and the
// bundle is still built (ADR-20261019-nrq rule 6).
func TestAModuleThatFailsKeepsWhatItGave(t *testing.T) {
	t.Parallel()

	calls := 0
	failing := func(context.Context, string) (diagnosticsapi.Page, error) {
		calls++
		if calls == 1 {
			return diagnosticsapi.Page{Files: []diagnosticsapi.File{{Name: "a.log", Text: "kept\n"}}, Cursor: "2"}, nil
		}

		return diagnosticsapi.Page{}, fault.New(authz.CodeAccessDenied(), "refused").
			Wrap(errors.New("the process exited"))
	}

	held := collectExtension(t.Context(), failing)

	require.Equal(t, extensionFailed, held.reason)
	require.Equal(t, fmt.Sprint(authz.CodeAccessDenied()), held.code)
	require.Equal(t, [][2]string{{"a.log", "kept\n"}}, text(held))
}

// source is a module that answers the pages given.
func source(t *testing.T, instance, title string, answers ...diagnosticsapi.Page) ExtensionSource {
	t.Helper()

	r, err := ref.Parse(instance)
	require.NoError(t, err)

	return ExtensionSource{Instance: r, Title: title, Collect: pages(answers...)}
}

func manifestOf(t *testing.T, files map[string]string) map[string]any {
	t.Helper()

	var manifest map[string]any
	require.NoError(t, json.Unmarshal([]byte(files["manifest.json"]), &manifest))

	return manifest
}

// A module's files are scrubbed line by line and swept, written under its
// folder, and listed in manifest.json, and only when the admin asked.
func TestAModulesFilesAreScrubbedAndOnlyIncludedWhenAsked(t *testing.T) {
	t.Parallel()

	src := Sources{
		Version: "0.0.1",
		Started: time.Unix(0, 0),
		Secrets: []string{"PLANTED-MODULE-SECRET"},
		Extensions: []ExtensionSource{source(t, "sink:buddy-debug-logs", "Printer logs", diagnosticsapi.Page{
			Files: []diagnosticsapi.File{{
				Name: "syslog/mk4.log",
				Text: "Connect: dial 10.9.8.7 failed\nWUI: token PLANTED-MODULE-SECRET from aa:bb:cc:dd:ee:ff\n",
			}},
		})},
	}

	for _, included := range []bool{true, false} {
		bundle, err := Build(
			t.Context(),
			src,
			Options{Extensions: included, Now: time.Unix(1, 0), Salt: []byte("salt")},
		)
		require.NoError(t, err)

		files := unzip(t, bundle)
		manifest := manifestOf(t, files)

		log, written := files["modules/sink.buddy-debug-logs/syslog/mk4.log"]
		if !included {
			require.False(t, written, "a module file was written without being asked for")
			require.Equal(t, "not included", manifest["extensions"])

			continue
		}

		require.True(t, written)
		require.Contains(t, log, "Connect: dial <ip> failed\n")
		require.Contains(t, log, "<mac>")
		require.NotContains(t, log, "10.9.8.7")
		require.NotContains(t, log, "PLANTED-MODULE-SECRET")
		require.NotContains(t, log, "aa:bb:cc:dd:ee:ff")

		listed := manifest["extensions"].([]any)[0].(map[string]any)
		require.Equal(t, "sink:buddy-debug-logs", listed["instance"])
		require.Equal(t, "Printer logs", listed["title"])
	}
}

// Two instances of different kinds may share a name, and each keeps its own
// folder.
func TestInstancesOfTwoKindsWithOneNameKeepTheirOwnFolders(t *testing.T) {
	t.Parallel()

	page := func(text string) diagnosticsapi.Page {
		return diagnosticsapi.Page{Files: []diagnosticsapi.File{{Name: "a.log", Text: text}}}
	}

	bundle, err := Build(t.Context(), Sources{Extensions: []ExtensionSource{
		source(t, "sink:logs", "Sink", page("sink\n")),
		source(t, "driver:logs", "Driver", page("driver\n")),
	}}, Options{Extensions: true, Now: time.Unix(1, 0), Salt: []byte("salt")})
	require.NoError(t, err)

	files := unzip(t, bundle)
	require.Equal(t, "sink", files["modules/sink.logs/a.log"])
	require.Equal(t, "driver", files["modules/driver.logs/a.log"])
}

// A file name a module built from a MAC, an address or a secret is written
// under a hash of that part, and manifest.json names the same hash.
func TestAModulesFileNamesAreScrubbed(t *testing.T) {
	t.Parallel()

	const secret = "PLANTED-NAME-SECRET"

	names := []string{
		"syslog/aa-bb-cc-dd-ee-ff.log",
		"syslog/192.168.1.20.log",
		"syslog/" + secret + ".log",
		"syslog/printer-1.log",
	}

	page := diagnosticsapi.Page{}
	for _, name := range names {
		page.Files = append(page.Files, diagnosticsapi.File{Name: name, Text: "line\n"})
	}

	bundle, err := Build(t.Context(), Sources{
		Secrets:    []string{secret},
		Extensions: []ExtensionSource{source(t, "sink:buddy-debug-logs", "Printer logs", page)},
	}, Options{Extensions: true, Now: time.Unix(1, 0), Salt: []byte("salt")})
	require.NoError(t, err)

	files := unzip(t, bundle)
	s := NewScrubber([]byte("salt"), []string{secret})

	for _, raw := range []string{"aa-bb-cc-dd-ee-ff", "192.168.1.20", secret} {
		for name := range files {
			require.NotContains(t, name, raw)
		}

		require.NotContains(t, files["manifest.json"], raw)

		hashed := "modules/sink.buddy-debug-logs/syslog/h-" + strings.TrimPrefix(s.Hash(raw+".log"), "h:") + ".log"
		require.Equal(t, "line", files[hashed], "no file at %s", hashed)
	}

	require.Contains(t, files, "modules/sink.buddy-debug-logs/syslog/printer-1.log")

	listed := manifestOf(t, files)["extensions"].([]any)[0].(map[string]any)["files"].(map[string]any)
	for name := range listed {
		require.Contains(t, files, "modules/sink.buddy-debug-logs/"+name)
	}

	require.Len(t, listed, len(names))
}

// A long line of a module file is cut after the sweep, so the cut cannot
// leave the head of a known secret behind (the same order as the core's
// logs).
func TestAModuleLineIsClippedAfterTheSweep(t *testing.T) {
	t.Parallel()

	secret := "planted-secret-value"
	s := NewScrubber([]byte("salt"), []string{secret})
	line := strings.Repeat("a", maxLineBytes-5) + secret + strings.Repeat("b", 100) + "\n"

	scrubbed, err := scrubLines(t.Context(), s, line)
	require.NoError(t, err)

	require.NotContains(t, scrubbed, "plant")
	require.True(t, strings.HasSuffix(scrubbed, machinetext.Marker+"\n"))
}

// slow is a module that answers a first page and then nothing until its
// context ends or, when done is not zero, until done has passed. It records
// the error it saw.
func slow(t *testing.T, instance string, done time.Duration, seen *[]error) ExtensionSource {
	t.Helper()

	r, err := ref.Parse(instance)
	require.NoError(t, err)

	return ExtensionSource{Instance: r, Title: instance, Collect: func(ctx context.Context, cursor string) (
		diagnosticsapi.Page, error,
	) {
		if cursor == "" {
			return diagnosticsapi.Page{Files: []diagnosticsapi.File{{Name: "a.log", Text: "first\n"}}, Cursor: "1"}, nil
		}

		var finished <-chan time.Time
		if done > 0 {
			finished = time.After(done)
		}

		select {
		case <-finished:
			return diagnosticsapi.Page{}, nil
		case <-ctx.Done():
		}

		*seen = append(*seen, ctx.Err())

		return diagnosticsapi.Page{}, fmt.Errorf("call the module: %w", ctx.Err())
	}}
}

// A module gets a minute and all modules three. A module that runs out keeps
// what it gave and is recorded as truncated by time, the modules after the
// three minutes are skipped, and the bundle is still built. The module sees a
// cancel, never a deadline, so its breaker does not count the bundle's choice
// (ADR-20261019-nrq rule 6).
func TestModulesAreHeldToTheirTime(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var seen []error

		started := time.Now()

		bundle, err := Build(t.Context(), Sources{Extensions: []ExtensionSource{
			slow(t, "sink:a", 0, &seen),
			slow(t, "sink:b", 50*time.Second, &seen),
			slow(t, "sink:c", 50*time.Second, &seen),
			slow(t, "sink:d", 50*time.Second, &seen),
			slow(t, "sink:e", 50*time.Second, &seen),
		}}, Options{Extensions: true, Now: time.Unix(1, 0), Salt: []byte("salt")})
		require.NoError(t, err)

		require.Equal(t, maxCollectTime, time.Since(started))

		files := unzip(t, bundle)
		listed := manifestOf(t, files)["extensions"].([]any)

		for i, want := range []map[string]any{
			{"reason": extensionTruncated, "limit": limitTime},
			{},
			{},
			{"reason": extensionTruncated, "limit": limitTime},
		} {
			entry := listed[i].(map[string]any)
			require.Equal(t, want["reason"], entry["reason"], entry["instance"])
			require.Equal(t, want["limit"], entry["limit"], entry["instance"])
			folder := strings.Replace(entry["instance"].(string), ":", ".", 1)
			require.Equal(t, "first", files["modules/"+folder+"/a.log"])
		}

		require.Equal(t, extensionSkipped, listed[4].(map[string]any)["reason"])
		require.NotContains(t, files, "modules/sink.e/a.log")

		require.Len(t, seen, 2)

		for _, err := range seen {
			require.ErrorIs(t, err, context.Canceled)
			require.NotErrorIs(t, err, context.DeadlineExceeded)
		}
	})
}

// A request with a deadline leaves half its time to build and send the
// bundle, and the module still sees a cancel and not the request's deadline.
func TestModulesLeaveHalfTheRequestsTime(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var seen []error

		ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
		defer cancel()

		started := time.Now()

		_, err := Build(ctx, Sources{Extensions: []ExtensionSource{slow(t, "sink:a", 0, &seen)}},
			Options{Extensions: true, Now: time.Unix(1, 0), Salt: []byte("salt")})
		require.NoError(t, err)

		require.Equal(t, 15*time.Second, time.Since(started))
		require.Len(t, seen, 1)
		require.ErrorIs(t, seen[0], context.Canceled)
	})
}

// A request that ends while a module is asked stops the bundle, and the
// module sees a cancel.
func TestARequestThatEndsStopsTheBundle(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var seen []error

		ctx, cancel := context.WithCancel(t.Context())
		time.AfterFunc(10*time.Second, cancel)

		_, err := Build(ctx, Sources{Extensions: []ExtensionSource{slow(t, "sink:a", 0, &seen)}},
			Options{Extensions: true, Now: time.Unix(1, 0), Salt: []byte("salt")})
		require.ErrorIs(t, err, context.Canceled)
		require.Len(t, seen, 1)
		require.ErrorIs(t, seen[0], context.Canceled)
	})
}

// A known secret can hold a '/', which splits it across the parts of a file
// name. The name is then hashed whole, in the zip and in manifest.json.
func TestAFileNameHoldingASecretAcrossItsPartsIsHashedWhole(t *testing.T) {
	t.Parallel()

	const secret = "abc/def1"

	bundle, err := Build(t.Context(), Sources{
		Secrets: []string{secret},
		Extensions: []ExtensionSource{source(t, "sink:buddy-debug-logs", "Printer logs", diagnosticsapi.Page{
			Files: []diagnosticsapi.File{{Name: "logs/" + secret + ".log", Text: "line\n"}},
		})},
	}, Options{Extensions: true, Now: time.Unix(1, 0), Salt: []byte("salt")})
	require.NoError(t, err)

	files := unzip(t, bundle)
	for name := range files {
		require.NotContains(t, name, "abc/def1")
	}

	require.NotContains(t, files["manifest.json"], "abc/def1")

	s := NewScrubber([]byte("salt"), []string{secret})
	hashed := "modules/sink.buddy-debug-logs/h-" + strings.TrimPrefix(s.Hash("logs/"+secret+".log"), "h:")
	require.Equal(t, "line", files[hashed], "no file at %s", hashed)
}
