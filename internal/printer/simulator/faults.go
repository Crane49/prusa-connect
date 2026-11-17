package simulator

import (
	"context"
	"strings"
	"time"
)

// The failure modes, as inputs.
//
// None of them is a probability and none of them is timed off a clock the test
// does not own. A simulator that fails on its own schedule teaches people to
// rerun the suite until it passes.

// GoOffline drops the current session and refuses to redial until GoOnline.
//
// It is the fault that tests the offline rule: the host queues commands for a
// printer that is away and drains them when it returns.
func (p *Printer) GoOffline() {
	p.mu.Lock()

	conn := p.conn

	if !p.offline {
		p.offline = true
		p.online = make(chan struct{})
	}

	p.mu.Unlock()

	if conn != nil {
		_ = conn.Close()
	}
}

// GoOnline lets the printer redial. The next session drains whatever the host
// queued while it was away.
func (p *Printer) GoOnline() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.offline {
		return
	}

	p.offline = false
	close(p.online)
}

// Offline reports whether the printer has been told to stay off the wire.
func (p *Printer) Offline() bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.offline
}

// SetResponseDelay makes the printer answer slowly, so a host-side timeout and
// a breaker are reachable without waiting for a real one.
func (p *Printer) SetResponseDelay(delay time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.delay = delay
}

// SetAcceptFirst makes the printer answer every command with ACCEPTED before
// the event that carries its answer, which is what a printer on the Prusa
// Connect SDK does. Buddy firmware sends ACCEPTED for a G-code command only.
func (p *Printer) SetAcceptFirst(on bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.acceptFirst = on
}

// ResponseDelay is the delay SetResponseDelay set.
func (p *Printer) ResponseDelay() time.Duration {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.delay
}

// RemoveStorage pulls a device out, which is what a USB stick removed mid-job
// looks like from the server. A path on it is refused and INFO stops listing
// it. ok is false when no such device was mounted.
func (p *Printer) RemoveStorage(mountpoint string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	for i, device := range p.storages {
		if device.Mountpoint != mountpoint {
			continue
		}

		p.storages = append(p.storages[:i], p.storages[i+1:]...)

		return true
	}

	return false
}

// InsertStorage mounts a device, replacing any device already at that
// mountpoint.
func (p *Printer) InsertStorage(device Storage) {
	p.mu.Lock()
	defer p.mu.Unlock()

	for i, existing := range p.storages {
		if existing.Mountpoint == device.Mountpoint {
			p.storages[i] = device

			return
		}
	}

	p.storages = append(p.storages, device)
}

// SetFreeSpace is the full-disk fault. Zero free bytes refuses every write to
// that device, which is where storage errors actually come from: out of space
// is reported at the write and not at the request. ok is false when no such
// device was mounted.
func (p *Printer) SetFreeSpace(mountpoint string, free int64) bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	for i, device := range p.storages {
		if device.Mountpoint != mountpoint {
			continue
		}

		p.storages[i].FreeSpace = free

		return true
	}

	return false
}

// checkStorage answers whether a command's target device can take it.
func (p *Printer) checkStorage(command jsonCommand) (reason string, allowed bool) {
	if size, ok := command.Kwargs["orig_size"].(float64); ok && size >= transferMaximum {
		return ReasonTransferTooLarge, false
	}

	mountpoint := mountpointOf(pathArgument(command.Kwargs))
	if mountpoint == "" {
		return "", true
	}

	device, mounted := p.storage(mountpoint)
	if !mounted {
		return ReasonMediaNotInserted, false
	}

	if !writingCommands[command.Command] {
		return "", true
	}

	switch {
	case device.ReadOnly:
		return ReasonStorageReadOnly, false
	case device.FreeSpace <= 0:
		return ReasonStorageFull, false
	default:
		return "", true
	}
}

func (p *Printer) storage(mountpoint string) (Storage, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, device := range p.storages {
		if device.Mountpoint == mountpoint {
			return device, true
		}
	}

	return Storage{}, false
}

// waitOnline blocks while the printer is offline. ok is false when the context
// ended first, which is a stop and not a failure.
func (p *Printer) waitOnline(ctx context.Context) bool {
	p.mu.Lock()
	online := p.online
	p.mu.Unlock()

	select {
	case <-ctx.Done():
		return false
	case <-online:
		return true
	}
}

// mountpointOf reads the storage device out of a firmware-side path. Those
// carry a leading slash and are storage-prefixed, /usb/model.gcode, and the
// connector is where the slash is added and removed.
func mountpointOf(path string) string {
	if !strings.HasPrefix(path, "/") {
		return ""
	}

	rest := path[1:]
	if segment, _, found := strings.Cut(rest, "/"); found {
		return "/" + segment
	}

	return "/" + rest
}

// FailJob ends the running print in ERROR, the word a machine reports when a
// fault takes the print off the bed. It reports false when nothing was
// printing.
func (p *Printer) FailJob() bool {
	return p.stopJob(stateError)
}

// AbortBeforeStart ends the running print the way a printer does when its
// operator cancels from its own menu before printing began, at the filament
// check for one: the machine goes to IDLE and reports no STOPPED. It reports
// false when nothing was printing.
func (p *Printer) AbortBeforeStart() bool {
	return p.stopJob(stateIdle)
}
