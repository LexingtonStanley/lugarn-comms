//go:build windows

package herdr

import (
	"os"
	"slices"
	"strings"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const wmiHelperArg = "lugarn-herdr-wmi-helper"

// TestWMIHelperSleep is the long-running process the WMI test creates. WMI
// gives it the user's environment, not the test's, so it's chosen by argv.
func TestWMIHelperSleep(t *testing.T) {
	if !slices.Contains(os.Args, wmiHelperArg) {
		t.Skip("helper process only")
	}
	time.Sleep(2 * time.Minute)
}

// A process created through WMI is alive after we return, and isn't our
// child: it belongs to the WMI service, outside the caller's job object.
func TestWMICreateOutlivesCaller(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	pid, err := wmiCreate(testContext(t), []string{exe, "-test.run=^TestWMIHelperSleep$", wmiHelperArg}, os.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { killProcess(pid) })
	time.Sleep(2 * time.Second)
	if !processAlive(pid) {
		t.Fatalf("helper %d isn't running", pid)
	}
	parent, parentExe, err := parentOf(uint32(pid))
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("helper %d, parent %d (%s), test %d", pid, parent, parentExe, os.Getpid())
	if int(parent) == os.Getpid() {
		t.Fatal("the WMI-created process is the test's child")
	}
	if !strings.EqualFold(parentExe, "WmiPrvSE.exe") {
		t.Logf("parent isn't WmiPrvSE.exe: %s", parentExe)
	}
}

func parentOf(pid uint32) (uint32, string, error) {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return 0, "", err
	}
	defer windows.CloseHandle(snap)
	entries := map[uint32]windows.ProcessEntry32{}
	var e windows.ProcessEntry32
	e.Size = uint32(unsafe.Sizeof(e))
	for err = windows.Process32First(snap, &e); err == nil; err = windows.Process32Next(snap, &e) {
		entries[e.ProcessID] = e
	}
	self, ok := entries[pid]
	if !ok {
		return 0, "", os.ErrNotExist
	}
	parentExe := ""
	if p, ok := entries[self.ParentProcessID]; ok {
		parentExe = windows.UTF16ToString(p.ExeFile[:])
	}
	return self.ParentProcessID, parentExe, nil
}

func TestEncodePowerShell(t *testing.T) {
	// "a€" in UTF-16LE is 61 00 AC 20.
	if got := encodePowerShell("a€"); got != "YQCsIA==" {
		t.Fatalf("encodePowerShell = %q", got)
	}
}
