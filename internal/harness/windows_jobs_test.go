//go:build windows

package harness

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// These helpers are compiled into the test binary. They need neither a harness
// installation nor fixture files, including when go test -c is copied to Windows.
func init() {
	platformProcessHelpers["windows-job-parent"] = windowsJobParent
	platformProcessHelpers["windows-job-holder"] = windowsJobHolder
	platformProcessHelpers["windows-job-leaf"] = windowsJobLeaf
	platformProcessHelpers["windows-job-terminate"] = func() int {
		found, e := TerminateJob(os.Getenv("LUGARN_TEST_JOB_NAME"))
		if e != nil || !found {
			fmt.Fprintf(os.Stderr, "recorded job termination: found=%v err=%v", found, e)
			return 1
		}
		return 0
	}
}

type windowsJobState struct {
	Name             string
	PID, LeafPID     int
	Birth, LeafBirth string
	QueryOnly        bool
}

func jobTestCommand(mode, root string) *exec.Cmd {
	cmd := exec.Command(os.Args[0])
	cmd.Env = mergeEnv(os.Environ(), map[string]string{"LUGARN_HARNESS_HELPER": mode, "LUGARN_HARNESS_HELPER_DIR": root, "LUGARN_COMMS_HOME": root}, false)
	return cmd
}
func waitJobState(path string) (windowsJobState, error) {
	var last error
	for until := time.Now().Add(15 * time.Second); time.Now().Before(until); {
		raw, e := os.ReadFile(path)
		if e == nil {
			var state windowsJobState
			if e = json.Unmarshal(raw, &state); e == nil && state.PID > 0 {
				return state, nil
			}
		}
		last = e
		time.Sleep(10 * time.Millisecond)
	}
	return windowsJobState{}, fmt.Errorf("waiting for helper state: %v", last)
}
func jobProcessBirth(pid int) (string, error) {
	h, e := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if e != nil {
		return "", e
	}
	defer windows.CloseHandle(h)
	var c, x, k, u windows.Filetime
	if e = windows.GetProcessTimes(h, &c, &x, &k, &u); e != nil {
		return "", e
	}
	return fmt.Sprintf("%d:%d", c.HighDateTime, c.LowDateTime), nil
}
func windowsJobParent() int {
	root := os.Getenv("LUGARN_HARNESS_HELPER_DIR")
	cmd := jobTestCommand("windows-job-holder", root)
	group, e := newProcessGroup(cmd, testLaunchID)
	if e != nil {
		fmt.Fprintln(os.Stderr, e)
		return 1
	}
	defer group.close()
	success := false
	defer func() {
		if !success {
			_ = group.kill()
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
				_ = cmd.Wait()
			}
		}
	}()
	cmd.Env = append(cmd.Env, "LUGARN_TEST_JOB_QUERY_HANDLE="+strconv.FormatUint(uint64(group.inherited), 10))
	if e = cmd.Start(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		return 1
	}
	group.started()
	if e = group.assign(cmd); e != nil {
		fmt.Fprintln(os.Stderr, e)
		return 1
	}
	if e = os.WriteFile(filepath.Join(root, "assigned"), nil, 0600); e != nil {
		fmt.Fprintln(os.Stderr, e)
		return 1
	}
	state, e := waitJobState(filepath.Join(root, "holder.json"))
	if e != nil {
		fmt.Fprintln(os.Stderr, e)
		return 1
	}
	state.Name = group.name
	raw, e := json.Marshal(state)
	if e == nil {
		e = os.WriteFile(filepath.Join(root, "parent.json"), raw, 0600)
	}
	if e != nil {
		fmt.Fprintln(os.Stderr, e)
		return 1
	}
	// Simulate executable exit, not normal turn completion. Closing only the
	// creator's full-access handle must leave the holder and leaf running.
	success = true
	return 0
}
func windowsJobHolder() int {
	root := os.Getenv("LUGARN_HARNESS_HELPER_DIR")
	for until := time.Now().Add(15 * time.Second); ; {
		if _, e := os.Stat(filepath.Join(root, "assigned")); e == nil {
			break
		}
		if time.Now().After(until) {
			return 1
		}
		time.Sleep(10 * time.Millisecond)
	}
	raw, e := strconv.ParseUint(os.Getenv("LUGARN_TEST_JOB_QUERY_HANDLE"), 10, 64)
	if e != nil {
		return 1
	}
	query := windows.Handle(raw)
	defer windows.CloseHandle(query)
	var limits windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	if e = windows.QueryInformationJobObject(query, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits)), nil); e != nil {
		return 2
	}
	if limits.BasicLimitInformation.LimitFlags&windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE != 0 {
		return 3
	}
	if e = windows.TerminateJobObject(query, 1); !errors.Is(e, windows.ERROR_ACCESS_DENIED) {
		return 4
	}
	leaf := jobTestCommand("windows-job-leaf", root)
	if e = leaf.Start(); e != nil {
		return 5
	}
	state := windowsJobState{PID: os.Getpid(), LeafPID: leaf.Process.Pid, QueryOnly: true}
	state.Birth, e = jobProcessBirth(state.PID)
	if e != nil {
		return 6
	}
	state.LeafBirth, e = jobProcessBirth(state.LeafPID)
	if e != nil {
		return 7
	}
	data, e := json.Marshal(state)
	if e == nil {
		e = os.WriteFile(filepath.Join(root, "holder.json"), data, 0600)
	}
	if e != nil {
		return 8
	}
	for until := time.Now().Add(time.Minute); time.Now().Before(until); {
		if _, e = os.Stat(filepath.Join(root, "exit-root")); e == nil {
			return 0
		}
		time.Sleep(10 * time.Millisecond)
	}
	return 0
}
func windowsJobLeaf() int {
	root := os.Getenv("LUGARN_HARNESS_HELPER_DIR")
	for until := time.Now().Add(time.Minute); time.Now().Before(until); {
		_ = os.WriteFile(filepath.Join(root, "leaf-heartbeat"), []byte(strconv.FormatInt(time.Now().UnixNano(), 10)), 0600)
		time.Sleep(20 * time.Millisecond)
	}
	return 0
}
func cleanupJobTestProcess(pid int, birth string) {
	h, e := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.PROCESS_TERMINATE, false, uint32(pid))
	if e != nil {
		return
	}
	defer windows.CloseHandle(h)
	var c, x, k, u windows.Filetime
	if e = windows.GetProcessTimes(h, &c, &x, &k, &u); e == nil && birth == fmt.Sprintf("%d:%d", c.HighDateTime, c.LowDateTime) {
		_ = windows.TerminateProcess(h, 1)
	}
}
func TestWindowsJobSurvivesCreatorAndTerminatesFromAnotherProcess(t *testing.T) {
	root := t.TempDir()
	parent := jobTestCommand("windows-job-parent", root)
	if out, e := parent.CombinedOutput(); e != nil {
		t.Fatalf("creator: %v %s", e, out)
	}
	state, e := waitJobState(filepath.Join(root, "parent.json"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		_, _ = TerminateJob(state.Name)
		cleanupJobTestProcess(state.PID, state.Birth)
		cleanupJobTestProcess(state.LeafPID, state.LeafBirth)
	})
	if !state.QueryOnly || processDead(state.PID) || processDead(state.LeafPID) {
		t.Fatal("job members did not outlive their creator", state)
	}
	// No job handle was opened in this test process before creator exit, so this
	// second process can succeed only if the inherited query handle retained it.
	terminator := jobTestCommand("windows-job-terminate", root)
	terminator.Env = append(terminator.Env, "LUGARN_TEST_JOB_NAME="+state.Name)
	if out, e := terminator.CombinedOutput(); e != nil {
		t.Fatalf("second process: %v %s", e, out)
	}
	assertDead(t, state.PID)
	assertDead(t, state.LeafPID)
}
func TestWindowsJobRejectsExistingName(t *testing.T) {
	first, e := newProcessGroup(exec.Command(os.Args[0]), testLaunchID)
	if e != nil {
		t.Fatal(e)
	}
	defer first.close()
	second, e := newNamedProcessGroup(exec.Command(os.Args[0]), first.name)
	if second != nil || !errors.Is(e, windows.ERROR_ALREADY_EXISTS) {
		if second != nil {
			second.close()
		}
		t.Fatal("joined existing job", e)
	}
	var limits windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	if e = windows.QueryInformationJobObject(first.job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits)), nil); e != nil {
		t.Fatal("collision closed the original handle", e)
	}
}
func TestWindowsJobNormalCompletionTerminatesRemainingMembers(t *testing.T) {
	root := t.TempDir()
	cmd := jobTestCommand("windows-job-holder", root)
	group, e := newProcessGroup(cmd, testLaunchID)
	if e != nil {
		t.Fatal(e)
	}
	defer group.close()
	defer group.kill()
	cmd.Env = append(cmd.Env, "LUGARN_TEST_JOB_QUERY_HANDLE="+strconv.FormatUint(uint64(group.inherited), 10))
	if e = cmd.Start(); e != nil {
		t.Fatal(e)
	}
	group.started()
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	if group.inherited != 0 {
		t.Fatal("parent duplicate was not closed")
	}
	if e = group.assign(cmd); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(root, "assigned"), nil, 0600); e != nil {
		t.Fatal(e)
	}
	state, e := waitJobState(filepath.Join(root, "holder.json"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { cleanupJobTestProcess(state.LeafPID, state.LeafBirth) })
	if e = os.WriteFile(filepath.Join(root, "exit-root"), nil, 0600); e != nil {
		t.Fatal(e)
	}
	if e = cmd.Wait(); e != nil {
		t.Fatal(e)
	}
	if processDead(state.LeafPID) {
		t.Fatal("leaf did not survive normal root exit long enough to test cleanup")
	}
	if e = group.finish(); e != nil {
		t.Fatal(e)
	}
	assertDead(t, state.LeafPID)
}
