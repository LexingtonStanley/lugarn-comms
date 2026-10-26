package broker

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
)

func TestRunnerRecordsWindowsJobIdentity(t *testing.T) {
	b, _ := setup(t)
	r := NewRunner(b, "t", RunnerOptions{Processes: &fakeProcesses{alive: true, birth: "fake"}})
	turn, _ := installRunnerTurn(t, b)
	job := `Local\lugarn-comms-` + turn.Launch + "-0123456789abcdef0123456789abcdef"
	must(t, r.recordProcess(context.Background(), turn, *turn.PID, job))
	raw, e := os.ReadFile(r.processPath(turn))
	must(t, e)
	var record runnerProcessRecord
	must(t, json.Unmarshal(raw, &record))
	if record.JobName != job || record.PID != *turn.PID || record.Birth == "" {
		t.Fatal(record)
	}
	must(t, r.recordProcess(context.Background(), turn, *turn.PID))
	raw, e = os.ReadFile(r.processPath(turn))
	must(t, e)
	// Old metadata omits job_name; a fresh decode retains the fallback contract.
	record = runnerProcessRecord{}
	must(t, json.Unmarshal(raw, &record))
	if record.JobName != "" {
		t.Fatal(record)
	}
}

type namedJobProcesses struct {
	fakeProcesses
	names []string
	found bool
	err   error
}

func (p *namedJobProcesses) TerminateJob(_ context.Context, name string) (bool, error) {
	p.names = append(p.names, name)
	return p.found, p.err
}
func TestRunnerCancellationUsesOnlyRecordedJob(t *testing.T) {
	for _, kind := range []string{"named", "missing", "unrecorded", "denied", "wrong-launch"} {
		t.Run(kind, func(t *testing.T) {
			b, _ := setup(t)
			turn, _ := installRunnerTurn(t, b)
			p := &namedJobProcesses{fakeProcesses: fakeProcesses{alive: true, birth: "original"}, found: kind == "named"}
			if kind == "denied" {
				p.err = errors.New("job access denied")
			}
			r := NewRunner(b, "t", RunnerOptions{Processes: p})
			name := `Local\lugarn-comms-` + turn.Launch + "-0123456789abcdef0123456789abcdef"
			if kind == "unrecorded" {
				name = ""
			}
			must(t, r.recordProcess(context.Background(), turn, *turn.PID, name))
			if kind == "wrong-launch" {
				raw, e := json.Marshal(runnerProcessRecord{PID: *turn.PID, Birth: "original", JobName: `Local\lugarn-comms-other-0123456789abcdef0123456789abcdef`})
				must(t, e)
				must(t, os.WriteFile(r.processPath(turn), raw, 0600))
			}
			e := r.cancelTurnProcess(context.Background(), turn)
			if kind == "denied" || kind == "wrong-launch" {
				if e == nil || p.cancelled != 0 {
					t.Fatal(e, p.cancelled)
				}
			} else {
				must(t, e)
			}
			wantCalls := 1
			if kind == "unrecorded" || kind == "wrong-launch" {
				wantCalls = 0
			}
			if len(p.names) != wantCalls {
				t.Fatal("opened an unrecorded job", p.names)
			}
			if len(p.names) > 0 && p.names[0] != name {
				t.Fatal(p.names, name)
			}
			wantPID := 0
			if kind == "missing" || kind == "unrecorded" {
				wantPID = 1
			}
			if p.cancelled != wantPID {
				t.Fatal("incorrect PID fallback", p.cancelled, wantPID)
			}
		})
	}
}
func TestRunnerReconcileCleansJobAfterRootExit(t *testing.T) {
	b, _ := setup(t)
	turn, _ := installRunnerTurn(t, b)
	p := &namedJobProcesses{fakeProcesses: fakeProcesses{alive: false, birth: "original"}, found: true}
	r := NewRunner(b, "t", RunnerOptions{Processes: p})
	name := `Local\lugarn-comms-` + turn.Launch + "-0123456789abcdef0123456789abcdef"
	must(t, r.recordProcess(context.Background(), turn, *turn.PID, name))
	runnerReady(t, b)
	must(t, r.acquire(context.Background()))
	must(t, r.reconcile(context.Background()))
	if len(p.names) != 1 || p.names[0] != name || p.cancelled != 0 {
		t.Fatal("missed remaining job or signalled dead root", p.names, p.cancelled)
	}
}
