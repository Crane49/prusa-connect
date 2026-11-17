// Package sources tells which printer sent a datagram.
//
// A datagram names its printer by the MAC address of the printer's Ethernet
// port. The xBuddy connector puts the printer's network on its state tree,
// under /network, when the printer connects. A
// printer on Ethernet is matched by its MAC. A printer on Wi-Fi alone reports
// no Ethernet MAC, so it is matched by the datagram's source address against
// its Wi-Fi address. A pair matched once is kept in the data directory, so it
// survives a new address and a restart.
//
// A MAC is matched against every printer, also one that is away, because a
// printer whose link to the server is down may still send its logs. An
// address is matched only against a connected printer, because one that is
// away may have given its address to another. When two printers report the
// same MAC or address, the connected one wins, and two of the same kind match
// neither.
//
// A MAC that matches no printer is kept as an unassigned source, with its
// counts, and a person can assign it to a printer by hand.
//
// A source address can be forged, so a pair matched by the address alone is
// trusted less than one matched by the MAC. A full table forgets such a pair
// after the unassigned sources. Such a pair is also not written to the data
// directory when its datagram arrives. The next read of the fleet writes it,
// so a burst of datagrams from made-up MACs writes the file once at most.
package sources

import (
	"cmp"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/deviceapi"
)

// The state tree paths the connector writes the printer's network to.
const (
	pathLanMAC   = "/network/lan_mac"
	pathWifiMAC  = "/network/wifi_mac"
	pathLanIPv4  = "/network/lan_ipv4"
	pathWifiIPv4 = "/network/wifi_ipv4"
)

const (
	// RefreshEvery is how often the fleet is read again, so a printer that
	// joined or moved is matched without a miss.
	RefreshEvery = time.Minute
	// refreshAfterMiss is the shortest wait between two reads a miss asks
	// for, so an unknown sender cannot make the module read the fleet on every
	// datagram.
	refreshAfterMiss = 10 * time.Second
	// maxSources bounds the table. A stranger sending datagrams with new MACs
	// would otherwise grow it without end. A new MAC in a full table takes the
	// place of the unassigned source seen longest ago, or when there is none,
	// of the source matched by its address alone seen longest ago.
	maxSources = 1024
	// maxDevices bounds one read of the fleet.
	maxDevices = 4096
)

// fileName is where the matched pairs are kept, in the data directory.
const fileName = "sources.json"

// ErrUnknownSource reports a MAC this module has never received from.
var ErrUnknownSource = errors.New("sources: no datagram has come from that MAC")

// ErrUnknownDevice reports a device id the module's principal does not see.
var ErrUnknownDevice = errors.New("sources: no such device")

// Fleet is the part of the host's device door this package reads.
type Fleet interface {
	List(ctx context.Context, params deviceapi.ListParams) (deviceapi.Page, error)
}

// Source is one MAC that sent something.
type Source struct {
	MAC string `json:"mac"`
	// Device is the device id the MAC is matched to, and empty while it is
	// matched to none.
	Device string `json:"device_id,omitempty"`
	// DeviceName is the name the fleet gives that device.
	DeviceName string `json:"device_name,omitempty"`
	// Manual is whether a person assigned the MAC, which no refresh undoes.
	Manual bool `json:"manual"`
	// byAddress is whether the match came from the datagram's address alone,
	// which a sender can forge.
	byAddress bool
	LastSeen  time.Time `json:"last_seen"`
	LastIP    string    `json:"last_ip,omitempty"`
	Syslog    int64     `json:"syslog"`
	Metrics   int64     `json:"metrics"`
}

// kept is what the data directory holds for one MAC.
type kept struct {
	Device    string `json:"device_id"`
	Manual    bool   `json:"manual,omitempty"`
	ByAddress bool   `json:"by_address,omitempty"`
}

// Table is every source, and what the fleet says about the printers.
type Table struct {
	path  string
	fleet Fleet
	now   func() time.Time

	mu        sync.Mutex
	sources   map[string]*Source
	byMAC     map[string]string
	byIP      map[string]string
	names     map[string]string
	refreshed time.Time
	saveErr   error
	// unsaved is whether a pair matched by address waits for the next read
	// of the fleet to be written.
	unsaved bool

	// aliasKey makes the name Alias gives a MAC. It is new in each process,
	// so the name says nothing about the MAC outside it.
	aliasKey []byte

	kick chan struct{}
}

