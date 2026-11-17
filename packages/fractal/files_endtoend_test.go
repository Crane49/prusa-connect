package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"math/big"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prusa3d/fractal-link/bridge"
	pfiles "github.com/prusa3d/fractal-link/files"
	"github.com/prusa3d/fractal-link/internal/testport"
	"github.com/prusa3d/fractal-link/natsbind"

	"github.com/prusa3d/prusa-connect/sdk/deviceapi"
	"github.com/prusa3d/prusa-connect/sdk/fractal"
	sdkfiles "github.com/prusa3d/prusa-connect/sdk/fractal/files"
	"github.com/prusa3d/prusa-connect/sdk/fractal/join"
	fractalnode "github.com/prusa3d/prusa-connect/sdk/fractal/node"
	"github.com/prusa3d/prusa-connect/sdk/storageapi"
	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

// This file runs three nodes of this module with TLS. hq shares a folder of
// its storage "library", brno joins hq, and workshop joins brno and has a
// printer. A print on workshop fetches the file through brno.

// tlsFiles are the certificate and key every node serves, which is also the
// trust anchor of every node.
type tlsFiles struct{ cert, key string }

func writeTLS(t *testing.T) tlsFiles {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("create a key: %v", err)
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "fractal files test"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true,
		KeyUsage:    x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
	}

	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create a certificate: %v", err)
	}

	raw, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("encode the key: %v", err)
	}

	dir := t.TempDir()
	out := tlsFiles{cert: filepath.Join(dir, "node.pem"), key: filepath.Join(dir, "node.key")}

	for at, block := range map[string]*pem.Block{
		out.cert: {Type: "CERTIFICATE", Bytes: der},
		out.key:  {Type: "EC PRIVATE KEY", Bytes: raw},
	} {
		if err = os.WriteFile(at, pem.EncodeToMemory(block), 0o600); err != nil {
			t.Fatalf("write %s: %v", at, err)
		}
	}

	return out
}

// library is a fake storage plane with one storage, "library" unless name
// says another. It counts the reads, so a test sees whether a print reached
// it.
type library struct {
	mu    sync.Mutex
	name  string
	files map[string][]byte
	reads int
}

// storage is the name of the one storage of l.
func (l *library) storage() string {
	if l.name == "" {
		return "library"
	}

	return l.name
}

