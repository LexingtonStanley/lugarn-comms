package harness

import (
	"os/exec"
	"unsafe"

	"golang.org/x/sys/windows"
)

type processGroup struct{ job windows.Handle }

func newProcessGroup(cmd *exec.Cmd) (*processGroup, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, err
	}
	limits := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	limits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	_, err = windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits)))
	if err != nil {
		windows.CloseHandle(job)
		return nil, err
	}
	return &processGroup{job: job}, nil
}
func (g *processGroup) assign(cmd *exec.Cmd) error {
	// Go starts the process before exposing its handle. A child could spawn in the
	// short interval before assignment and escape the job. Assign immediately;
	// eliminating that window requires suspended creation and thread resumption,
	// which os/exec does not expose. Assignment failure kills and reaps the parent.
	process, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(cmd.Process.Pid))
	if err != nil {
		return err
	}
	defer windows.CloseHandle(process)
	return windows.AssignProcessToJobObject(g.job, process)
}
func (g *processGroup) kill() error {
	if g.job == 0 {
		return nil
	}
	return windows.TerminateJobObject(g.job, 1)
}
func (g *processGroup) close() {
	if g.job != 0 {
		_ = windows.CloseHandle(g.job)
		g.job = 0
	}
}
