package storageapi_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/storageapi"
	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

// keeper is a host that keeps what a write sends, and publishes it only when
// the body ended cleanly, the way a storage provider does.
type keeper struct {
	filesFake

	mu        sync.Mutex
	published map[string][]byte
	saw       storageapi.WriteParams
	refuse    error
}

func (k *keeper) Write(
	_ context.Context, params storageapi.WriteParams, body io.Reader,
) (storageapi.Written, error) {
	k.mu.Lock()
	k.saw = params
	refuse := k.refuse
	k.mu.Unlock()

	if refuse != nil {
		return storageapi.Written{}, refuse
	}

	held, err := io.ReadAll(body)
	if err != nil {
		return storageapi.Written{}, err
	}

	k.mu.Lock()
	defer k.mu.Unlock()

	if k.published == nil {
		k.published = map[string][]byte{}
	}

	k.published[params.Path] = held
	digest := sha256.Sum256(held)

	return storageapi.Written{Path: params.Path, Size: int64(len(held)), SHA256: hex.EncodeToString(digest[:])}, nil
}

func (k *keeper) file(path string) ([]byte, bool) {
	k.mu.Lock()
	defer k.mu.Unlock()

	held, ok := k.published[path]

	return held, ok
}

// A file several windows long crosses the session whole, and the answer
// carries the digest of what the host kept.
func TestAWriteCarriesTheWholeFile(t *testing.T) {
	t.Parallel()

	host := &keeper{}
	files := storageapi.NewClient(serveFile(t, host))

	body := bytes.Repeat([]byte("G1 X10 Y10 E0.5\n"), 300_000)

	written, err := files.Write(t.Context(), storageapi.WriteParams{
		Storage: "shelf", Path: "out/part.gcode", Size: int64(len(body)),
	}, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("write: %v", err)
	}

	kept, ok := host.file("out/part.gcode")
	if !ok || !bytes.Equal(kept, body) {
		t.Fatalf("the host kept %d bytes, want the %d that were sent", len(kept), len(body))
	}

	digest := sha256.Sum256(body)
	if written.SHA256 != hex.EncodeToString(digest[:]) || written.Size != int64(len(body)) {
		t.Fatalf("the answer was %+v", written)
	}
}

// A body shorter than the size the call named publishes nothing, and the
// module reads ErrShortWrite. It is short either inside the first piece, before
// anything is sent, or after it, once the bytes are on their way.
func TestAShortBodyPublishesNothing(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		body string
		size int64
	}{
		"inside the first piece": {body: "only a few bytes", size: 100},
		"after the first piece":  {body: strings.Repeat("y", 100<<10), size: 300 << 10},
	}

	for name, held := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			host := &keeper{}
			files := storageapi.NewClient(serveFile(t, host))

			_, err := files.Write(t.Context(), storageapi.WriteParams{
				Storage: "shelf", Path: "short.gcode", Size: held.size,
			}, strings.NewReader(held.body))
			if !errors.Is(err, storageapi.ErrShortWrite) {
				t.Fatalf("a body short of its size answered %v, want %v", err, storageapi.ErrShortWrite)
			}

			if _, ok := host.file("short.gcode"); ok {
				t.Fatal("the host published a file whose body was short")
			}

			// The session is still usable.
			if _, err = files.Write(t.Context(), storageapi.WriteParams{
				Storage: "shelf", Path: "after.gcode", Size: 2,
			}, strings.NewReader("ok")); err != nil {
				t.Fatalf("the write after a short one failed: %v", err)
			}
		})
	}
}

// A refusal at the host reaches the module as its sentinel, and the session
// stays usable for the next write.
func TestARefusedWriteLeavesTheSessionUsable(t *testing.T) {
	t.Parallel()

	host := &keeper{refuse: storageapi.ErrDenied}
	files := storageapi.NewClient(serveFile(t, host))

	body := bytes.Repeat([]byte("x"), 2<<20)

	_, err := files.Write(t.Context(), storageapi.WriteParams{
		Storage: "shelf", Path: "denied.gcode", Size: int64(len(body)),
	}, bytes.NewReader(body))
	if !errors.Is(err, storageapi.ErrDenied) {
		t.Fatalf("the refusal read %v, want %v", err, storageapi.ErrDenied)
	}

	host.mu.Lock()
	host.refuse = nil
	host.mu.Unlock()

	if _, err = files.Write(t.Context(), storageapi.WriteParams{
		Storage: "shelf", Path: "next.gcode", Size: 4,
	}, strings.NewReader("next")); err != nil {
		t.Fatalf("the write after a refusal failed: %v", err)
	}
}

// A size over the published bound is refused before a stream is opened, and a
// write made as a plain call is refused at the host.
func TestAWriteOutsideTheBoundsIsRefused(t *testing.T) {
	t.Parallel()

	over := storageapi.WriteParams{Storage: "shelf", Path: "big.gcode", Size: storageapi.MaxWriteBytes + 1}
	if err := over.Validate(); !errors.Is(err, storageapi.ErrInvalidRange) {
		t.Fatalf("the size was held to be %v, want %v", err, storageapi.ErrInvalidRange)
	}

	_, err := answer(t, &keeper{}, storageapi.MethodWrite,
		storageapi.WriteParams{Storage: "shelf", Path: "plain.gcode", Size: 1})

	var coded *wirerpc.Error
	if !errors.As(err, &coded) || coded.Code != storageapi.CodeShortWrite {
		t.Fatalf("a write with no stream answered %v, want code %d", err, storageapi.CodeShortWrite)
	}
}

// Many small writes, side by side on one session. A file of a few bytes is
// sent at once, so its end of stream reaches the host about when the call
// does, and a host that took the end first would wait for ever.
func TestManySmallWritesAllEnd(t *testing.T) {
	t.Parallel()

	host := &keeper{}
	files := storageapi.NewClient(serveFile(t, host))

	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()

	var group sync.WaitGroup

	for index := range 200 {
		group.Go(func() {
			name := "small/" + strconv.Itoa(index) + ".gcode"

			if _, err := files.Write(ctx, storageapi.WriteParams{Storage: "shelf", Path: name, Size: 10},
				strings.NewReader("GCDE123456")); err != nil {
				t.Errorf("write %s: %v", name, err)
			}
		})
	}

	group.Wait()
}
