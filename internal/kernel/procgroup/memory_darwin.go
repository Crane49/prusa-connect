package procgroup

import (
	"bytes"
	"fmt"
	"os/exec"
	"strconv"
)

// kibibyte converts the kB figures of ps to bytes.
const kibibyte = 1024

// psColumns is one ps line: pid, ppid, pgid, rss.
const psColumns = 4

// procStat is what the walk and Memory need of one ps line.
type procStat struct {
	pid, ppid, pgrp int
	rss             int64
}

// member reports whether p is in leader's process group. ps on darwin gives
// no session id, so a process that moved to a group of its own is found only
// through its parent while that lives; darwin serves development only.
func (p procStat) member(leader int) bool {
	return p.pgrp == leader
}

// readProcs lists every process ps reports.
func readProcs() ([]procStat, error) {
	// A fixed program; readProcs takes no context and ps returns at once.
	out, err := exec.Command("ps", "-A", "-o", "pid=,ppid=,pgid=,rss=").Output() //nolint:noctx // see above.
	if err != nil {
		return nil, fmt.Errorf("procgroup: list processes: %w", err)
	}

	var procs []procStat

	for line := range bytes.Lines(out) {
		fields := bytes.Fields(line)
		if len(fields) != psColumns {
			continue
		}

		var values [psColumns]int

		for i, field := range fields {
			if values[i], err = strconv.Atoi(string(field)); err != nil {
				return nil, fmt.Errorf("procgroup: list processes: %w", err)
			}
		}

		procs = append(procs, procStat{
			pid: values[0], ppid: values[1], pgrp: values[2], rss: int64(values[3]) * kibibyte,
		})
	}

	return procs, nil
}

// Memory sums the resident set size of the leader and every process walk
// reaches from it, as ps reports it. It counts shared and file-backed pages
// too, so it reads higher than the anonymous memory Linux reports; it serves
// development only.
func (g *Group) Memory() (int64, error) {
	procs, err := readProcs()
	if err != nil {
		return 0, err
	}

	var (
		total  int64
		leader bool
	)

	for _, p := range procs {
		if p.pid == g.pid {
			total, leader = p.rss, true
		}
	}

	if !leader {
		return 0, fmt.Errorf("procgroup: read memory of %d: no such process", g.pid)
	}

	for _, p := range walk(procs, g.pid) {
		total += p.rss
	}

	return total, nil
}
