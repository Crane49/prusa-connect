package files_test

import (
	"bytes"
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prusa3d/fractal-link/files"

	sdkfiles "github.com/prusa3d/prusa-connect/sdk/fractal/files"
)

// fetched is what one Fetch gave and what the parent saw.
type fetched struct {
	Data    string
	Kept    files.Retention
	Failure string
	Asked   []seen
}

func fetchFrom(t *testing.T, client *files.Client, base string, id sdkfiles.Identity) (string, files.Retention, error) {
	t.Helper()

	sink, err := os.CreateTemp(t.TempDir(), "object-*")
	if err != nil {
		t.Fatalf("create the sink: %v", err)
	}
	defer sink.Close()

	kept, err := client.Fetch(t.Context(), base, id, sink, false)

	data, readErr := os.ReadFile(sink.Name())
	if readErr != nil {
		t.Fatalf("read the sink: %v", readErr)
	}

	if err != nil {
		return "", kept, err
	}

	return string(data), kept, nil
}

// failureOf describes a Fetch error so a table compares it whole.
func failureOf(err error) string {
	if err == nil {
		return ""
	}

	var refused *files.FetchError
	if !errors.As(err, &refused) {
		return "not a FetchError: " + err.Error()
	}

	cause := "other"

	var host x509.HostnameError

	switch {
	case errors.As(err, &host):
		cause = "wrong host"
	default:
		for _, known := range []error{
			files.ErrNotAvailable, files.ErrMismatch, files.ErrResumesSpent,
			files.ErrUnusablePart, files.ErrRefused, files.ErrCleartext,
		} {
			if errors.Is(err, known) {
				cause = known.Error()

				break
			}
		}
	}

	after := ""
	if !refused.RetryAfter.IsZero() {
		after = ", after " + refused.RetryAfter.UTC().Format(http.TimeFormat)
	}

	return fmt.Sprintf("status %d, retryable %t%s: %s", refused.Status, refused.Retryable(), after, cause)
}

