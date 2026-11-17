//go:build linux || darwin

package procgroup

// descendants lists the processes the group reaches; see walk.
func (g *Group) descendants() ([]procStat, error) {
	procs, err := readProcs()
	if err != nil {
		return nil, err
	}

	return walk(procs, g.pid), nil
}

// walk returns every member of leader's module but the leader (see
// procStat.member), and all descendants of the leader and of those members.
// Members are found without parent links, so a process whose parent exited
// still counts; descendants are found through parent links, so a process that
// left with setsid still counts while its parent lives.
func walk(procs []procStat, leader int) []procStat {
	byPid := make(map[int]procStat, len(procs))
	children := make(map[int][]int, len(procs))
	queue := []int{}

	for _, p := range procs {
		byPid[p.pid] = p
		children[p.ppid] = append(children[p.ppid], p.pid)

		if p.member(leader) && p.pid != leader {
			queue = append(queue, p.pid)
		}
	}

	queue = append(queue, children[leader]...)
	seen := map[int]bool{leader: true}

	var found []procStat

	for len(queue) > 0 {
		pid := queue[0]
		queue = queue[1:]

		if seen[pid] {
			continue
		}

		seen[pid] = true
		queue = append(queue, children[pid]...)
		found = append(found, byPid[pid])
	}

	return found
}
