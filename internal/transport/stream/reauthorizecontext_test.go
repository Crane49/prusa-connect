package stream

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/sdk/event"
)

// readerScope stands for whatever the transport put on the request context
// that opened a stream, such as the scopes of the reader's credential.
type readerScope struct{}

// contextAuthorizer records what each authorization question carried on its
// context.
type contextAuthorizer struct {
	*revocableAuthorizer

	mu   sync.Mutex
	seen []any
}

func (a *contextAuthorizer) VisibleOrigins(ctx context.Context, principal string) (Visibility, error) {
	a.mu.Lock()
	a.seen = append(a.seen, ctx.Value(readerScope{}))
	a.mu.Unlock()

	return a.revocableAuthorizer.VisibleOrigins(ctx, principal)
}

// A re-authorization asks under the context the stream was opened with, so a
// limit the transport put there, such as the scopes of a credential, still
// holds after the graph moves and the question is asked again.
func TestAReauthorizationCarriesWhatTheFirstQuestionCarried(t *testing.T) {
	t.Parallel()

	authz := &contextAuthorizer{
		revocableAuthorizer: newRevocableAuthorizer(map[string][]event.Origin{alice: {printerA, printerB}}),
	}
	hub := newTestHub(t, Options{Authorizer: authz})

	defer hub.Close()

	opened, cancel := context.WithCancel(context.WithValue(context.Background(), readerScope{}, "read"))

	sub, err := hub.Subscribe(opened, SubscribeRequest{Principal: alice, Interval: time.Hour})
	require.NoError(t, err)

	defer sub.Close()

	// The request that opened the stream is over; the stream is not.
	cancel()

	authz.grant(printerA)
	require.True(t, sub.reauthorize())

	authz.mu.Lock()
	defer authz.mu.Unlock()

	require.Equal(t, []any{"read", "read"}, authz.seen)
}
