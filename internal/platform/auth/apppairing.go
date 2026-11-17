package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"
)

// An app pairing is how an app gets an api credential without anybody copying
// a token by hand. It follows the device authorization grant of RFC 8628. The
// app asks for a pairing and gets two codes. It shows the short one to a
// person, who confirms it in the browser, where this server shows the app's
// name and the scopes it asked for. Meanwhile the app polls with the long code,
// and once the person approves, its next poll collects a credential with the
// scopes the person approved.
//
// Nothing here knows which app is asking. The name is what the app says about
// itself, and the person who reads it decides whether to believe it.

// The bounds of a pairing. RFC 8628 section 3.2 leaves them to the server.
const (
	// AppPairingLifetime is how long a pairing waits for a decision and then
	// for its collection. A person reads the code off one screen and types
	// it into another, which takes a minute, not an hour.
	AppPairingLifetime = 10 * time.Minute
	// AppPairingInterval is how long an app waits between two polls. RFC 8628
	// section 3.5 names five seconds as the default.
	AppPairingInterval = 5 * time.Second
	// AppPairingSlowDown is what a poll that came too soon adds to the
	// interval, as RFC 8628 section 3.5 asks.
	AppPairingSlowDown = 5 * time.Second
	// MaxPendingAppPairings bounds the pairings waiting at once over the whole
	// server. Asking for one takes no credential, so the table must not grow
	// at the pace somebody on the network can send requests.
	MaxPendingAppPairings = 32
	// MaxAppNameLength bounds the name an app gives itself, in bytes.
	MaxAppNameLength = 64
)

// userCodeAlphabet is the consonants without the ones that read alike, the
// alphabet RFC 8628 section 6.1 suggests. No vowel, so no word is spelled.
const userCodeAlphabet = "BCDFGHJKLMNPQRSTVWXZ"

// userCodeLength is eight characters of that alphabet, 34 bits. A guess has to
// be confirmed by a signed-in person in the browser, so it is not the code
// that keeps a stranger out.
const userCodeLength = 8

// deviceCodeBytes is the random part of the code an app polls with, 256 bits.
const deviceCodeBytes = 32

// The states a pairing moves through. A pairing that was collected or that
// expired has no row at all.
const (
	AppPairingPending  = "pending"
	AppPairingApproved = "approved"
	AppPairingDenied   = "denied"
)

// The answers a poll gives besides a credential. Each is the error RFC 8628
// section 3.5 names, so a client written to the RFC reads them.
var (
	// ErrAppPairingPending is a pairing nobody has decided yet.
	ErrAppPairingPending = errors.New("auth: the pairing waits for a person to decide")
	// ErrAppPairingSlowDown is a poll that came before the interval passed.
	ErrAppPairingSlowDown = errors.New("auth: the app polls faster than it was told to")
	// ErrAppPairingDenied is a pairing a person refused. It is answered once,
	// and the pairing is gone after it.
	ErrAppPairingDenied = errors.New("auth: a person denied the pairing")
	// ErrAppPairingExpired is a pairing past its lifetime.
	ErrAppPairingExpired = errors.New("auth: the pairing expired")
	// ErrAppPairingUnknown is a code no pairing answers to: one never issued,
	// or one already collected. The two are one answer, so a code works once.
	ErrAppPairingUnknown = errors.New("auth: no pairing answers to that code")
	// ErrAppPairingBusy is a request refused because too many pairings are
	// already waiting.
	ErrAppPairingBusy = errors.New("auth: too many pairings are waiting")
	// ErrAppPairingDecided is a decision on a pairing that is no longer
	// waiting for one.
	ErrAppPairingDecided = errors.New("auth: the pairing is no longer waiting for a decision")
)

// AppPairing is one pairing's row. It holds no code an app could poll with:
// DeviceDigest is the SHA-256 of that code.
type AppPairing struct {
	DeviceDigest string
	UserCode     string
	AppName      string
	// Scopes are what the app asked for.
	Scopes        []string
	ClientAddress string
	CreatedAt     time.Time
	ExpiresAt     time.Time
	Interval      time.Duration
	PolledAt      time.Time
	State         string
	// DecidedBy is the graph subject of the person who decided, and
	// DecidedScopes the scopes they approved.
	DecidedBy     string
	DecidedScopes []string
	// DecidedGeneration is that person's generation, and DecidedRoot with
	// RootGeneration the root of their sign-in, read before the decision.
	DecidedGeneration int64
	DecidedRoot       string
	RootGeneration    int64
}

