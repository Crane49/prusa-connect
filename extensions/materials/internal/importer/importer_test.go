package importer

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/prusa3d/materials/internal/dbtest"
	"github.com/prusa3d/materials/internal/stock"
)

func newImporter(t *testing.T) (*Importer, *stock.Store) {
	t.Helper()

	db, grant := dbtest.SQLite(t)
	store := stock.New(db, grant.Engine)

	return New(store, time.Now), store
}

func wait(t *testing.T, i *Importer) Status {
	t.Helper()

	for range 500 {
		if st := i.Status(); st.Phase != Running {
			return st
		}

		time.Sleep(10 * time.Millisecond)
	}

	t.Fatal("import never finished")

	return Status{}
}

func catalogueZip(t *testing.T) Fetch {
	t.Helper()

	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	f, _ := w.Create("r-main/data/brands/b.yaml")
	_, _ = f.Write([]byte("uuid: 863c5f0c-04a5-50c9-b5a2-5a8e81d5821f\nslug: b\nname: B\n"))
	mat, _ := w.Create("r-main/data/materials/b/m.yaml")
	_, _ = mat.Write([]byte("uuid: 1b4e28ba-2fa1-11d2-883f-0016d3cca427\nslug: m\nname: M\nbrand:\n  slug: b\n"))
	m, _ := w.Create("r-main/data/manifest.yaml")
	_, _ = m.Write([]byte("data_hash: h1\n"))
	_ = w.Close()

	return func(context.Context) (io.ReaderAt, int64, error) {
		return bytes.NewReader(buf.Bytes()), int64(buf.Len()), nil
	}
}

func TestTheImporterRunsOneAtATime(t *testing.T) {
	i, _ := newImporter(t)
	release := make(chan struct{})

	err := i.Start(func(context.Context) (io.ReaderAt, int64, error) {
		<-release

		return nil, 0, errors.New("stop")
	})
	if err != nil {
		t.Fatal(err)
	}

	if err = i.Start(nil); !errors.Is(err, ErrRunning) {
		t.Fatalf("got %v", err)
	}

	close(release)
	wait(t, i)
}

func TestClosingTheImporterCancelsTheRunAndWaitsForIt(t *testing.T) {
	i, _ := newImporter(t)
	started := make(chan struct{})

	err := i.Start(func(ctx context.Context) (io.ReaderAt, int64, error) {
		close(started)
		<-ctx.Done()

		return nil, 0, ctx.Err()
	})
	if err != nil {
		t.Fatal(err)
	}

	<-started
	i.Close()

	if st := i.Status(); st.Phase != Failed || st.Reason != ReasonDownload {
		t.Fatalf("status %+v", st)
	}
}

func TestAClosedImporterStartsNothing(t *testing.T) {
	i, _ := newImporter(t)
	i.Close()

	if err := i.Start(catalogueZip(t)); !errors.Is(err, ErrClosed) {
		t.Fatalf("got %v", err)
	}

	if st := i.Status(); st.Phase != Idle {
		t.Fatalf("status %+v", st)
	}
}

func TestTheImporterLoadsTheCatalogue(t *testing.T) {
	i, store := newImporter(t)

	if err := i.Start(catalogueZip(t)); err != nil {
		t.Fatal(err)
	}

	st := wait(t, i)
	meta, ok, err := store.Catalogue(context.Background())

	if st.Phase != Idle || st.Last == nil || !ok || err != nil || meta.Brands != 1 {
		t.Fatalf("status %+v meta %+v ok=%v err=%v", st, meta, ok, err)
	}
}

func TestTheImporterReportsAFailure(t *testing.T) {
	i, store := newImporter(t)

	if err := i.Start(catalogueZip(t)); err != nil {
		t.Fatal(err)
	}

	wait(t, i)

	if err := i.Start(
		func(context.Context) (io.ReaderAt, int64, error) {
			return nil, 0, errors.New("dial /home/op/secret: offline")
		},
	); err != nil {
		t.Fatal(err)
	}

	st := wait(t, i)
	if st.Phase != Failed || st.Reason != ReasonDownload {
		t.Fatalf("status %+v", st)
	}

	if meta, ok, _ := store.Catalogue(context.Background()); !ok || meta.Brands != 1 {
		t.Fatalf("old catalogue lost: %+v", meta)
	}
}

func TestDownloadRefusesABodyOverTheCap(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(bytes.Repeat([]byte("x"), 20))
	}))
	defer srv.Close()

	tmp := t.TempDir()

	if _, _, err := Download(srv.Client(), srv.URL, 10, tmp)(context.Background()); err == nil {
		t.Fatal("want an error")
	}

	if left, _ := os.ReadDir(tmp); len(left) != 0 {
		t.Fatalf("a refused body left %v", left)
	}

	r, n, err := Download(srv.Client(), srv.URL, 20, tmp)(context.Background())
	if err != nil || n != 20 {
		t.Fatalf("n=%d err=%v", n, err)
	}

	if err = r.(io.Closer).Close(); err != nil {
		t.Fatal(err)
	}

	if left, _ := os.ReadDir(tmp); len(left) != 0 {
		t.Fatalf("a closed download left %v", left)
	}
}

func TestDownloadRefusesPlainHTTP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer srv.Close()

	if _, _, err := Download(srv.Client(), srv.URL, 10, t.TempDir())(context.Background()); err == nil {
		t.Fatal("want an error")
	}
}

func TestDownloadRefusesARedirectToPlainHTTP(t *testing.T) {
	plain := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer plain.Close()

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, plain.URL, http.StatusFound)
	}))
	defer srv.Close()

	if _, _, err := Download(srv.Client(), srv.URL, 10, t.TempDir())(context.Background()); err == nil {
		t.Fatal("want an error")
	}
}

func TestSweepRemovesOnlyTheDownloadsLeftBehind(t *testing.T) {
	dir := t.TempDir()

	for _, name := range []string{"materials-catalogue-1.zip", "materials-catalogue-2.zip", "materials.db"} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	Sweep(dir)

	left, err := os.ReadDir(dir)
	if err != nil || len(left) != 1 || left[0].Name() != "materials.db" {
		t.Fatalf("left %v, %v", left, err)
	}
}
