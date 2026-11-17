package wirerpc

import (
	"errors"
	"fmt"

	"github.com/prusa3d/prusa-connect/sdk/wire"
)

// The refusals credit accounting returns.
var (
	// ErrCreditOverflow reports a grant that would take one stream's total
	// past wire.MaxCredit. It is a protocol error.
	ErrCreditOverflow = errors.New("wirerpc: credit past the largest total one stream holds")

	// ErrCreditOverspent reports a sender that sent more payload bytes than
	// its balance covered. The receiver stops the connection.
	ErrCreditOverspent = errors.New("wirerpc: a sender spent credit it was not granted")
)

// Balance is the credit one sender has on one stream. A receiver grants
// payload bytes and a sender spends them. Only a data frame spends.
//
// The zero Balance is empty. A Balance has no lock, so a caller that shares
// one across goroutines guards it.
type Balance struct {
	held uint64
}

// Available reports how many payload bytes the sender may still send.
func (b *Balance) Available() uint32 {
	// Grant refuses a total past wire.MaxCredit and Spend never takes the
	// total below zero, so the held value is always inside a uint32.
	//nolint:gosec // Grant refuses a total past wire.MaxCredit, so this fits.
	return uint32(b.held)
}

// Grant adds a peer's grant to the balance. A grant of zero is refused.
func (b *Balance) Grant(n uint32) error {
	if n == 0 {
		return wire.ErrCreditZero
	}

	total := b.held + uint64(n)
	if total > wire.MaxCredit {
		return fmt.Errorf("%d bytes on top of %d: %w", n, b.held, ErrCreditOverflow)
	}

	b.held = total

	return nil
}

// Spend subtracts the payload size of a data frame. It refuses a frame the
// balance does not cover and leaves the balance unchanged.
func (b *Balance) Spend(n int) error {
	if n < 0 || uint64(n) > b.held {
		return fmt.Errorf("%d bytes against a balance of %d: %w", n, b.held, ErrCreditOverspent)
	}

	b.held -= uint64(n)

	return nil
}
