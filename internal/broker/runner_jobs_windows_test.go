//go:build windows

package broker

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
)

func TestRunnerWindowsMissingJobFallsBackToPIDIdentity(t *testing.T) {
	b, _ := setup(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.Command(os.Args[0], "-test.run=^TestRunnerNativeProcessHelper$")
	cmd.Env = append(os.Environ(), "LUGARN_RUNNER_PROCESS_HELPER=1", "LUGARN_COMMS_HOME="+b.opts.StateDir)
	stdin, e := cmd.StdinPipe()
	must(t, e)
	stdout, e := cmd.StdoutPipe()
	must(t, e)
	must(t, cmd.Start())
	defer func() { _ = stdin.Close(); _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	line, e := bufio.NewReader(stdout).ReadString('\n')
	must(t, e)
	if line != "ready\n" {
		t.Fatal(line)
	}
	launch, e := model.NewULID()
	must(t, e)
	id, e := model.NewULID()
	must(t, e)
	turn := model.Turn{ID: id, Launch: launch, PID: ptr(int64(cmd.Process.Pid))}
	// A fresh, persisted name that was never created: OpenJobObject must fail
	// with FILE_NOT_FOUND, and the PID-plus-birth fallback must still fence reuse.
	name := `Local\lugarn-comms-` + launch + "-0123456789abcdef0123456789abcdef"
	r := NewRunner(b, "t", RunnerOptions{})
	must(t, r.recordProcess(ctx, turn, *turn.PID, name))
	record, e := r.processRecord(turn)
	must(t, e)
	wrong := record
	wrong.Birth += "-wrong"
	raw, e := json.Marshal(wrong)
	must(t, e)
	must(t, os.WriteFile(r.processPath(turn), raw, 0600))
	if e = r.cancelTurnProcess(ctx, turn); e == nil {
		t.Fatal("missing-job fallback accepted a wrong birth")
	}
	live, e := r.opts.Processes.Inspect(ctx, *turn.PID)
	must(t, e)
	if !live.Alive {
		t.Fatal("killed wrong-identity process")
	}
	must(t, r.recordProcess(ctx, turn, *turn.PID, name))
	must(t, r.cancelTurnProcess(ctx, turn))
	_ = cmd.Wait()
	state, e := r.opts.Processes.Inspect(ctx, *turn.PID)
	must(t, e)
	if state.Alive {
		t.Fatal("PID fallback left the owned process running")
	}
	if filepath.Dir(r.processPath(turn)) != filepath.Join(b.opts.StateDir, "launch", launch) {
		t.Fatal("metadata outside isolated state")
	}
}
