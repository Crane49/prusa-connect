package sysmem

import (
	"path"
	"slices"
	"strconv"
	"strings"
)

const (
	cgroupRoot = "/sys/fs/cgroup"
	// v1 reports "no limit" as a page-aligned value near MaxInt64.
	noLimitFloor = int64(1) << 62
	// A /proc/self/cgroup line is "id:controllers:path".
	cgroupFields = 3
)

type readFunc func(path string) ([]byte, error)

// cgroupLimit returns the tightest memory limit that applies to the process
// described by selfCgroup (the text of /proc/self/cgroup). Ancestors count
// because a parent's limit binds its children.
func cgroupLimit(selfCgroup string, read readFunc) (int64, bool) {
	var limit int64
	var found bool
	for line := range strings.SplitSeq(selfCgroup, "\n") {
		parts := strings.SplitN(strings.TrimSpace(line), ":", cgroupFields)
		if len(parts) != cgroupFields {
			continue
		}
		var n int64
		var ok bool
		switch {
		case parts[0] == "0" && parts[1] == "":
			n, ok = minUp(parts[2], cgroupRoot, "memory.max", read)
		case slices.Contains(strings.Split(parts[1], ","), "memory"):
			n, ok = minUp(parts[2], path.Join(cgroupRoot, "memory"), "memory.limit_in_bytes", read)
		}
		if ok && (!found || n < limit) {
			limit, found = n, true
		}
	}
	return limit, found
}

// minUp returns the smallest numeric limit in file under base, for the cgroup
// at dir and each of its ancestors up to the root.
func minUp(dir, base, file string, read readFunc) (int64, bool) {
	var limit int64
	var found bool
	for dir = path.Clean("/" + dir); ; dir = path.Dir(dir) {
		if data, err := read(path.Join(base, dir, file)); err == nil {
			n, perr := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
			if perr == nil && n > 0 && n < noLimitFloor && (!found || n < limit) {
				limit, found = n, true
			}
		}
		if dir == "/" {
			return limit, found
		}
	}
}