func (l *library) List(_ context.Context, p storageapi.ListParams) (storageapi.Listing, error) {
	if p.Storage != l.storage() {
		return storageapi.Listing{}, storageapi.ErrNoFile
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	prefix := p.Path
	if prefix != "" {
		prefix += "/"
	}

	entries := map[string]storageapi.ListEntry{}

	for at, data := range l.files {
		rest, ok := strings.CutPrefix(at, prefix)
		if !ok {
			continue
		}

		if name, _, nested := strings.Cut(rest, "/"); nested {
			entries[name] = storageapi.ListEntry{Name: name, Dir: true, MTime: 1_800_000_000}
		} else {
			entries[name] = storageapi.ListEntry{Name: name, Size: int64(len(data)), MTime: 1_800_000_000}
		}
	}

	out := storageapi.Listing{Entries: []storageapi.ListEntry{}}
	for _, name := range slices.Sorted(maps.Keys(entries)) {
		out.Entries = append(out.Entries, entries[name])
	}

	return out, nil
}

func (l *library) Stat(_ context.Context, p storageapi.StatParams) (storageapi.FileInfo, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	data, ok := l.files[p.Path]
	if !ok || p.Storage != l.storage() {
		return storageapi.FileInfo{}, storageapi.ErrNoFile
	}

	return storageapi.FileInfo{Name: path.Base(p.Path), Path: p.Path, Size: int64(len(data)), MTime: 1_800_000_000}, nil
}

func (l *library) Read(_ context.Context, p storageapi.ReadParams) (storageapi.Chunk, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.reads++

	data, ok := l.files[p.Path]
	if !ok || p.Storage != l.storage() {
		return storageapi.Chunk{}, storageapi.ErrNoFile
	}

	length := int64(p.Length)
	if length == 0 {
		length = storageapi.DefaultReadBytes
	}

	size := int64(len(data))
	start := min(p.Offset, size)
	end := min(start+length, size)

	return storageapi.Chunk{Data: data[start:end], Size: size, EOF: end == size}, nil
}

func (l *library) readCount() int {
	l.mu.Lock()
	defer l.mu.Unlock()

	return l.reads
}

// printedFile is one print the workshop printer received.
type printedFile struct {
	Storage string
	Path    string
	Bytes   string
}

// printingMachine is the workshop printer: READY, and it reads the copy the
// module names from module:data, as the core would. It keeps every file it was
// sent. Like the core, it starts a file it was sent from the same source with
// no transfer, and refuses to send one onto a name that holds anything else.
type printingMachine struct {
	*oneMachine

	dataDir string
	mu      sync.Mutex
	prints  []printedFile
	held    map[string]heldFile
}

// heldFile is a file on the printer and the source it was sent from.
type heldFile struct {
	source string
	data   []byte
}

func (m *printingMachine) Print(_ context.Context, p deviceapi.PrintParams) (deviceapi.Started, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if held, taken := m.held[p.Path]; taken {
		if held.source != p.Source {
			return deviceapi.Started{}, fmt.Errorf("device.print: %w", deviceapi.ErrNameTaken)
		}

		m.prints = append(m.prints, printedFile{Path: p.Path, Bytes: string(held.data)})

		return deviceapi.Started{CommandID: "command-" + strconv.Itoa(len(m.prints))}, nil
	}

	data, err := os.ReadFile(filepath.Join(m.dataDir, cacheFolder, filepath.FromSlash(p.Source)))
	if err != nil {
		data = []byte("unreadable: " + err.Error())
	}

	if m.held == nil {
		m.held = map[string]heldFile{}
	}

	m.held[p.Path] = heldFile{source: p.Source, data: data}
	m.prints = append(m.prints, printedFile{Storage: p.Storage, Path: p.Path, Bytes: string(data)})

	return deviceapi.Started{TransferID: "transfer-" + strconv.Itoa(len(m.prints))}, nil
}

func (m *printingMachine) printed() []printedFile {
	m.mu.Lock()
	defer m.mu.Unlock()

	return append([]printedFile(nil), m.prints...)
}

// nodePorts are the ports of one node.
type nodePorts struct{ client, leaf, files int }

func portsOf(t *testing.T) nodePorts {
	t.Helper()

	return nodePorts{client: testport.Free(t), leaf: testport.Free(t), files: testport.Free(t)}
}

func filesSettings(t *testing.T, name string, ports nodePorts, above int, material tlsFiles) string {
	t.Helper()

	join := ""
	if above != 0 {
		join = fmt.Sprintf(`"join": {"address": "127.0.0.1:%d"},`, above)
	}

	return fmt.Sprintf(`{
		"node_id": %q,
		"data_dir": %q,
		"dedup_window": "1m",
		"heartbeat_interval": "100ms",
		"max_heartbeat_interval": "200ms",
		"poll_interval": "50ms",
		"flush_interval": "50ms",
		%s
		"accept": {
			"advertise": "tls://127.0.0.1:%d",
			"client_listen": "127.0.0.1:%d",
			"leaf_listen": "127.0.0.1:%d"
		},
		"tls": {"cert_file": %q, "key_file": %q, "ca_files": [%q]},
		"files": {"base_url": "https://127.0.0.1:%d", "rescan": "1h", "relay_timeout": "5s"}
	}`, name, t.TempDir(), join, ports.leaf, ports.client, ports.leaf,
		material.cert, material.key, material.cert, ports.files)
}

// startedWith builds one node with the grants of the host and runs it until
// ctx ends. It returns the node and the routes of its module.
func startedWith(
	ctx context.Context, t *testing.T, record string, host session, port int, dataDir string,
) (*parts, http.Handler) {
	t.Helper()

	cfg, err := ParseConfig([]byte(record))
	if err != nil {
		t.Fatalf("read the settings: %v", err)
	}

	cfg.Grants = wirerpc.Grants{
		Listen: map[string]wirerpc.ListenGrant{filesPort: {Network: "tcp", Port: port}},
		Data:   dataDir,
	}

	if err = cfg.checkGrants(); err != nil {
		t.Fatalf("check the grants: %v", err)
	}

	state := &tree{}

	built, err := build(ctx, cfg, newLink(), state, host, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("build the node: %v", err)
	}

	var group sync.WaitGroup

	built.start(ctx, &group, make(chan error, runners))

	// The file server closes itself and the idle connections of its client
	// when ctx ends, so waiting for the group closes both.
	t.Cleanup(func() {
		group.Wait()
		_ = built.node.Close()
		_ = built.binding.Close()
	})

	return built, routes(state)
}

// awaitTrue waits until cond holds, at most crossWait.
func awaitTrue(t *testing.T, what string, cond func() bool) {
	t.Helper()

	deadline := time.Now().Add(crossWait)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}

		time.Sleep(crossPoll)
	}

	t.Fatalf("waited in vain for %s", what)
}

