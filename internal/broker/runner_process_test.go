package broker

import (
	"bufio"
	"context"
	"io"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestRunnerNativeProcessHelper(t *testing.T) {
	if os.Getenv("LUGARN_RUNNER_PROCESS_HELPER") != "1" {
		return
	}
	_, _ = os.Stdout.WriteString("ready\n")
	_, _ = io.Copy(io.Discard, os.Stdin)
	os.Exit(0)
}
func TestRunnerNativeProcessIdentityAndCancellation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestRunnerNativeProcessHelper$")
	cmd.Env = append(os.Environ(), "LUGARN_RUNNER_PROCESS_HELPER=1", "LUGARN_COMMS_HOME="+t.TempDir())
	stdin, e := cmd.StdinPipe()
	must(t, e)
	stdout, e := cmd.StdoutPipe()
	must(t, e)
	must(t, cmd.Start())
	defer func() { stdin.Close(); cmd.Process.Kill(); _ = cmd.Wait() }()
	line, e := bufio.NewReader(stdout).ReadString('\n')
	must(t, e)
	if line != "ready\n" {
		t.Fatal(line)
	}
	p := nativeProcesses{}
	state, e := p.Inspect(ctx, int64(cmd.Process.Pid))
	must(t, e)
	if !state.Alive || state.Birth == "" {
		t.Fatal(state)
	}
	if e = p.Cancel(ctx, int64(cmd.Process.Pid), state.Birth+"wrong"); e == nil {
		t.Fatal("accepted incorrect process birth")
	}
	state, e = p.Inspect(ctx, int64(cmd.Process.Pid))
	must(t, e)
	if !state.Alive {
		t.Fatal("wrong-identity cancellation killed the process")
	}
	must(t, p.Cancel(ctx, int64(cmd.Process.Pid), state.Birth))
	_ = cmd.Wait()
	state, e = p.Inspect(ctx, int64(cmd.Process.Pid))
	must(t, e)
	if state.Alive {
		t.Fatal("owned child survived cancellation")
	}
}