func TestFetch(t *testing.T) {
	t.Parallel()

	signer, identity := newSigner(t)
	data := []byte("GCDE a part to print")
	wrong := []byte("GCDE a part to frint")
	id := sdkfiles.Sum(data)
	far, parseErr := http.ParseTime(farDate)
	if parseErr != nil {
		t.Fatalf("parse the far date: %v", parseErr)
	}

	asked := func(pairs ...string) []seen {
		var out []seen
		for i := 0; i+1 < len(pairs); i += 2 {
			out = append(out, seen{Method: http.MethodGet, Range: pairs[i], CacheControl: pairs[i+1], Caller: identity})
		}

		return out
	}

	for name, tc := range map[string]struct {
		steps []http.HandlerFunc
		want  fetched
	}{
		"a whole object": {
			[]http.HandlerFunc{whole(data, keptFor())},
			fetched{Data: string(data), Kept: files.Retention{Unlimited: true}, Asked: asked("", "")},
		},
		"a cut transfer resumes": {
			[]http.HandlerFunc{trickle(data, 8, keptFor()), part(data, 8, keptFor())},
			fetched{Data: string(data), Kept: files.Retention{Unlimited: true}, Asked: asked("", "", "bytes=8-", "")},
		},
		"a 200 to a range replaces the bytes": {
			[]http.HandlerFunc{trickle(data, 8, keptFor()), whole(data, keptFor())},
			fetched{Data: string(data), Kept: files.Retention{Unlimited: true}, Asked: asked("", "", "bytes=8-", "")},
		},
		"the most restrictive retention wins": {
			[]http.HandlerFunc{
				trickle(data, 8, keptFor()),
				part(data, 8, http.Header{"Cache-Control": {"private"}, "Expires": {farDate}}),
			},
			fetched{Data: string(data), Kept: files.Retention{Until: far}, Asked: asked("", "", "bytes=8-", "")},
		},
		"a changed whole length": {
			[]http.HandlerFunc{trickle(data, 8, keptFor()), part(append(slices.Clone(data), "!!"...), 8, keptFor())},
			fetched{Failure: "status 0, retryable true: " + files.ErrUnusablePart.Error(), Asked: asked("", "", "bytes=8-", "")},
		},
		"a malformed Content-Range": {
			[]http.HandlerFunc{
				trickle(data, 8, keptFor()),
				status(http.StatusPartialContent, http.Header{"Content-Range": {"bytes 8-/20"}}),
			},
			fetched{Failure: "status 0, retryable true: " + files.ErrUnusablePart.Error(), Asked: asked("", "", "bytes=8-", "")},
		},
		"no-store wins over unlimited": {
			[]http.HandlerFunc{whole(data, http.Header{"Cache-Control": {"no-store, " + unlimitedDirectives}})},
			fetched{Data: string(data), Asked: asked("", "")},
		},
		"a bad Expires keeps nothing": {
			[]http.HandlerFunc{whole(data, http.Header{"Cache-Control": {"private"}, "Expires": {"not a date"}})},
			fetched{Data: string(data), Asked: asked("", "")},
		},
		"a 200 after a part starts again": {
			[]http.HandlerFunc{
				trickle(data, 4, keptFor()),
				trickle(data, 4, http.Header{"Cache-Control": {"no-store"}}),
				whole(data, keptFor()),
			},
			fetched{
				Data: string(data), Kept: files.Retention{Unlimited: true},
				Asked: asked("", "", "bytes=4-", "", "bytes=8-", ""),
			},
		},
		"a part at another place": {
			[]http.HandlerFunc{trickle(data, 8, keptFor()), part(data, 4, keptFor())},
			fetched{Failure: "status 0, retryable true: " + files.ErrUnusablePart.Error(), Asked: asked("", "", "bytes=8-", "")},
		},
		"resumes are bounded": {
			[]http.HandlerFunc{trickle(data, 3, keptFor())},
			fetched{
				Failure: "status 0, retryable true: " + files.ErrResumesSpent.Error(),
				Asked:   asked("", "", "bytes=3-", "", "bytes=6-", ""),
			},
		},
		"a mismatch asks once more fresh": {
			[]http.HandlerFunc{whole(wrong, keptFor()), whole(data, keptFor())},
			fetched{Data: string(data), Kept: files.Retention{Unlimited: true}, Asked: asked("", "", "", "no-cache")},
		},
		"two mismatches": {
			[]http.HandlerFunc{whole(wrong, keptFor())},
			fetched{Failure: "status 0, retryable true: " + files.ErrMismatch.Error(), Asked: asked("", "", "", "no-cache")},
		},
		"not available": {
			[]http.HandlerFunc{status(http.StatusNotFound, nil)},
			fetched{Failure: "status 404, retryable false: " + files.ErrNotAvailable.Error(), Asked: asked("", "")},
		},
		"busy until a date": {
			[]http.HandlerFunc{status(http.StatusServiceUnavailable, http.Header{"Retry-After": {farDate}})},
			fetched{Failure: "status 503, retryable true, after " + farDate + ": " + files.ErrRefused.Error(), Asked: asked("", "")},
		},
		"a redirect is not followed": {
			[]http.HandlerFunc{status(http.StatusFound, http.Header{"Location": {"/objects/" + id.Text()}})},
			fetched{Failure: "status 302, retryable true: " + files.ErrRefused.Error(), Asked: asked("", "")},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			parent := newScripted(t, tc.steps...)
			client := files.NewClient(files.ClientConfig{
				Signer: signer, Anchors: parent.anchors(), MaxResumes: 2, Timeout: 10 * time.Second,
			})
			t.Cleanup(client.CloseIdleConnections)

			got, kept, err := fetchFrom(t, client, parent.srv.URL, id)
			result := fetched{Data: got, Kept: kept, Failure: failureOf(err), Asked: parent.requests()}

			if err != nil {
				result.Kept = files.Retention{}
			}

			if !reflect.DeepEqual(result, tc.want) {
				t.Errorf("Fetch gave %+v, want %+v", result, tc.want)
			}
		})
	}
}