// Open reads the pairs kept in dir. An empty dir keeps nothing on disk, which
// is a module granted no data directory.
func Open(dir string, fleet Fleet, now func() time.Time) (*Table, error) {
	t := &Table{
		fleet:   fleet,
		now:     now,
		sources: map[string]*Source{},
		byMAC:   map[string]string{},
		byIP:    map[string]string{},
		names:   map[string]string{},
		kick:    make(chan struct{}, 1),
	}

	t.aliasKey = make([]byte, sha256.Size)
	_, _ = rand.Read(t.aliasKey) // crypto/rand.Read never fails; it ends the program instead

	if dir == "" {
		return t, nil
	}

	t.path = filepath.Join(dir, fileName)

	raw, err := os.ReadFile(t.path)
	if errors.Is(err, os.ErrNotExist) {
		return t, nil
	}

	if err != nil {
		return nil, fmt.Errorf("read %s: %w", fileName, err)
	}

	var held map[string]kept
	if err = json.Unmarshal(raw, &held); err != nil {
		return nil, fmt.Errorf("read %s: %w", fileName, err)
	}

	for mac, pair := range held {
		t.sources[mac] = &Source{
			MAC: mac, Device: pair.Device, Manual: pair.Manual, byAddress: pair.ByAddress && !pair.Manual,
		}
	}

	return t, nil
}

// Run reads the fleet now, then every RefreshEvery and whenever a miss asks,
// until ctx ends.
func (t *Table) Run(ctx context.Context, failed func(error)) {
	ticker := time.NewTicker(RefreshEvery)
	defer ticker.Stop()

	for {
		if err := t.Refresh(ctx); err != nil && ctx.Err() == nil && failed != nil {
			failed(err)
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-t.kick:
		}
	}
}

// Refresh reads every device the module may see and matches the sources
// again. A source a person assigned keeps its device. It writes the pairs
// when one changed, or when a pair matched by address waits to be written.
func (t *Table) Refresh(ctx context.Context) error {
	seen, err := t.read(ctx)
	if err != nil {
		return err
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	t.byMAC, t.byIP, t.names, t.refreshed = seen.byMAC, seen.byIP, seen.names, t.now()

	changed := t.unsaved

	for _, source := range t.sources {
		if source.Manual {
			continue
		}

		device, byAddress := t.match(source.MAC, source.LastIP)
		if device != "" && (device != source.Device || byAddress != source.byAddress) {
			source.Device, source.byAddress = device, byAddress
			changed = true
		}
	}

	if changed {
		t.saveLocked()
	}

	return nil
}

// fleetView is what one read of the fleet says about the printers' networks.
type fleetView struct {
	byMAC, byIP, names map[string]string
}

// read pages through the fleet as the module itself.
func (t *Table) read(ctx context.Context) (fleetView, error) {
	names := map[string]string{}
	macs, ips := claims{}, claims{}

	err := each(ctx, t.fleet, "", func(device deviceapi.Device) {
		names[device.ID] = device.Name

		for _, path := range []string{pathLanMAC, pathWifiMAC} {
			if mac := text(device.State[path]); mac != "" {
				macs.add(strings.ToLower(mac), device)
			}
		}

		if !device.Connected {
			return
		}

		for _, path := range []string{pathLanIPv4, pathWifiIPv4} {
			if ip := text(device.State[path]); ip != "" {
				ips.add(ip, device)
			}
		}
	})
	if err != nil {
		return fleetView{}, err
	}

	return fleetView{byMAC: macs.owners(), byIP: ips.owners(), names: names}, nil
}

// claims is every device that reports one MAC or one address, split by
// whether it is connected.
type claims map[string]*claim

type claim struct {
	connected, away map[string]bool
}

func (c claims) add(key string, device deviceapi.Device) {
	held, seen := c[key]
	if !seen {
		held = &claim{connected: map[string]bool{}, away: map[string]bool{}}
		c[key] = held
	}

	if device.Connected {
		held.connected[device.ID] = true
	} else {
		held.away[device.ID] = true
	}
}

// owners is the one device each key belongs to: the connected device that
// reports it, or the one away when none is connected. A key two devices of
// the same kind report belongs to neither.
func (c claims) owners() map[string]string {
	out := map[string]string{}

	for key, held := range c {
		devices := held.connected
		if len(devices) == 0 {
			devices = held.away
		}

		if len(devices) != 1 {
			continue
		}

		for id := range devices {
			out[key] = id
		}
	}

	return out
}

// each calls visit on every device principal may see, at most maxDevices. An
// empty principal is the module itself.
func each(ctx context.Context, fleet Fleet, principal string, visit func(deviceapi.Device)) error {
	for offset := 0; offset < maxDevices; {
		page, err := fleet.List(ctx, deviceapi.ListParams{
			Principal: principal, Limit: deviceapi.MaxPageLimit, Offset: offset,
		})
		if err != nil {
			return fmt.Errorf("list the devices: %w", err)
		}

		for _, device := range page.Items {
			visit(device)
		}

		offset += len(page.Items)
		if len(page.Items) == 0 || offset >= page.Total {
			break
		}
	}

	return nil
}

// Visible is the id of every device principal may see. The host answers the
// list as that person, so the answer is what they would see at the API.
func Visible(ctx context.Context, fleet Fleet, principal string) (map[string]bool, error) {
	out := map[string]bool{}

	err := each(ctx, fleet, principal, func(device deviceapi.Device) {
		out[device.ID] = true
	})
	if err != nil {
		return nil, err
	}

	return out, nil
}

// Covers reports whether ids holds every device the module saw on its last
// read of the fleet. It is false before the first read, and false when that
// read found no device: a fleet the module cannot see is not one a person
// sees whole, so nobody sees the records matched to no printer then.
func (t *Table) Covers(ids map[string]bool) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.refreshed.IsZero() || len(t.names) == 0 {
		return false
	}

	for id := range t.names {
		if !ids[id] {
			return false
		}
	}

	return true
}