// AppPairingStore holds pairing rows. Every method that changes a row names
// the state it expects, so two processes over one database cannot both decide
// one pairing or both collect one credential.
type AppPairingStore interface {
	// CreateAppPairing writes a new row. A user code another row holds is
	// ErrUserCodeTaken, and the caller draws another code.
	CreateAppPairing(ctx context.Context, pairing AppPairing) error
	// CountAppPairings counts the rows not yet expired at now.
	CountAppPairings(ctx context.Context, now time.Time) (int, error)
	// AppPairingByUserCode and AppPairingByDevice find one row, or report
	// ErrAppPairingUnknown.
	AppPairingByUserCode(ctx context.Context, userCode string) (AppPairing, error)
	AppPairingByDevice(ctx context.Context, deviceDigest string) (AppPairing, error)
	// DecideAppPairing moves a pending row to the decision's state, and
	// reports false when the row was not pending or not live at now.
	DecideAppPairing(ctx context.Context, decision AppPairing, now time.Time) (bool, error)
	// PolledAppPairing records a poll and the interval the next one waits.
	PolledAppPairing(ctx context.Context, deviceDigest string, polled time.Time, interval time.Duration) error
	// TakeAppPairing deletes the row if it is in the given state, and reports
	// whether this call deleted it. Only one caller deletes it, so a code
	// works once.
	TakeAppPairing(ctx context.Context, deviceDigest, state string) (bool, error)
	// DeleteExpiredAppPairings removes the rows past their expiry.
	DeleteExpiredAppPairings(ctx context.Context, now time.Time) error
}

// ErrUserCodeTaken reports a user code another live pairing holds. A request
// draws again.
var ErrUserCodeTaken = errors.New("auth: the user code is taken")

// AppPairingRequest is what an app sends to ask.
type AppPairingRequest struct {
	AppName string
	Scopes  []string
	// ClientAddress is where the request came from, for the person deciding.
	ClientAddress string
}

// IssuedAppPairing is a new pairing and the code the app polls with. The code
// is returned once.
type IssuedAppPairing struct {
	AppPairing

	DeviceCode string
}

// drawAttempts is how many user codes a request draws before it gives up. A
// collision needs two live pairings out of 20^8 codes, so a second draw is
// already rare.
const drawAttempts = 4

// RequestAppPairing opens a pairing. The caller has already checked the name
// and the scopes.
func RequestAppPairing(
	ctx context.Context, store AppPairingStore, request AppPairingRequest, now time.Time,
) (IssuedAppPairing, error) {
	if err := store.DeleteExpiredAppPairings(ctx, now); err != nil {
		return IssuedAppPairing{}, fmt.Errorf("sweep the expired pairings: %w", err)
	}

	waiting, err := store.CountAppPairings(ctx, now)
	if err != nil {
		return IssuedAppPairing{}, fmt.Errorf("count the waiting pairings: %w", err)
	}

	if waiting >= MaxPendingAppPairings {
		return IssuedAppPairing{}, ErrAppPairingBusy
	}

	deviceCode, err := randomDeviceCode()
	if err != nil {
		return IssuedAppPairing{}, err
	}

	pairing := AppPairing{
		DeviceDigest:  DeviceCodeDigest(deviceCode),
		AppName:       request.AppName,
		Scopes:        append([]string(nil), request.Scopes...),
		ClientAddress: request.ClientAddress,
		CreatedAt:     now,
		ExpiresAt:     now.Add(AppPairingLifetime),
		Interval:      AppPairingInterval,
		// One interval back, so the first poll is never too soon. The
		// interval paces the polls against each other, and an app that
		// polls the moment it has its codes has done nothing wrong yet.
		PolledAt: now.Add(-AppPairingInterval),
		State:    AppPairingPending,
	}

	for range drawAttempts {
		pairing.UserCode, err = randomUserCode()
		if err != nil {
			return IssuedAppPairing{}, err
		}

		err = store.CreateAppPairing(ctx, pairing)
		if !errors.Is(err, ErrUserCodeTaken) {
			break
		}
	}

	if err != nil {
		return IssuedAppPairing{}, fmt.Errorf("write the pairing: %w", err)
	}

	return IssuedAppPairing{AppPairing: pairing, DeviceCode: deviceCode}, nil
}

// LiveAppPairing finds a pairing by its user code for the person who is about
// to decide. One past its expiry is unknown.
func LiveAppPairing(ctx context.Context, store AppPairingStore, userCode string, now time.Time) (AppPairing, error) {
	pairing, err := store.AppPairingByUserCode(ctx, NormalizeUserCode(userCode))
	if err != nil {
		return AppPairing{}, err
	}

	if !now.Before(pairing.ExpiresAt) {
		return AppPairing{}, ErrAppPairingUnknown
	}

	return pairing, nil
}

