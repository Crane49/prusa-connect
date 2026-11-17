package mini404

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestAnUnauthorisedReplyMeansTheStackIsUp(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)

	ctx, cancel := contextWithBudget(t)
	defer cancel()
	if err := WaitPrusaLink(ctx, portOf(t, srv.URL)); err != nil {
		t.Fatalf("a 401 was not read as a live stack: %v", err)
	}
}

func TestAPortNothingListensOnNamesThePhase(t *testing.T) {
	t.Parallel()

	// A port reserved and released, so the probe fails on connect rather than
	// on a response.
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr, ok := l.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("a tcp4 listener reported a %T address", l.Addr())
	}
	if closeErr := l.Close(); closeErr != nil {
		t.Fatalf("close: %v", closeErr)
	}

	ctx, cancel := context.WithTimeout(t.Context(), readinessPoll)
	defer cancel()
	err = WaitPrusaLink(ctx, addr.Port)
	if err == nil {
		t.Fatal("a dead port was reported as ready")
	}
	if !strings.Contains(err.Error(), "prusalink-http") {
		t.Errorf("the error does not name the phase: %v", err)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("the error does not carry the deadline: %v", err)
	}
}

func TestAProbeThatSucceedsOnTheSecondPollReturnsNil(t *testing.T) {
	t.Parallel()

	calls := 0
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	err := WaitFunc(ctx, "second-try", func(context.Context) error {
		calls++
		if calls < 2 {
			return errors.New("not yet")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("a probe that came good on the second poll was reported as a failure: %v", err)
	}
	if calls != 2 {
		t.Errorf("the probe ran %d times and should have run twice", calls)
	}
}

func TestAPhaseThatNeverCompletesCarriesTheLastProbeError(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), readinessPoll)
	defer cancel()

	err := WaitFunc(ctx, "eeprom-journal", func(context.Context) error {
		return errors.New("the journal is still replaying")
	})
	if err == nil {
		t.Fatal("a probe that never succeeded was reported as ready")
	}
	for _, want := range []string{"eeprom-journal", "the journal is still replaying"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error does not carry %q: %v", want, err)
		}
	}
}

func portOf(t *testing.T, rawURL string) int {
	t.Helper()

	_, port, err := net.SplitHostPort(strings.TrimPrefix(rawURL, "http://"))
	if err != nil {
		t.Fatalf("split %s: %v", rawURL, err)
	}
	n, err := strconv.Atoi(port)
	if err != nil {
		t.Fatalf("parse the port of %s: %v", rawURL, err)
	}
	return n
}
