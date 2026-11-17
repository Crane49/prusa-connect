package natsbind

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
	"github.com/prusa3d/prusa-connect/sdk/fractal/join"
)

// refusalFile holds the permanent refusal of this node, so that a new start of
// the module does not ask the node above again. An operator deletes it to let
// the node ask again.
const refusalFile = "refusal.json"

// refusal is what refusalFile holds: the Status.Uplink fields an operator
// reads, and the digest of the join credential the node above refused with
// ERR_JOIN_UNAUTHORIZED.
type refusal struct {
	Code       fractal.Code `json:"code,omitempty"`
	Reason     string       `json:"reason"`
	Refused    bool         `json:"refused,omitempty"`
	Credential string       `json:"credential,omitempty"`
}

// holdRefusal keeps err as this node's permanent refusal, in memory and in
// refusalFile. Every later join request is then answered by err without a
// request to the node above. The first refusal stays.
func (b *Binding) holdRefusal(ctx context.Context, err error, held refusal) {
	b.mu.Lock()
	if b.permanent != nil {
		b.mu.Unlock()
		return
	}
	b.permanent = err
	b.mu.Unlock()
	if held.Code.Is(fractal.CodeJoinUnauthorized) {
		held.Credential = digest(b.cfg.JoinCredential)
	}
	data, encodeErr := json.Marshal(held)
	if encodeErr == nil {
		encodeErr = writeRefusal(b.cfg.DataDir, data)
	}
	if encodeErr != nil {
		b.log.ErrorContext(ctx, "save the permanent refusal; a new start asks the node above again",
			"error", encodeErr)
	}
}

// loadRefusal reads refusalFile into the Binding and its Status. A file that
// cannot be read or decoded, as a power loss can leave it, still refuses: the
// node logs the error and asks the node above nothing. A refusal
// with ERR_JOIN_UNAUTHORIZED of another join credential than the configured
// one is deleted, because the node asks again once it has a new join
// credential.
func (b *Binding) loadRefusal(ctx context.Context) error {
	path := filepath.Join(b.cfg.DataDir, refusalFile)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	var held refusal
	if err == nil {
		err = json.Unmarshal(data, &held)
	}
	if err != nil {
		b.log.ErrorContext(ctx, unreadableRefusalLog, "path", path, "error", err)
		b.permanent = fmt.Errorf("the saved refusal cannot be read: %w", err)
		b.uplink.State, b.uplink.Reason, b.uplink.Unreadable = UplinkDenied, b.permanent.Error(), true
		return nil
	}
	if held.Code.Is(fractal.CodeJoinUnauthorized) && held.Credential != digest(b.cfg.JoinCredential) {
		b.log.InfoContext(ctx, "this node holds a new join credential and asks the node above again")
		if removeErr := os.Remove(path); removeErr != nil {
			return fmt.Errorf("delete %s: %w", path, removeErr)
		}
		return nil
	}
	b.permanent = errors.New(held.Reason)
	if held.Code != 0 {
		b.permanent = &join.DenialError{Denial: fractal.JoinDenial{
			Version: fractal.Version, Code: held.Code, Reason: held.Reason,
		}}
	}
	b.uplink.State, b.uplink.Code, b.uplink.Reason, b.uplink.Refused = UplinkDenied, held.Code, held.Reason, held.Refused
	return nil
}

// unreadableRefusalLog is the error a node logs when it cannot read
// refusalFile.
const unreadableRefusalLog = "the saved refusal cannot be read; this node stays refused until an operator deletes it"

// writeRefusal writes data to a temporary file in dir, syncs it, renames it to
// refusalFile and syncs dir. After a power loss refusalFile then holds the
// whole refusal or its old content.
func writeRefusal(dir string, data []byte) error {
	tmp, err := os.CreateTemp(dir, ".refusal-*.tmp")
	if err != nil {
		return fmt.Errorf("open a temporary file in %s: %w", dir, err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }() // fails once the rename moved it, which is the aim
	_, err = tmp.Write(data)
	if err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), filepath.Join(dir, refusalFile))
	}
	if err != nil {
		return fmt.Errorf("write %s: %w", refusalFile, err)
	}
	return syncDir(dir)
}

// syncDir makes a rename in dir survive a power loss. Windows cannot open a
// directory to sync it, and its rename needs no such step.
func syncDir(dir string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	d, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("open %s: %w", dir, err)
	}
	err = d.Sync()
	if closeErr := d.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("sync %s: %w", dir, err)
	}
	return nil
}

// permanentRefusal returns this node's permanent refusal, or nil.
func (b *Binding) permanentRefusal() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.permanent
}

// digest returns the SHA-256 digest of a join credential as hex, so
// refusalFile does not hold the credential.
func digest(c fractal.JoinCredential) string {
	sum := sha256.Sum256([]byte(c))
	return hex.EncodeToString(sum[:])
}

// refusedLog is the message a node logs when it asks the node above nothing
// more.
const refusedLog = "this node asks the node above nothing more"