// DecideAppPairing records a person's answer to a pending pairing. The
// decision carries the user code, the state, who decided, the scopes approved
// and the generations read before the check that allowed it.
func DecideAppPairing(ctx context.Context, store AppPairingStore, decision AppPairing, now time.Time) error {
	decision.UserCode = NormalizeUserCode(decision.UserCode)

	decided, err := store.DecideAppPairing(ctx, decision, now)
	if err != nil {
		return fmt.Errorf("record the decision: %w", err)
	}

	if !decided {
		return ErrAppPairingDecided
	}

	return nil
}

// PollAppPairing is one poll of an app. It returns the approved pairing once,
// and the caller mints the credential from it. Every other answer is one of
// the errors above.
func PollAppPairing(ctx context.Context, store AppPairingStore, deviceCode string, now time.Time) (AppPairing, error) {
	digest := DeviceCodeDigest(deviceCode)

	pairing, err := store.AppPairingByDevice(ctx, digest)
	if err != nil {
		return AppPairing{}, err
	}

	if !now.Before(pairing.ExpiresAt) {
		if _, takeErr := store.TakeAppPairing(ctx, digest, pairing.State); takeErr != nil {
			return AppPairing{}, fmt.Errorf("remove the expired pairing: %w", takeErr)
		}

		return AppPairing{}, ErrAppPairingExpired
	}

	if now.Before(pairing.PolledAt.Add(pairing.Interval)) {
		if err = store.PolledAppPairing(ctx, digest, now, pairing.Interval+AppPairingSlowDown); err != nil {
			return AppPairing{}, fmt.Errorf("record the poll: %w", err)
		}

		return AppPairing{}, ErrAppPairingSlowDown
	}

	if err = store.PolledAppPairing(ctx, digest, now, pairing.Interval); err != nil {
		return AppPairing{}, fmt.Errorf("record the poll: %w", err)
	}

	switch pairing.State {
	case AppPairingPending:
		return AppPairing{}, ErrAppPairingPending
	case AppPairingDenied, AppPairingApproved:
		taken, takeErr := store.TakeAppPairing(ctx, digest, pairing.State)
		if takeErr != nil {
			return AppPairing{}, fmt.Errorf("collect the pairing: %w", takeErr)
		}

		// Another poll collected it between the read and the delete.
		if !taken {
			return AppPairing{}, ErrAppPairingUnknown
		}

		if pairing.State == AppPairingDenied {
			return AppPairing{}, ErrAppPairingDenied
		}

		return pairing, nil
	default:
		return AppPairing{}, fmt.Errorf("%w: the pairing is in state %q", ErrAppPairingUnknown, pairing.State)
	}
}

// DeviceCodeDigest is the stored form of the code an app polls with.
func DeviceCodeDigest(deviceCode string) string {
	sum := sha256.Sum256([]byte(deviceCode))

	return hex.EncodeToString(sum[:])
}

// NormalizeUserCode reads a user code the way a person types it: in any case,
// with or without the dash in the middle, with spaces around it.
func NormalizeUserCode(code string) string {
	var out strings.Builder

	for _, r := range strings.ToUpper(code) {
		if r == '-' || r == ' ' {
			continue
		}

		out.WriteRune(r)
	}

	return out.String()
}

// FormatUserCode writes a user code with a dash in the middle, which is how it
// is shown to a person.
func FormatUserCode(code string) string {
	if len(code) != userCodeLength {
		return code
	}

	return code[:userCodeLength/2] + "-" + code[userCodeLength/2:]
}

// randomUserCode draws a user code.
func randomUserCode() (string, error) {
	var out strings.Builder

	limit := big.NewInt(int64(len(userCodeAlphabet)))

	for range userCodeLength {
		index, err := rand.Int(rand.Reader, limit)
		if err != nil {
			return "", fmt.Errorf("draw a user code: %w", err)
		}

		out.WriteByte(userCodeAlphabet[index.Int64()])
	}

	return out.String(), nil
}

// randomDeviceCode draws the code an app polls with.
func randomDeviceCode() (string, error) {
	code, err := randomBase62(deviceCodeBytes + deviceCodeBytes/3)
	if err != nil {
		return "", fmt.Errorf("draw a device code: %w", err)
	}

	return code, nil
}