// owns reports whether node has registered the interface instance.
func owns(node *fractalnode.Node, name fractal.InterfaceName, instance fractal.InstanceID) bool {
	for _, handle := range node.Interfaces() {
		if handle.Name() == name && handle.Instance() == instance {
			return true
		}
	}

	return false
}

// TestThreeServersPrintASharedFile: the grant, the proof, the relay and the
// copy fit together over real servers with TLS.
func TestThreeServersPrintASharedFile(t *testing.T) {
	if testing.Short() {
		t.Skip("this test starts three NATS servers and three HTTPS servers")
	}

	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	material := writeTLS(t)
	cube := []byte("GCDE a cube for the workshop")
	other := []byte("G28 ; a file nobody fetched")
	remade := []byte("GCDE the cube sliced again")
	shared := &library{files: map[string][]byte{
		"prints/cube.bgcode": cube, "prints/other.gcode": other, "prints/remade.bgcode": remade,
	}}

	hqPorts, brnoPorts, workshopPorts := portsOf(t), portsOf(t), portsOf(t)

	// A node starts its NATS server only after its own join, and a join that
	// finds no server waits up to twice as long each time. So each node starts
	// after the node above listens, or the waits of brno and workshop add up.
	hq, hqRoutes := startedWith(ctx, t, filesSettings(t, "hq", hqPorts, 0, material),
		session{fleet: noMachines{}, storage: shared}, hqPorts.files, t.TempDir())
	awaitTrue(t, "hq to listen", func() bool { return len(hq.binding.Status().Listeners) > 0 })

	brno, _ := startedWith(ctx, t, filesSettings(t, "brno", brnoPorts, hqPorts.client, material),
		session{fleet: noMachines{}, storage: &library{}}, brnoPorts.files, t.TempDir())
	awaitTrue(t, "brno to connect to hq", func() bool {
		return brno.binding.Status().Uplink.State == natsbind.UplinkConnected
	})

	workshopData := t.TempDir()
	device := newMachine()
	device.device.State["/state"] = json.RawMessage(`"READY"`)
	printerOnWorkshop := &printingMachine{oneMachine: device, dataDir: workshopData}
	workshop, _ := startedWith(ctx, t, filesSettings(t, "workshop", workshopPorts, brnoPorts.client, material),
		session{fleet: printerOnWorkshop, storage: &library{}}, workshopPorts.files, workshopData)

	awaitTrue(t, "workshop to join brno", func() bool { return workshop.binding.Path() == "hq/brno/workshop" })
	awaitTrue(t, "workshop to register its printer", func() bool { return owns(workshop.node, "Printer1", machine) })

	if got := answerOf(t, hqRoutes, http.MethodPut, "/files/shares/library/prints"); got != (answered{
		status: http.StatusNoContent,
	}) {
		t.Fatalf("sharing the folder answered %+v", got)
	}

	cubeID, otherID, remadeID := sdkfiles.Sum(cube), sdkfiles.Sum(other), sdkfiles.Sum(remade)
	awaitTrue(t, "hq to publish the shared folder", func() bool {
		_, hasCube := hq.publisher.Lookup(t.Context(), cubeID)
		_, hasOther := hq.publisher.Lookup(t.Context(), otherID)
		_, hasRemade := hq.publisher.Lookup(t.Context(), remadeID)

		return hasCube && hasOther && hasRemade
	})

	printFile := func(id sdkfiles.Identity, name string) {
		t.Helper()

		answer, err := hq.node.Call(ctx, fractalnode.CallRequest{
			Target: "hq/brno/workshop", Interface: "Printer1", Instance: fractal.InstanceID(machine),
			Method:  "PrintFile",
			Params:  []json.RawMessage{json.RawMessage(strconv.Quote(sdkfiles.URL{ID: id, Name: name}.String()))},
			Expires: fractal.TimestampOf(time.Now().Add(crossWait)),
		})
		if err != nil {
			t.Fatalf("the PrintFile call got no result: %v", err)
		}

		if answer.Error != nil {
			t.Fatalf("the PrintFile call returned the error %s", answer.Error)
		}
	}

	// A print is counted after the printer took it, so each step waits for
	// the count.
	awaitCount := func(what, outcome string, want uint64) {
		t.Helper()

		awaitTrue(t, what, func() bool { return workshop.bridge.FetchCounts()[outcome] == want })
	}

	printFile(cubeID, "cube.bgcode")
	awaitCount("the first print", bridge.OutcomeStarted, 1)

	readsAfterFirst := shared.readCount()
	middleKeptACopy := brno.files.cache.Path(cubeID) != ""

	printFile(cubeID, "cube.bgcode")
	awaitCount("the print from the copy", bridge.OutcomeStarted, 2)

	readsAfterSecond := shared.readCount()

	printFile(remadeID, "cube.bgcode")
	awaitCount("the print of another file of the same name", bridge.OutcomeStarted, 3)

	if got := answerOf(t, hqRoutes, http.MethodDelete, "/files/shares/library/prints"); got != (answered{
		status: http.StatusNoContent,
	}) {
		t.Fatalf("unsharing the folder answered %+v", got)
	}

	awaitTrue(t, "hq to stop publishing the folder", func() bool {
		_, published := hq.publisher.Lookup(t.Context(), otherID)

		return !published
	})

	printFile(otherID, "other.gcode")
	awaitCount("the print of a file nobody fetched to end", bridge.OutcomeNotFound, 1)

	printFile(cubeID, "cube.bgcode")
	awaitCount("the print of the kept copy", bridge.OutcomeStarted, 4)

	// workshop fetches from the files address in the grant of brno, and brno
	// neither publishes the cube nor keeps a copy, so the bytes hq read went
	// through the relay of brno.
	workshopAsked := ""
	if raw, ok := workshop.binding.ParentExtension(sdkfiles.ExtensionName); ok {
		if entry, err := sdkfiles.ParseJoinEntry(raw, false); err == nil {
			workshopAsked = entry.HTTP.BaseURL
		}
	}

	_, middlePublishes := brno.publisher.Lookup(t.Context(), cubeID)

	type result struct {
		Prints            []printedFile
		Counts            map[string]uint64
		SecondReadTheCopy bool
		WorkshopAsked     string
		MiddlePublishes   bool
		MiddleKeptACopy   bool
		HQRead            bool
	}

	// The first 12 characters of the digest tell two files of one name apart.
	digestName := func(id sdkfiles.Identity) string {
		return "usb/cube-" + strings.TrimPrefix(id.Text(), "sha256:")[:12] + ".bgcode"
	}
	cubePrint := printedFile{Storage: deviceapi.ModuleDataStorage, Path: "usb/cube.bgcode", Bytes: string(cube)}
	cubeHeld := printedFile{Path: "usb/cube.bgcode", Bytes: string(cube)}
	remadePrint := printedFile{Storage: deviceapi.ModuleDataStorage, Path: digestName(remadeID), Bytes: string(remade)}
	got := result{
		Prints:            printerOnWorkshop.printed(),
		Counts:            workshop.bridge.FetchCounts(),
		SecondReadTheCopy: readsAfterSecond == readsAfterFirst,
		WorkshopAsked:     workshopAsked,
		MiddlePublishes:   middlePublishes,
		MiddleKeptACopy:   middleKeptACopy || brno.files.cache.Path(cubeID) != "",
		HQRead:            readsAfterFirst > 0,
	}
	want := result{
		Prints:            []printedFile{cubePrint, cubeHeld, remadePrint, cubeHeld},
		Counts:            map[string]uint64{bridge.OutcomeStarted: 4, bridge.OutcomeNotFound: 1},
		SecondReadTheCopy: true,
		WorkshopAsked:     fmt.Sprintf("https://127.0.0.1:%d", brnoPorts.files),
		HQRead:            true,
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("the three servers gave %+v, want %+v", got, want)
	}
}

