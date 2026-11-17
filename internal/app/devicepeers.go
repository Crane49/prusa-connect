package app

import "net/netip"

// peers maps the address each printer of this server reaches it from to the
// printer's id. The list of printers nearby reads it, so a printer that is
// already here is told apart from one still to add.
//
// A printer that is away, or whose session has ended, is named by the
// address it last came from. A printer announces itself as soon as it is on
// the network again, before it reconnects, and one that does not reconnect
// is still on the page above the list. A connected printer that comes from
// the same address names it instead.
//
// ponytail: the last address lives in memory, so a printer that has not
// connected since a restart is not named until it does.
//
// The addresses are read after the fleet lock is released, because each one
// takes its session's own lock.
func (d *devices) peers() map[netip.Addr]string {
	d.mu.Lock()
	reporting := make([]*device, 0, len(d.entries)+len(d.departed))

	for _, table := range []map[string]*device{d.entries, d.departed} {
		for _, entry := range table {
			if entry.peer != nil {
				reporting = append(reporting, entry)
			}
		}
	}
	d.mu.Unlock()

	here := make(map[netip.Addr]string, len(reporting))
	away := make(map[netip.Addr]string)

	for _, entry := range reporting {
		// A retired id is not its machine's id. The id the machine holds after
		// the retirement is the one to name.
		if _, _, retired := entry.retirement(); retired {
			continue
		}

		addr, connected := entry.address()
		if !addr.IsValid() {
			continue
		}

		if connected {
			claim(here, addr, entry.id)
		} else {
			claim(away, addr, entry.id)
		}
	}

	for addr, id := range away {
		if _, taken := here[addr]; !taken {
			here[addr] = id
		}
	}

	return here
}

// claim names an address after a printer. Two printers behind one NAT router
// come from one address, and the lower id names it, so the answer does not
// change between two reads.
func claim(named map[netip.Addr]string, addr netip.Addr, id string) {
	if held, taken := named[addr]; !taken || id < held {
		named[addr] = id
	}
}

// address is where the machine's connection comes from, and whether that
// connection is open. A machine that is away answers with the address it last
// came from, and the zero address when none was ever read.
func (e *device) address() (netip.Addr, bool) {
	if e.peer != nil && e.connected() {
		if addr := e.peer.Peer(); addr.IsValid() {
			addr = addr.Unmap().WithZone("")

			e.mu.Lock()
			e.lastPeer = addr
			e.mu.Unlock()

			return addr, true
		}
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	return e.lastPeer, false
}
