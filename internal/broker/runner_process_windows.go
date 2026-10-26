//go:build windows

package broker

import (
	"context"
	"errors"
	"fmt"
	"unsafe"

	"github.com/LexingtonStanley/lugarn-comms/internal/harness"
	"golang.org/x/sys/windows"
)

func (nativeProcesses) Inspect(_ context.Context, pid int64) (ProcessState, error) {
	if pid <= 0 {
		return ProcessState{}, nil
	}
	h, e := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.SYNCHRONIZE, false, uint32(pid))
	if e == windows.ERROR_INVALID_PARAMETER {
		return ProcessState{}, nil
	}
	if e != nil {
		return ProcessState{}, e
	}
	defer windows.CloseHandle(h)
	state, e := windows.WaitForSingleObject(h, 0)
	if e != nil {
		return ProcessState{}, e
	}
	if state == windows.WAIT_OBJECT_0 {
		return ProcessState{}, nil
	}
	var created, exited, kernel, user windows.Filetime
	if e = windows.GetProcessTimes(h, &created, &exited, &kernel, &user); e != nil {
		return ProcessState{}, e
	}
	return ProcessState{Alive: true, Birth: fmt.Sprintf("%d:%d", created.HighDateTime, created.LowDateTime)}, nil
}
func (p nativeProcesses) Cancel(ctx context.Context, pid int64, birth string) error {
	s, e := p.Inspect(ctx, pid)
	if e != nil || !s.Alive {
		return e
	}
	if birth == "" || birth != s.Birth {
		return fmt.Errorf("refusing to kill unverified process %d", pid)
	}
	// The recorded job is tried before this fallback. Retain process handles while
	// walking the snapshot, and terminate descendants before the verified root.
	return p.killTree(ctx, uint32(pid), birth, 0, map[uint32]bool{})
}
func (p nativeProcesses) killTree(ctx context.Context, pid uint32, expected string, parentBirth uint64, seen map[uint32]bool) error {
	if seen[pid] {
		return nil
	}
	seen[pid] = true
	h, e := windows.OpenProcess(windows.PROCESS_TERMINATE|windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.SYNCHRONIZE, false, pid)
	if e == windows.ERROR_INVALID_PARAMETER {
		return nil
	}
	if e != nil {
		return e
	}
	defer windows.CloseHandle(h)
	var created, exited, kernel, user windows.Filetime
	if e = windows.GetProcessTimes(h, &created, &exited, &kernel, &user); e != nil {
		return e
	}
	birth := uint64(created.HighDateTime)<<32 | uint64(created.LowDateTime)
	if expected != "" && expected != fmt.Sprintf("%d:%d", created.HighDateTime, created.LowDateTime) {
		return fmt.Errorf("refusing to kill reused process %d", pid)
	}
	if birth < parentBirth {
		return nil
	}
	snap, e := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if e != nil {
		return e
	}
	defer windows.CloseHandle(snap)
	entry := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
	e = windows.Process32First(snap, &entry)
	for e == nil {
		if entry.ParentProcessID == pid {
			if e := p.killTree(ctx, entry.ProcessID, "", birth, seen); e != nil {
				return e
			}
		}
		e = windows.Process32Next(snap, &entry)
	}
	if e != windows.ERROR_NO_MORE_FILES {
		return e
	}
	state, e := windows.WaitForSingleObject(h, 0)
	if e != nil {
		return e
	}
	if state == windows.WAIT_OBJECT_0 {
		return nil
	}
	return terminateWindowsProcess(h, windows.TerminateProcess, windows.WaitForSingleObject)
}

// TerminateJob receives only an exact name read from the turn's private process
// record. Missing names permit the caller's PID/birth fallback; access failures do not.
func (nativeProcesses) TerminateJob(ctx context.Context, name string) (bool, error) {
	if e := ctx.Err(); e != nil {
		return false, e
	}
	return harness.TerminateJob(name)
}

// A process can finish between the zero-time wait and TerminateProcess. Windows
// then returns ACCESS_DENIED even for our verified process. Keep the same handle
// pinned and accept that error only if it signals within this short bounded wait.
func terminateWindowsProcess(handle windows.Handle, terminate func(windows.Handle, uint32) error, wait func(windows.Handle, uint32) (uint32, error)) error {
	err := terminate(handle, 1)
	if !errors.Is(err, windows.ERROR_ACCESS_DENIED) {
		return err
	}
	state, waitErr := wait(handle, 500)
	if waitErr != nil {
		return errors.Join(err, fmt.Errorf("wait for terminating process: %w", waitErr))
	}
	if state == windows.WAIT_OBJECT_0 {
		return nil
	}
	return err
}
