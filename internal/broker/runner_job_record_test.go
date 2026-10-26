package broker

import (
	"context"
	"encoding/json"
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
