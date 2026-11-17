// Package lane marks a command no person is waiting for. The host sets the mark
// and a driver that serialises commands reads it, and neither may import the
// other.
package lane

import (
	"context"
	"sync"
)

// Background marks a command no person is waiting for, such as a listing read
// to warm a cache. A driver that serialises commands may send a person's
// command ahead of it, until Promote says a person has started waiting on it
// after all. A driver that has one lane ignores the mark.
type Background struct {
	promoted chan struct{}
	once     sync.Once
}

// NewBackground returns a mark that has not been promoted.
func NewBackground() *Background {
	return &Background{promoted: make(chan struct{})}
}

type backgroundKey struct{}

// WithBackground returns ctx carrying b, which a driver reads with BackgroundOf.
func WithBackground(ctx context.Context, b *Background) context.Context {
	return context.WithValue(ctx, backgroundKey{}, b)
}

// BackgroundOf returns the mark ctx carries, and nil when a person is waiting
// for the command.
func BackgroundOf(ctx context.Context) *Background {
	b, _ := ctx.Value(backgroundKey{}).(*Background)

	return b
}

// Promote says a person is now waiting for the command. It is safe to call on
// nil and more than once.
func (b *Background) Promote() {
	if b == nil {
		return
	}

	b.once.Do(func() { close(b.promoted) })
}

// Promoted closes when Promote is first called. On nil it is nil, which a
// select never picks.
func (b *Background) Promoted() <-chan struct{} {
	if b == nil {
		return nil
	}

	return b.promoted
}