// TestCleartextBaseURLServesHTTP: an http base_url serves the files in
// cleartext, though the settings hold a certificate for NATS, so the children
// the grant sends there can read them.
func TestCleartextBaseURLServesHTTP(t *testing.T) {
	if testing.Short() {
		t.Skip("this test starts a NATS server and a files server")
	}

	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	ports := portsOf(t)
	record := strings.Replace(filesSettings(t, "hq", ports, 0, writeTLS(t)),
		fmt.Sprintf(`"base_url": "https://127.0.0.1:%d"`, ports.files),
		fmt.Sprintf(`"base_url": "http://127.0.0.1:%d", "allow_cleartext_parent": true`, ports.files), 1)
	startedWith(ctx, t, record, session{fleet: noMachines{}, storage: &library{}}, ports.files, t.TempDir())

	request, err := http.NewRequestWithContext(ctx, http.MethodGet,
		fmt.Sprintf("http://127.0.0.1:%d/objects/none", ports.files), nil)
	if err != nil {
		t.Fatalf("build the request: %v", err)
	}

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("GET over http: %v", err)
	}
	_ = response.Body.Close()

	if response.StatusCode != http.StatusUnauthorized {
		t.Errorf("a GET with no proof over http answered %d, want 401 from the files server", response.StatusCode)
	}
}