func TestFetchStalledParent(t *testing.T) {
	t.Parallel()

	signer, identity := newSigner(t)
	data := []byte("GCDE a part to print")
	parent := newScripted(t, stall(data, 4))
	client := files.NewClient(files.ClientConfig{
		Signer: signer, Anchors: parent.anchors(), MaxResumes: 1, Timeout: 300 * time.Millisecond,
	})
	t.Cleanup(client.CloseIdleConnections)

	done := make(chan fetched, 1)

	go func() {
		_, _, err := fetchFrom(t, client, parent.srv.URL, sdkfiles.Sum(data))
		done <- fetched{Failure: failureOf(err), Asked: parent.requests()}
	}()

	select {
	case got := <-done:
		want := fetched{
			Failure: "status 0, retryable true: " + files.ErrResumesSpent.Error(),
			Asked: []seen{
				{Method: http.MethodGet, Caller: identity},
				{Method: http.MethodGet, Range: "bytes=4-", Caller: identity},
			},
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("Fetch gave %+v, want %+v", got, want)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Fetch still waits for a parent that stopped sending")
	}
}

// A caller that pauses between two reads of a body, as a relay does while
// its child is slow, does not cut the request to the parent. Only a parent
// that sends nothing does.
func TestGetWaitsForASlowReader(t *testing.T) {
	t.Parallel()

	signer, _ := newSigner(t)
	// More than the socket buffers hold, so the parent is still sending
	// while the caller pauses.
	data := bytes.Repeat([]byte("GCDE a part to print "), 2<<20)
	parent := newScripted(t, whole(data, keptFor()))
	client := files.NewClient(
		files.ClientConfig{Signer: signer, Anchors: parent.anchors(), Timeout: 200 * time.Millisecond},
	)
	t.Cleanup(client.CloseIdleConnections)

	answer, err := client.Get(t.Context(), parent.srv.URL, sdkfiles.Sum(data), files.GetOptions{})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer answer.Body.Close()

	first := make([]byte, 4)
	if _, err = io.ReadFull(answer.Body, first); err != nil {
		t.Fatalf("read the first bytes: %v", err)
	}

	time.Sleep(600 * time.Millisecond) // the pause under test, three times the client's timeout

	rest, err := io.ReadAll(answer.Body)
	got := slices.Concat(first, rest)
	if err != nil || !bytes.Equal(got, data) {
		t.Errorf("the body after a pause is %d bytes and %v, want %d bytes", len(got), err, len(data))
	}
}

func TestFetchChannel(t *testing.T) {
	t.Parallel()

	signer, _ := newSigner(t)
	data := []byte("GCDE over the wire")
	id := sdkfiles.Sum(data)

	plain := httptest.NewServer(whole(data, keptFor()))
	t.Cleanup(plain.Close)

	secure := newScripted(t, whole(data, keptFor()))
	localhost := strings.Replace(secure.srv.URL, "127.0.0.1", "localhost", 1)

	for name, tc := range map[string]struct {
		base   string
		config files.ClientConfig
		want   string
	}{
		"cleartext refused": {plain.URL, files.ClientConfig{Signer: signer}, "status 0, retryable false: " + files.ErrCleartext.Error()},
		"cleartext allowed": {plain.URL, files.ClientConfig{Signer: signer, AllowCleartext: true}, ""},
		"another host":      {localhost, files.ClientConfig{Signer: signer, Anchors: secure.anchors()}, "status 0, retryable true: wrong host"},
		"an unknown authority": {secure.srv.URL, files.ClientConfig{Signer: signer, Anchors: x509.NewCertPool()},
			"status 0, retryable true: other"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			client := files.NewClient(tc.config)
			t.Cleanup(client.CloseIdleConnections)

			_, _, err := fetchFrom(t, client, tc.base, id)
			if got := failureOf(err); got != tc.want {
				t.Errorf("Fetch failed with %q, want %q", got, tc.want)
			}
		})
	}
}

// rejoined is what each Fetch gave after a 401, in order, how often the
// client ran the join exchange, how often the parent was asked, and what the
// client logged.
type rejoined struct {
	Results []string
	Rejoins int
	Asked   int
	Logged  []string
}

