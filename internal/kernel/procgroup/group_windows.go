package procgroup

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Group is a started command together with every process it starts.
type Group struct {
	pid int
	job windows.Handle
}

// ponytail: a member list longer than this fails Memory with ERROR_MORE_DATA;
// grow the buffer and retry if a module ever runs that many processes.
const maxJobMembers = 512

// processIDList is JOBOBJECT_BASIC_PROCESS_ID_LIST with room for
// maxJobMembers ids.
type processIDList struct {
	assigned uint32
	listed   uint32
	ids      [maxJobMembers]uintptr
}

// processMemoryCounters is PROCESS_MEMORY_COUNTERS_EX.
type processMemoryCounters struct {
	cb                         uint32
	pageFaultCount             uint32
	peakWorkingSetSize         uintptr
	workingSetSize             uintptr
	quotaPeakPagedPoolUsage    uintptr
	quotaPagedPoolUsage        uintptr
	quotaPeakNonPagedPoolUsage uintptr
	quotaNonPagedPoolUsage     uintptr
	pagefileUsage              uintptr
	peakPagefileUsage          uintptr
	privateUsage               uintptr
}

// Prepare starts cmd in a console process group of its own. Call it before
// cmd.Start.
func Prepare(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}

	cmd.SysProcAttr.CreationFlags |= windows.CREATE_NEW_PROCESS_GROUP
}

// Attach puts cmd in a job object that kills every member when it closes.
// Call it after cmd.Start. A process cmd starts before Attach returns is not
// in the job: os/exec gives no way to start cmd suspended.
func Attach(cmd *exec.Cmd) (*Group, error) {
	if cmd.Process == nil {
		return nil, errors.New("procgroup: attach before the command started")
	}

	handle, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, fmt.Errorf("procgroup: create job object: %w", err)
	}

	g := &Group{pid: cmd.Process.Pid, job: handle}

	if err = g.setLimits(windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE, 0); err != nil {
		return nil, errors.Join(err, g.Close())
	}

	process, err := windows.OpenProcess(
		windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE,
		false,
		uint32(g.pid), //nolint:gosec // a Windows pid is a DWORD.
	)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("procgroup: open process %d: %w", g.pid, err), g.Close())
	}
	defer windows.CloseHandle(process) //nolint:errcheck // the job holds the process from here on.

	if err = windows.AssignProcessToJobObject(handle, process); err != nil {
		return nil, errors.Join(fmt.Errorf("procgroup: assign process %d: %w", g.pid, err), g.Close())
	}

	return g, nil
}

func (g *Group) setLimits(flags uint32, jobMemory uintptr) error {
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{JobMemoryLimit: jobMemory}
	info.BasicLimitInformation.LimitFlags = flags

	if _, err := windows.SetInformationJobObject(g.job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		return fmt.Errorf("procgroup: set job limits: %w", err)
	}

	return nil
}

// LimitMemory caps the committed memory of the whole job at bytes.
func (g *Group) LimitMemory(bytes int64) error {
	if bytes <= 0 {
		return fmt.Errorf("procgroup: memory limit %d is not positive", bytes)
	}

	return g.setLimits(windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE|windows.JOB_OBJECT_LIMIT_JOB_MEMORY,
		uintptr(bytes))
}

// Kill ends every process in the job.
func (g *Group) Kill() error {
	if err := windows.TerminateJobObject(g.job, 1); err != nil {
		return fmt.Errorf("procgroup: terminate job: %w", err)
	}

	return nil
}

// Signal kills the job: Windows cannot deliver a signal to a group.
func (g *Group) Signal(os.Signal) error { return g.Kill() }

// Alive reports whether any process is left in the job.
func (g *Group) Alive() bool {
	list, err := g.members()

	return err == nil && list.assigned > 0
}

// Memory sums the private bytes of every process in the job. It is the
// current figure, not the job's peak.
func (g *Group) Memory() (int64, error) {
	list, err := g.members()
	if err != nil {
		return 0, err
	}

	if list.assigned == 0 {
		return 0, errors.New("procgroup: the job has no process left")
	}

	getInfo := windows.NewLazySystemDLL("psapi.dll").NewProc("GetProcessMemoryInfo")

	var total int64

	for _, id := range list.ids[:list.listed] {
		process, openErr := windows.OpenProcess(
			windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.PROCESS_VM_READ,
			false,
			uint32(id), //nolint:gosec // a Windows pid is a DWORD.
		)
		if openErr != nil {
			continue // the process ended after the list was taken.
		}

		counters := processMemoryCounters{cb: uint32(unsafe.Sizeof(processMemoryCounters{}))}
		ok, _, callErr := getInfo.Call(uintptr(process), uintptr(unsafe.Pointer(&counters)), uintptr(counters.cb))
		_ = windows.CloseHandle(process) // only read from.

		if ok == 0 {
			return 0, fmt.Errorf("procgroup: memory of process %d: %w", id, callErr)
		}

		total += int64(counters.privateUsage) //nolint:gosec // no process commits 8 EiB.
	}

	return total, nil
}

func (g *Group) members() (*processIDList, error) {
	var list processIDList

	if err := windows.QueryInformationJobObject(g.job, windows.JobObjectBasicProcessIdList,
		uintptr(unsafe.Pointer(&list)), uint32(unsafe.Sizeof(list)), nil); err != nil {
		return nil, fmt.Errorf("procgroup: list job members: %w", err)
	}

	return &list, nil
}

// Close releases the job handle, which kills what is left in the job.
func (g *Group) Close() error {
	if err := windows.CloseHandle(g.job); err != nil {
		return fmt.Errorf("procgroup: close job: %w", err)
	}

	return nil
}