// TestFilesServerClosesAnIdleConnection: a child that keeps a connection to
// the files server open and sends no next request loses it after
// files.relay_timeout.
func TestFilesServerClosesAnIdleConnection(t *testing.T) {
	if testing.Short() {
		t.Skip("this test starts a NATS server and a files server")
	}

	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	ports := portsOf(t)
	record := strings.Replace(
		filesSettings(t, "hq", ports, 0, writeTLS(t)),
		fmt.Sprintf(`"base_url": "https://127.0.0.1:%d", "rescan": "1h", "relay_timeout": "5s"`, ports.files),
		fmt.Sprintf(
			`"base_url": "http://127.0.0.1:%d", "allow_cleartext_parent": true, "rescan": "1h", "relay_timeout": "300ms"`,
			ports.files,
		),
		1,
	)
	startedWith(ctx, t, record, session{fleet: noMachines{}, storage: &library{}}, ports.files, t.TempDir())

	var dialer net.Dialer

	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(ports.files)))
	if err != nil {
		t.Fatalf("connect to the files server: %v", err)
	}
	defer conn.Close()

	if _, err = conn.Write([]byte("GET /objects/none HTTP/1.1\r\nHost: hq\r\n\r\n")); err != nil {
		t.Fatalf("send a request: %v", err)
	}

	if err = conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatalf("bound the read: %v", err)
	}

	if _, err = io.Copy(io.Discard, conn); err != nil {
		t.Errorf("the idle connection stayed open: %v", err)
	}
}