// Resolve answers the device a datagram came from, and counts it against its
// source. It never waits for the fleet: a miss asks Run for a read and the
// datagram is kept under its MAC, which a later query still finds.
func (t *Table) Resolve(mac, ip string, syslog bool) string {
	t.mu.Lock()
	defer t.mu.Unlock()

	source, known := t.sources[mac]
	if !known {
		if len(t.sources) >= maxSources && !t.evictLocked() {
			device, _ := t.match(mac, ip)

			return device
		}

		source = &Source{MAC: mac}
		t.sources[mac] = source
	}

	source.LastSeen = t.now()
	source.LastIP = ip

	if syslog {
		source.Syslog++
	} else {
		source.Metrics++
	}

	if source.Device == "" {
		if device, byAddress := t.match(mac, ip); device != "" {
			source.Device, source.byAddress = device, byAddress
			t.savePair(byAddress)
		} else if t.now().Sub(t.refreshed) >= refreshAfterMiss {
			select {
			case t.kick <- struct{}{}:
			default:
			}
		}
	}

	return source.Device
}

// savePair writes the pairs now for a match by MAC, and leaves a match by
// address to the next read of the fleet. The caller holds t.mu.
func (t *Table) savePair(byAddress bool) {
	if byAddress {
		t.unsaved = true

		return
	}

	t.saveLocked()
}

// evictLocked removes the unassigned source seen longest ago. When there is
// none, it removes the source matched by its address alone seen longest ago,
// because a sender can forge an address. It reports false when every source
// is matched by MAC or by hand. The caller holds t.mu.
func (t *Table) evictLocked() bool {
	unassigned, byAddress := "", ""

	for mac, source := range t.sources {
		switch {
		case source.Manual:
		case source.Device == "":
			if unassigned == "" || source.LastSeen.Before(t.sources[unassigned].LastSeen) {
				unassigned = mac
			}
		case source.byAddress:
			if byAddress == "" || source.LastSeen.Before(t.sources[byAddress].LastSeen) {
				byAddress = mac
			}
		}
	}

	oldest := cmp.Or(unassigned, byAddress)
	if oldest == "" {
		return false
	}

	if t.sources[oldest].byAddress {
		// The file is written at the next read of the fleet, without it.
		t.unsaved = true
	}

	delete(t.sources, oldest)

	return true
}

// match is the device a MAC or an address belongs to, and whether it was the
// address that matched. The caller holds t.mu.
func (t *Table) match(mac, ip string) (string, bool) {
	if device, found := t.byMAC[mac]; found {
		return device, false
	}

	device := t.byIP[ip]

	return device, device != ""
}

