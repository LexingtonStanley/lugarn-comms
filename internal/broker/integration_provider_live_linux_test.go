//go:build linux

package broker

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"
)

// This optional baseline deliberately has its own switch so running the requested
// flow matrix cannot silently add another model invocation. It uses the person's
// existing configuration and never opens/copies provider credential files.
func TestIntegrationLiveOpenCodeBaseline(t *testing.T) {
	if os.Getenv("LUGARN_INTEGRATION_PROVIDER_DIAGNOSTIC") != "1" {
		t.Skip("opt in to the standalone free-model diagnostic")
	}
	binary, e := exec.LookPath("opencode")
	if e != nil {
		t.Skip("OpenCode not installed")
	}
	cmd := exec.Command(binary, "run", "--standalone", "--format", "json", "--model", "opencode-go/iceman-contributor-free")
	cmd.Dir = t.TempDir()
	cmd.Stdin = strings.NewReader("Reply with only the word ok. Do not use tools.\n")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	protect := startProtection()
	e = cmd.Start()
	if e != nil {
		protect(0)
		t.Fatal(e)
	}
	protect(cmd.Process.Pid)
	defer releaseProtection(cmd.Process.Pid)
	state, e := (nativeProcesses{}).Inspect(context.Background(), int64(cmd.Process.Pid))
	must(t, e)
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	timedOut := false
	select {
	case <-done:
	case <-time.After(25 * time.Second):
		timedOut = true
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		must(t, cancelOwnedTree(cleanup, int64(cmd.Process.Pid), state.Birth))
		<-done
	}
	t.Logf("standalone OpenCode baseline: timeout=%v exit=%d stderr_bytes=%d", timedOut, cmd.ProcessState.ExitCode(), stderr.Len())
	for _, line := range bytes.Split(stdout.Bytes(), []byte{'\n'}) {
		var event struct {
			Type    string `json:"type"`
			Session string `json:"sessionID"`
		}
		if json.Unmarshal(line, &event) == nil {
			t.Logf("event type=%s session=%s", event.Type, event.Session)
		}
	}
}
