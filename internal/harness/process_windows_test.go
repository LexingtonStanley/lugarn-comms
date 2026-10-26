package harness

import (
	"context"
	"errors"
	"github.com/LexingtonStanley/lugarn-comms/internal/model"
	"golang.org/x/sys/windows"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
	"unsafe"
)

func processDead(pid int) bool {
	h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		return true
	}
	defer windows.CloseHandle(h)
	status, err := windows.WaitForSingleObject(h, 0)
	return err == nil && status == windows.WAIT_OBJECT_0
}
func assertDead(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if processDead(pid) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("process %d remains live", pid)
}

func liveProcessIDs(h *Handle) ([]int, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	var list struct {
		Assigned, List uint32
		PIDs           [128]uintptr
	}
	err := windows.QueryInformationJobObject(h.group.job, windows.JobObjectBasicProcessIdList, uintptr(unsafe.Pointer(&list)), uint32(unsafe.Sizeof(list)), nil)
	if err != nil {
		return nil, err
	}
	var pids []int
	for _, pid := range list.PIDs[:list.List] {
		pids = append(pids, int(pid))
	}
	return pids, nil
}

func detachHelper(cmd *exec.Cmd) {}

func TestNativeExecutableOnly(t *testing.T) {
	d, l, _ := fakeDriver(t, "ok")
	d.Resolver = fakeResolver{path: filepath.Join(t.TempDir(), "codex.cmd")}
	_, err := d.Start(context.Background(), l, "ok")
	var e *model.Error
	if !errors.As(err, &e) || e.Code != model.ErrorHarnessUnavailable || e.Details["reason"] != "shim_only" {
		t.Fatal(err)
	}
}