// Assign matches a MAC to a device by hand. An empty device takes the match
// away, and the next refresh matches the MAC again by itself.
func (t *Table) Assign(mac, device string) (Source, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	source, known := t.sources[mac]
	if !known {
		return Source{}, fmt.Errorf("%w: %s", ErrUnknownSource, mac)
	}

	if device != "" {
		if _, seen := t.names[device]; !seen {
			return Source{}, fmt.Errorf("%w: %s", ErrUnknownDevice, device)
		}
	}

	source.Device = device
	source.Manual = device != ""
	source.byAddress = false

	if device == "" {
		source.Device, source.byAddress = t.match(mac, source.LastIP)
	}

	t.saveLocked()

	return t.view(source), nil
}

// List is every source, by MAC.
func (t *Table) List() []Source {
	t.mu.Lock()
	defer t.mu.Unlock()

	out := make([]Source, 0, len(t.sources))
	for _, mac := range slices.Sorted(maps.Keys(t.sources)) {
		out = append(out, t.view(t.sources[mac]))
	}

	return out
}

// view is a copy of one source with its device's name. The caller holds t.mu.
func (t *Table) view(source *Source) Source {
	out := *source
	out.DeviceName = t.names[source.Device]

	return out
}

// MACsOf is every MAC matched to device. A query for a device reads the
// records kept under them too, which covers what arrived before the match.
func (t *Table) MACsOf(device string) []string {
	t.mu.Lock()
	defer t.mu.Unlock()

	var out []string

	for mac, source := range t.sources {
		if source.Device == device {
			out = append(out, mac)
		}
	}

	slices.Sort(out)

	return out
}

// Matched is every MAC matched to a device now, and its device.
func (t *Table) Matched() map[string]string {
	t.mu.Lock()
	defer t.mu.Unlock()

	out := make(map[string]string, len(t.sources))

	for mac, source := range t.sources {
		if source.Device != "" {
			out[mac] = source.Device
		}
	}

	return out
}

// Alias is a name for a MAC that does not show the MAC, such as
// unassigned-3f2a9c01b4d7. It is the same for one MAC while this process
// runs, so the files of one diagnostics bundle agree.
func (t *Table) Alias(mac string) string {
	sum := hmac.New(sha256.New, t.aliasKey)
	_, _ = sum.Write([]byte(mac)) // a hash.Hash never returns an error

	const aliasBytes = 6

	return "unassigned-" + hex.EncodeToString(sum.Sum(nil)[:aliasBytes])
}

// DeviceOf is the device a MAC is matched to now, and empty for none.
func (t *Table) DeviceOf(mac string) string {
	t.mu.Lock()
	defer t.mu.Unlock()

	if source, known := t.sources[mac]; known {
		return source.Device
	}

	return ""
}

// SaveError is the last failure to keep the pairs on disk, and nil when the
// last write landed.
func (t *Table) SaveError() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	return t.saveErr
}

// saveLocked writes the matched pairs. It writes a whole new file and renames
// it into place, so a crash leaves the old pairs and never half of them. The
// caller holds t.mu.
func (t *Table) saveLocked() {
	if t.path == "" {
		return
	}

	t.unsaved = false

	held := map[string]kept{}

	for mac, source := range t.sources {
		if source.Device != "" {
			held[mac] = kept{Device: source.Device, Manual: source.Manual, ByAddress: source.byAddress}
		}
	}

	t.saveErr = writeFile(t.path, held)
}

func writeFile(path string, held map[string]kept) error {
	raw, err := json.MarshalIndent(held, "", "  ")
	if err != nil {
		return fmt.Errorf("encode %s: %w", fileName, err)
	}

	temp := path + ".tmp"

	file, err := os.OpenFile(temp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("write %s: %w", fileName, err)
	}

	_, writeErr := file.Write(raw)
	syncErr := file.Sync()

	if err = errors.Join(writeErr, syncErr, file.Close()); err != nil {
		return fmt.Errorf("write %s: %w", fileName, err)
	}

	if err = os.Rename(temp, path); err != nil {
		return fmt.Errorf("write %s: %w", fileName, err)
	}

	return nil
}

// text reads a state tree value that is a JSON string, and nothing else.
func text(raw json.RawMessage) string {
	var value string
	if len(raw) == 0 || json.Unmarshal(raw, &value) != nil {
		return ""
	}

	return strings.TrimSpace(value)
}