// TestARestartDropsTheCopiesOfAnotherPath: a node that starts under a Node
// Path other than the one its copies were fetched under drops them before it
// connects. The joiner saves a new path before the module can drop the
// copies, so the start is where the two meet again.
func TestARestartDropsTheCopiesOfAnotherPath(t *testing.T) {
	if testing.Short() {
		t.Skip("this test starts a NATS server and a files server")
	}

	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	cube := []byte("GCDE a cube from the former parent")
	data := t.TempDir()
	copies := filepath.Join(data, cacheFolder)

	if err := os.MkdirAll(copies, 0o750); err != nil {
		t.Fatalf("make the copies: %v", err)
	}

	sum := sdkfiles.Sum(cube)
	copyName := "sha256-" + hex.EncodeToString(sum.Bytes()[1:])

	for name, content := range map[string][]byte{".node-path": []byte("hq/former"), copyName: cube} {
		if err := os.WriteFile(filepath.Join(copies, name), content, 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	ports := portsOf(t)
	hq, _ := startedWith(ctx, t, filesSettings(t, "hq", ports, 0, writeTLS(t)),
		session{fleet: noMachines{}, storage: &library{}}, ports.files, data)

	_, statErr := os.Stat(filepath.Join(copies, copyName))
	marker, _ := os.ReadFile(filepath.Join(copies, ".node-path"))

	type result struct {
		Held     bool
		FileGone bool
		Marker   string
	}

	got := result{
		Held: hq.files.cache.Path(sum) != "", FileGone: errors.Is(statErr, os.ErrNotExist), Marker: string(marker),
	}
	if want := (result{FileGone: true, Marker: "hq"}); got != want {
		t.Errorf("the start under another path gave %+v, want %+v", got, want)
	}
}

// shownStore is one entry of GET /files/stores, with updated_at left out.
type shownStore struct {
	Node     fractal.NodePath           `json:"node"`
	Instance fractal.InstanceID         `json:"instance"`
	Status   string                     `json:"status"`
	Props    map[string]json.RawMessage `json:"props"`
}

// shownListing is the answer of GET /files/stores/{node}/{instance}/{identity}.
type shownListing struct {
	Node     fractal.NodePath   `json:"node"`
	Instance fractal.InstanceID `json:"instance"`
	Identity string             `json:"identity"`
	Entries  []shownEntry       `json:"entries"`
}

type shownEntry struct {
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	Size     uint64 `json:"size"`
	Identity string `json:"identity"`
}

// listingOf encodes one listing and returns its bytes and its identity.
func listingOf(t *testing.T, entries ...sdkfiles.Entry) ([]byte, sdkfiles.Identity) {
	t.Helper()

	data, err := sdkfiles.Listing{Entries: entries}.Encode()
	if err != nil {
		t.Fatalf("encode a listing: %v", err)
	}

	return data, sdkfiles.Sum(data)
}

// folderOf is the root listing of a store that shares one folder holding
// files, and the entries of that root as the route shows them.
func folderOf(t *testing.T, folder string, held map[string][]byte) (sdkfiles.Identity, []shownEntry) {
	t.Helper()

	var files []sdkfiles.Entry
	for _, name := range slices.Sorted(maps.Keys(held)) {
		files = append(files, sdkfiles.Entry{
			Name: name, Kind: sdkfiles.KindFile, Size: uint64(len(held[name])), Hash: sdkfiles.Sum(held[name]),
		})
	}

	inner, innerID := listingOf(t, files...)
	_, root := listingOf(t, sdkfiles.Entry{
		Name: folder, Kind: sdkfiles.KindDirectory, Size: uint64(len(inner)), Hash: innerID,
	})

	return root, []shownEntry{{Name: folder, Kind: "directory", Size: uint64(len(inner)), Identity: innerID.Text()}}
}

// TestBrowsingTheStoresOfTheLine: brno lists the content stores of hq above
// it and of workshop below it, reads a listing of each, and reads none of a
// node outside its line or of its own.
func TestBrowsingTheStoresOfTheLine(t *testing.T) {
	if testing.Short() {
		t.Skip("this test starts three NATS servers and three HTTPS servers")
	}

	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	material := writeTLS(t)
	hqFiles := map[string][]byte{"cube.bgcode": []byte("GCDE a cube")}
	workshopFiles := map[string][]byte{"a.gcode": []byte("G28 ; a job")}

	hqPorts, brnoPorts, workshopPorts := portsOf(t), portsOf(t), portsOf(t)

	hqStorage := &library{files: map[string][]byte{"prints/cube.bgcode": hqFiles["cube.bgcode"]}}
	hq, hqRoutes := startedWith(ctx, t, filesSettings(t, "hq", hqPorts, 0, material),
		session{fleet: noMachines{}, storage: hqStorage}, hqPorts.files, t.TempDir())
	awaitTrue(t, "hq to listen", func() bool { return len(hq.binding.Status().Listeners) > 0 })

	brno, brnoRoutes := startedWith(ctx, t, filesSettings(t, "brno", brnoPorts, hqPorts.client, material),
		session{fleet: noMachines{}, storage: &library{}}, brnoPorts.files, t.TempDir())
	awaitTrue(t, "brno to connect to hq", func() bool {
		return brno.binding.Status().Uplink.State == natsbind.UplinkConnected
	})

	// A storage name is a DNS label, and its instance writes the hyphen as an
	// underscore.
	workshopStorage := &library{name: "nas-1", files: map[string][]byte{"jobs/a.gcode": workshopFiles["a.gcode"]}}
	workshop, workshopRoutes := startedWith(ctx, t,
		filesSettings(t, "workshop", workshopPorts, brnoPorts.client, material),
		session{fleet: noMachines{}, storage: workshopStorage}, workshopPorts.files, t.TempDir())
	awaitTrue(t, "workshop to join brno", func() bool { return workshop.binding.Path() == "hq/brno/workshop" })

	for routes, folder := range map[http.Handler]string{hqRoutes: "library/prints", workshopRoutes: "nas-1/jobs"} {
		got := answerOf(t, routes, http.MethodPut, "/files/shares/"+folder)
		if got.status != http.StatusNoContent {
			t.Fatalf("sharing %s answered %+v", folder, got)
		}
	}

	hqRoot, hqEntries := folderOf(t, "prints", hqFiles)
	workshopRoot, workshopEntries := folderOf(t, "jobs", workshopFiles)

	store := func(node fractal.NodePath, instance fractal.InstanceID, root sdkfiles.Identity) shownStore {
		return shownStore{Node: node, Instance: instance, Status: "OK", Props: map[string]json.RawMessage{
			"rootHash": json.RawMessage(strconv.Quote(root.Text())), "mediaPresent": json.RawMessage(`true`),
			"removable": json.RawMessage(`false`), "capacity": json.RawMessage(`null`), "free": json.RawMessage(`null`),
		}}
	}
	wantStores := []shownStore{store("hq", "library", hqRoot), store("hq/brno/workshop", "nas_1", workshopRoot)}

	var stores []shownStore

	awaitTrue(t, "brno to see both stores", func() bool {
		var listed struct {
			Items []shownStore `json:"items"`
		}

		got := answerOf(t, brnoRoutes, http.MethodGet, "/files/stores")
		if got.status != http.StatusOK || json.Unmarshal([]byte(got.body), &listed) != nil {
			return false
		}

		stores = listed.Items

		return reflect.DeepEqual(stores, wantStores)
	})

	listingAt := func(node, instance string, root sdkfiles.Identity) (int, shownListing) {
		got := answerOf(t, brnoRoutes, http.MethodGet, "/files/stores/"+node+"/"+instance+"/"+root.Text())

		var shown shownListing
		if got.status == http.StatusOK {
			if err := json.Unmarshal([]byte(got.body), &shown); err != nil {
				t.Fatalf("read the listing of %s: %v", node, err)
			}
		}

		return got.status, shown
	}

	type read struct {
		Status  int
		Listing shownListing
	}

	got := map[string]read{}

	for _, at := range []struct {
		node, instance string
		root           sdkfiles.Identity
	}{
		{"hq", "library", hqRoot}, {"hq/brno/workshop", "nas_1", workshopRoot},
		{"hq/zagreb", "library", hqRoot}, {"hq/brno", "library", hqRoot},
	} {
		status, shown := listingAt(at.node, at.instance, at.root)
		got[at.node] = read{Status: status, Listing: shown}
	}

	want := map[string]read{
		"hq": {Status: http.StatusOK, Listing: shownListing{
			Node: "hq", Instance: "library", Identity: hqRoot.Text(), Entries: hqEntries,
		}},
		"hq/brno/workshop": {Status: http.StatusOK, Listing: shownListing{
			Node: "hq/brno/workshop", Instance: "nas_1", Identity: workshopRoot.Text(), Entries: workshopEntries,
		}},
		"hq/zagreb": {Status: http.StatusNotFound},
		"hq/brno":   {Status: http.StatusNotFound},
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("brno read %+v, want %+v", got, want)
	}
}

// TestARevocationDropsTheCopiesFromAbove: a server the server above revoked
// drops its downward cache and the files entry of that parent when its next
// join is answered ERR_JOIN_REVOKED (fractal-files.md, Relaying and Caching).
func TestARevocationDropsTheCopiesFromAbove(t *testing.T) {
	if testing.Short() {
		t.Skip("this test starts two NATS servers and two HTTPS servers")
	}

	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	material := writeTLS(t)
	hqPorts, brnoPorts := portsOf(t), portsOf(t)

	hq, _ := startedWith(ctx, t, filesSettings(t, "hq", hqPorts, 0, material),
		session{fleet: noMachines{}, storage: &library{}}, hqPorts.files, t.TempDir())
	awaitTrue(t, "hq to listen", func() bool { return len(hq.binding.Status().Listeners) > 0 })

	brno, _ := startedWith(ctx, t, filesSettings(t, "brno", brnoPorts, hqPorts.client, material),
		session{fleet: noMachines{}, storage: &library{}}, brnoPorts.files, t.TempDir())
	awaitTrue(t, "brno to connect to hq", func() bool {
		return brno.binding.Status().Uplink.State == natsbind.UplinkConnected
	})

	cube := []byte("GCDE a cube from hq")
	id := sdkfiles.Sum(cube)

	if err := brno.files.cache.Put(id, func(sink pfiles.Sink) (pfiles.Retention, error) {
		_, err := sink.Write(cube)

		return pfiles.Retention{Unlimited: true}, err
	}); err != nil {
		t.Fatalf("keep a copy on brno: %v", err)
	}

	if err := hq.binding.Revoke(ctx, "brno"); err != nil {
		t.Fatalf("Revoke: %v", err)
	}

	joinErr := brno.binding.Join(ctx)
	_, entry := brno.binding.ParentExtension(sdkfiles.ExtensionName)

	type result struct {
		Revoked bool
		Held    bool
		Entry   bool
	}

	var denial *join.DenialError

	got := result{
		Revoked: errors.As(joinErr, &denial) && denial.Denial.Code == fractal.CodeJoinRevoked,
		Held:    brno.files.cache.Path(id) != "",
		Entry:   entry,
	}
	if want := (result{Revoked: true}); got != want {
		t.Errorf("after the revocation brno gave %+v, want %+v", got, want)
	}
}
