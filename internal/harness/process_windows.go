package harness

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os/exec"
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const jobObjectQuery = 0x4
const jobObjectTerminate = 0x8

var jobKernel32 = windows.NewLazySystemDLL("kernel32.dll")
var createJobObjectW = jobKernel32.NewProc("CreateJobObjectW")

type processGroup struct {
	job       windows.Handle
	inherited windows.Handle
	name      string
}

func newProcessGroup(cmd *exec.Cmd, launchID string) (*processGroup, error) {
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return nil, err
	}
	return newNamedProcessGroup(cmd, `Local\lugarn-comms-`+launchID+"-"+hex.EncodeToString(random))
}

func newNamedProcessGroup(cmd *exec.Cmd, name string) (*processGroup, error) {
	encoded, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return nil, err
	}
	// x/sys's CreateJobObject discards LastError when the handle is nonzero.
	// A successful open of an existing job must instead fail, never join it.
	raw, _, lastErr := createJobObjectW.Call(0, uintptr(unsafe.Pointer(encoded)))
	runtime.KeepAlive(encoded)
	job := windows.Handle(raw)
	if job == 0 {
		return nil, fmt.Errorf("CreateJobObjectW: %w", lastErr)
	}
	if lastErr == windows.ERROR_ALREADY_EXISTS {
		_ = windows.CloseHandle(job)
		return nil, fmt.Errorf("CreateJobObjectW: %w", windows.ERROR_ALREADY_EXISTS)
	}
	g := &processGroup{job: job, name: name}
	fail := func(err error) (*processGroup, error) { g.close(); return nil, err }
	// No kill-on-close: a runner exiting must not end a detached turn. The
	// query-only duplicate keeps this name alive in the harness after that exit.
	if err = windows.DuplicateHandle(windows.CurrentProcess(), job, windows.CurrentProcess(), &g.inherited, jobObjectQuery, true, 0); err != nil {
		return fail(err)
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	if cmd.SysProcAttr.NoInheritHandles || cmd.SysProcAttr.ParentProcess != 0 {
		return fail(fmt.Errorf("job inheritance requires this process as parent and handle inheritance enabled"))
	}
	cmd.SysProcAttr.AdditionalInheritedHandles = append(cmd.SysProcAttr.AdditionalInheritedHandles, syscall.Handle(g.inherited))
	return g, nil
}
func (g *processGroup) jobName() string { return g.name }

// started closes the parent's duplicate after Start has copied the handle into
// the child. The original job handle remains private to the parent.
func (g *processGroup) started() {
	if g.inherited != 0 {
		_ = windows.CloseHandle(g.inherited)
		g.inherited = 0
	}
}
func (g *processGroup) assign(cmd *exec.Cmd) error {
	// os/exec exposes the process only after Start. A child can spawn before this
	// assignment and escape job membership; inheriting a handle alone does not
	// assign it. Eliminating that window requires suspended creation and thread
	// resumption, which os/exec does not expose. Failure kills/reaps the root, but
	// cannot guarantee capture of descendants that escaped before assignment.
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
	g.started()
	if g.job != 0 {
		_ = windows.CloseHandle(g.job)
		g.job = 0
	}
}