func TestFetchRejoinsAfterA401(t *testing.T) {
	t.Parallel()

	signer, _ := newSigner(t)
	data := []byte("GCDE a part to print")
	id := sdkfiles.Sum(data)
	unauthorized := status(http.StatusUnauthorized, nil)
	skewed := status(http.StatusUnauthorized, http.Header{"Date": {farDate}})
	joinFailed := errors.New("the parent did not answer the join request")
	refused := "status 401, retryable false: " + files.ErrRefused.Error()
	rejoinLine := `level=INFO msg="this server ran the join exchange again after a 401 from the parent"`

	for name, tc := range map[string]struct {
		steps   []http.HandlerFunc
		joinErr error
		fetches int
		// together runs the fetches at once, and the join exchange waits
		// until every one of them got its 401.
		together bool
		want     rejoined
	}{
		"a 401 runs the join exchange and asks again": {
			steps: []http.HandlerFunc{unauthorized, whole(data, keptFor())}, fetches: 1,
			want: rejoined{Results: []string{string(data)}, Rejoins: 1, Asked: 2, Logged: []string{rejoinLine}},
		},
		"a skewed clock is logged": {
			steps: []http.HandlerFunc{skewed, whole(data, keptFor())}, fetches: 1,
			want: rejoined{Results: []string{string(data)}, Rejoins: 1, Asked: 2, Logged: []string{
				`level=WARN msg="the clock of this server differs from the clock of the parent by more than ` +
					`files.proof_tolerance" parent="` + farDate + `"`,
				rejoinLine,
			}},
		},
		"a failed join does not ask again": {
			steps: []http.HandlerFunc{unauthorized}, joinErr: joinFailed, fetches: 1,
			want: rejoined{Results: []string{refused}, Rejoins: 1, Asked: 1, Logged: []string{
				`level=WARN msg="this server could not run the join exchange again after a 401 from the parent" ` +
					`error="` + joinFailed.Error() + `"`,
			}},
		},
		"one join per proof tolerance": {
			steps: []http.HandlerFunc{unauthorized}, fetches: 2,
			want: rejoined{Results: []string{refused, refused}, Rejoins: 1, Asked: 3, Logged: []string{rejoinLine}},
		},
		"401s at once share one join": {
			steps:   []http.HandlerFunc{unauthorized, unauthorized, unauthorized, whole(data, keptFor())},
			fetches: 3, together: true,
			want: rejoined{
				Results: []string{string(data), string(data), string(data)}, Rejoins: 1, Asked: 6,
				Logged: []string{rejoinLine},
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer

			log := slog.New(slog.NewTextHandler(&out, &slog.HandlerOptions{
				ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
					if a.Key == slog.TimeKey || a.Key == "local" {
						return slog.Attr{}
					}

					return a
				},
			}))

			var rejoins atomic.Int32

			parent := newScripted(t, tc.steps...)
			client := files.NewClient(files.ClientConfig{
				Signer: signer, Anchors: parent.anchors(), Timeout: 10 * time.Second,
				Tolerance: time.Minute, Log: log,
				Rejoin: func(context.Context) error {
					rejoins.Add(1)

					for tc.together && len(parent.requests()) < tc.fetches {
						time.Sleep(time.Millisecond)
					}

					return tc.joinErr
				},
			})
			t.Cleanup(client.CloseIdleConnections)

			results := make([]string, tc.fetches)
			fetch := func(i int) {
				got, _, err := fetchFrom(t, client, parent.srv.URL, id)
				results[i] = got + failureOf(err)
			}

			var wg sync.WaitGroup

			for i := range tc.fetches {
				if tc.together {
					wg.Go(func() { fetch(i) })
				} else {
					fetch(i)
				}
			}

			wg.Wait()

			result := rejoined{
				Results: results, Rejoins: int(rejoins.Load()), Asked: len(parent.requests()),
				Logged: strings.Split(strings.TrimSpace(out.String()), "\n"),
			}
			if !reflect.DeepEqual(result, tc.want) {
				t.Errorf("Fetch gave %+v, want %+v", result, tc.want)
			}
		})
	}
}
