//go:build windows

package broker

import (
	"context"
	"fmt"
	"unsafe"

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
	// C2c normally cancels the job. For an orphan, retain process handles while
	// walking the snapshot, and terminate descendants before the verified root.
	return p.killTree(ctx, uint32(pid), map[uint32]bool{})
}
func (p nativeProcesses) killTree(ctx context.Context, pid uint32, seen map[uint32]bool) error {
	if seen[pid] {
		return nil
	}
	seen[pid] = true
	h, e := windows.OpenProcess(windows.PROCESS_TERMINATE|windows.SYNCHRONIZE, false, pid)
	if e == windows.ERROR_INVALID_PARAMETER {
		return nil
	}
	if e != nil {
		return e
	}
	defer windows.CloseHandle(h)
	snap, e := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if e != nil {
		return e
	}
	defer windows.CloseHandle(snap)
	entry := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
	e = windows.Process32First(snap, &entry)
	for e == nil {
		if entry.ParentProcessID == pid {
			if e := p.killTree(ctx, entry.ProcessID, seen); e != nil {
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
	return windows.TerminateProcess(h, 1)
}
