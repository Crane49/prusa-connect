package main

import (
	"context"
	"fmt"
	"log/slog"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
	"github.com/prusa3d/prusa-connect/sdk/storageapi"
)

// oneStorage is a host with one storage, local, that holds one empty folder,
// lib. Every other storage is unknown to it.
type oneStorage struct{}

func (oneStorage) List(_ context.Context, p storageapi.ListParams) (storageapi.Listing, error) {
	switch {
	case p.Storage != "local":
		return storageapi.Listing{}, storageapi.ErrNoFile
	case p.Path == "":
		return storageapi.Listing{Entries: []storageapi.ListEntry{{Name: "lib", Dir: true}}}, nil
	default:
		return storageapi.Listing{}, nil
	}
}

func (oneStorage) Stat(context.Context, storageapi.StatParams) (storageapi.FileInfo, error) {
	return storageapi.FileInfo{}, storageapi.ErrNoFile
}

func (oneStorage) Read(context.Context, storageapi.ReadParams) (storageapi.Chunk, error) {
	return storageapi.Chunk{}, storageapi.ErrNoFile
}

// owned is one interface instance the node owns, as a comparable value.
type owned struct {
	Name     fractal.InterfaceName
	Instance fractal.InstanceID
}

// TestSharedStorageIsContentStore: a server with a files block registers a
// ContentStore1 instance for a storage once a folder of it is shared.
func TestSharedStorageIsContentStore(t *testing.T) {
	t.Parallel()

	held, err := ParseConfig(fmt.Appendf(nil,
		`{"node_id":"hq","data_dir":%q,"dedup_window":"1m","files":{"base_url":"https://hq.example:8443/f"}}`,
		t.TempDir()))
	if err != nil {
		t.Fatalf("read the settings: %v", err)
	}

	held.Grants.Data = t.TempDir()
	below := &tree{}

	built, err := build(t.Context(), held, newLink(), below,
		session{fleet: noMachines{}, storage: oneStorage{}}, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("build the node: %v", err)
	}
	t.Cleanup(func() { _ = built.binding.Close() })

	if err = below.shares.Share("local", "lib"); err != nil {
		t.Fatalf("share: %v", err)
	}

	ctx, cancel := context.WithCancel(t.Context())

	var group sync.WaitGroup

	group.Go(func() { _ = built.stores.Run(ctx) })
	group.Go(func() { _ = built.publisher.Run(ctx, time.Hour) })

	t.Cleanup(func() {
		cancel()
		group.Wait()
	})

	want := []owned{{Name: "ContentStore1", Instance: "local"}}

	deadline := time.Now().Add(crossWait)
	for time.Now().Before(deadline) {
		var got []owned
		for _, handle := range built.node.Interfaces() {
			got = append(got, owned{Name: handle.Name(), Instance: handle.Instance()})
		}

		if reflect.DeepEqual(got, want) {
			return
		}

		time.Sleep(crossPoll)
	}

	t.Fatalf("the node owns %v, want %v", built.node.Interfaces(), want)
}
