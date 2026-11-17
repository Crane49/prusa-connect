package joinwire_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/prusa3d/fractal-link/natsbind/joinwire"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
)

// TestTheStoreWritesAndReadsBack checks that a new FileStore loads the record
// another FileStore saved in the same file.
func TestTheStoreWritesAndReadsBack(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "pending.json")
	store := joinwire.NewFileStore(path)
	held, err := store.Load(t.Context())
	if err != nil || len(held) != 0 {
		t.Fatalf("a store nothing wrote holds %v: %v", held, err)
	}
	record := joinwire.Record{
		NodeID:    "printer-1",
		Identity:  "UAAAA",
		FirstSeen: time.Unix(1, 0),
		LastSeen:  time.Unix(2, 0),
	}
	if saveErr := store.Save(t.Context(), record); saveErr != nil {
		t.Fatalf("write: %v", saveErr)
	}
	again, err := joinwire.NewFileStore(path).Load(t.Context())
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(again) != 1 || again[0].NodeID != "printer-1" || again[0].Identity != "UAAAA" {
		t.Errorf("the store holds %v, want the record that was written", again)
	}
}

// TestARecordIsRemovedFromTheFile checks Delete: the record is gone from the
// file, so a restart does not bring back a name an operator cleared. A Node ID
// with no record is not an error, so an operator can repeat the command.
func TestARecordIsRemovedFromTheFile(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "pending.json")
	store := joinwire.NewFileStore(path)
	for _, id := range []fractal.NodeID{"printer-1", "printer-2"} {
		if saveErr := store.Save(t.Context(), joinwire.Record{NodeID: id, Identity: "UAAAA"}); saveErr != nil {
			t.Fatalf("write %s: %v", id, saveErr)
		}
	}
	if err := store.Delete(t.Context(), "printer-1"); err != nil {
		t.Fatalf("remove printer-1: %v", err)
	}
	if err := store.Delete(t.Context(), "printer-9"); err != nil {
		t.Errorf("removing a node id with no record returned %v, want nil", err)
	}
	again, err := joinwire.NewFileStore(path).Load(t.Context())
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(again) != 1 || again[0].NodeID != "printer-2" {
		t.Errorf("the file holds %v, want printer-2 alone", again)
	}
}

// TestAStoreFileThatCannotBeReadIsRefused checks that Load returns
// ErrCorruptStore for each kind of invalid file. It must never return an empty
// list for one, because AskFirst would then lose the approvals an operator
// gave.
func TestAStoreFileThatCannotBeReadIsRefused(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		content string
	}{
		{"not json", "{"},
		{"another layout version", `{"format":99,"records":[]}`},
		{"a node id outside the grammar", `{"format":1,"records":[{"nodeId":"-x"}]}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "pending.json")
			if err := os.WriteFile(path, []byte(c.content), 0o600); err != nil {
				t.Fatalf("write the file: %v", err)
			}
			if _, err := joinwire.NewFileStore(path).Load(t.Context()); !errors.Is(err, joinwire.ErrCorruptStore) {
				t.Errorf("the refusal is %v, want a file that cannot be read", err)
			}
		})
	}
}

// TestAStoreThatRefusesToWriteLeavesMemoryAlone checks that after a failed
// Save, AskFirst has no record in memory that the store does not have.
func TestAStoreThatRefusesToWriteLeavesMemoryAlone(t *testing.T) {
	t.Parallel()
	store := &joinwire.MemoryStore{SaveErr: errors.New("the disk is full")}
	ask := &joinwire.AskFirst{Store: store}
	if err := ask.Restore(t.Context()); err != nil {
		t.Fatalf("restore: %v", err)
	}
	err := ask.Authorize(t.Context(), askRequest(), "hq")
	if err == nil {
		t.Fatal("a request was authorized although nothing could be written down")
	}
	if errors.Is(err, joinwire.ErrAwaitingApproval) {
		t.Errorf("the refusal is %v, want the one from the store", err)
	}
	if got := len(ask.Records()); got != 0 {
		t.Errorf("the mode holds %d identities the store does not", got)
	}
}

// TestAModeWithNoStoreIsRefused checks that NewAskFirst returns an error for a
// nil Store and for a Store whose Load fails.
func TestAModeWithNoStoreIsRefused(t *testing.T) {
	t.Parallel()
	if _, err := joinwire.NewAskFirst(t.Context(), nil, nil); err == nil {
		t.Error("a mode with nowhere to write was built")
	}
	store := &joinwire.MemoryStore{LoadErr: errors.New("the file is gone")}
	if _, err := joinwire.NewAskFirst(t.Context(), nil, store); err == nil {
		t.Error("a mode whose store cannot be read was built")
	}
}

// TestSigningRefusesWhatItCannotSign checks that Sign returns the error of
// Signer.PublicKey and the error of Signer.Sign.
func TestSigningRefusesWhatItCannotSign(t *testing.T) {
	t.Parallel()
	failing := errors.New("the key is gone")
	if _, err := joinwire.Sign(joinwire.Request{}, brokenSigner{keyErr: failing}); !errors.Is(err, failing) {
		t.Errorf("the refusal is %v, want the one from the key", err)
	}
	if _, err := joinwire.Sign(joinwire.Request{}, brokenSigner{signErr: failing}); !errors.Is(err, failing) {
		t.Errorf("the refusal is %v, want the one from the signature", err)
	}
}

// brokenSigner is a Signer that returns the errors a test sets.
type brokenSigner struct {
	keyErr  error
	signErr error
}

// PublicKey returns a public key, or the error the test set.
func (s brokenSigner) PublicKey() (string, error) {
	if s.keyErr != nil {
		return "", s.keyErr
	}
	return "UAAAA", nil
}

// Sign returns a signature, or the error the test set.
func (s brokenSigner) Sign([]byte) ([]byte, error) {
	if s.signErr != nil {
		return nil, s.signErr
	}
	return []byte("signature"), nil
}

// TestAnAnswerThatDoesNotParseIsRefused checks that DecodeReply returns an
// error for an answer that is not JSON.
func TestAnAnswerThatDoesNotParseIsRefused(t *testing.T) {
	t.Parallel()
	if _, err := joinwire.DecodeReply([]byte("{")); err == nil {
		t.Error("an answer that is not json was read")
	}
}

// TestAKeySourceWithNoUrlIsRefused checks that a JWKS with an empty URL
// returns an error from Key.
func TestAKeySourceWithNoUrlIsRefused(t *testing.T) {
	t.Parallel()
	source := joinwire.NewJWKS("")
	if _, err := source.Key(t.Context(), "one"); err == nil {
		t.Error("a key source with no address answered")
	}
}
