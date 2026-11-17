package procgroup

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

// kibibyte converts the kB figures of /proc to bytes.
const kibibyte = 1024

// procStat is what the walk and the Reaper need from /proc/<pid>/stat.
type procStat struct {
	pid, ppid, pgrp, session int
	state                    byte
}

// member reports whether p belongs to the module that leader started: its
// process group, or its session, which a process that moved to a group of its
// own with setpgid keeps after its parent exits. A process that calls setsid
// leaves both and is found only through its parent while that lives.
func (p procStat) member(leader int) bool {
	return p.pgrp == leader || p.session == leader
}

// readProcs lists every process in /proc. A process that ends while the list
// is taken is left out.
func readProcs() ([]procStat, error) {
	paths, err := filepath.Glob("/proc/[0-9]*/stat")
	if err != nil {
		return nil, fmt.Errorf("procgroup: list /proc: %w", err)
	}

	procs := make([]procStat, 0, len(paths))

	for _, path := range paths {
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			continue
		}

		if p, ok := parseStat(data); ok {
			procs = append(procs, p)
		}
	}

	return procs, nil
}

// parseStat reads "pid (comm) state ppid pgrp session ...". comm may hold spaces and
// parentheses, so the fields after it are found from the last ')'.
func parseStat(data []byte) (procStat, bool) {
	open, end := bytes.IndexByte(data, '('), bytes.LastIndexByte(data, ')')
	if open < 1 || end < open {
		return procStat{}, false
	}

	pid, err := strconv.Atoi(string(bytes.TrimSpace(data[:open])))
	if err != nil {
		return procStat{}, false
	}

	rest := bytes.Fields(data[end+1:])
	if len(rest) < 4 || len(rest[0]) != 1 {
		return procStat{}, false
	}

	ppid, err := strconv.Atoi(string(rest[1]))
	if err != nil {
		return procStat{}, false
	}

	pgrp, err := strconv.Atoi(string(rest[2]))
	if err != nil {
		return procStat{}, false
	}

	session, err := strconv.Atoi(string(rest[3]))
	if err != nil {
		return procStat{}, false
	}

	return procStat{pid: pid, ppid: ppid, pgrp: pgrp, session: session, state: rest[0][0]}, true
}

// Memory sums RssAnon over the leader and every process walk reaches from it.
func (g *Group) Memory() (int64, error) {
	total, err := rssAnon(g.pid)
	if err != nil {
		return 0, fmt.Errorf("procgroup: read memory of %d: %w", g.pid, err)
	}

	procs, err := g.descendants()
	if err != nil {
		return 0, err
	}

	for _, p := range procs {
		if kb, readErr := rssAnon(p.pid); readErr == nil {
			total += kb
		}
	}

	return total, nil
}

// rssAnon reads the RssAnon line of /proc/<pid>/status in bytes. A zombie has
// no such line, nor has a kernel older than 4.5, and either is an error.
func rssAnon(pid int) (int64, error) {
	file, err := os.Open("/proc/" + strconv.Itoa(pid) + "/status")
	if err != nil {
		return 0, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		value, found := bytes.CutPrefix(scanner.Bytes(), []byte("RssAnon:"))
		if !found {
			continue
		}

		fields := bytes.Fields(value)
		if len(fields) == 0 {
			return 0, errors.New("empty RssAnon line")
		}

		kb, parseErr := strconv.ParseInt(string(fields[0]), 10, 64)
		if parseErr != nil {
			return 0, fmt.Errorf("parse RssAnon: %w", parseErr)
		}

		return kb * kibibyte, nil
	}

	if err = scanner.Err(); err != nil {
		return 0, fmt.Errorf("read status: %w", err)
	}

	return 0, errors.New("no RssAnon line")
}
