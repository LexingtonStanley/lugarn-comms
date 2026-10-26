package broker

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
	"github.com/LexingtonStanley/lugarn-comms/internal/statedir"
)

type nativeProcesses struct{}
type runnerProcessRecord struct {
	PID     int64  `json:"pid"`
	Birth   string `json:"birth"`
	JobName string `json:"job_name,omitempty"`
}

func (r *Runner) processPath(turn model.Turn) string {
	return filepath.Join(r.b.opts.StateDir, "launch", turn.Launch, turn.ID+".process.json")
}
func (r *Runner) recordProcess(ctx context.Context, turn model.Turn, pid int64, jobNames ...string) error {
	state, e := r.opts.Processes.Inspect(ctx, pid)
	if e != nil {
		return e
	}
	record := runnerProcessRecord{PID: pid, Birth: state.Birth}
	if len(jobNames) > 0 {
		record.JobName = jobNames[0]
	}
	if e = validateRecordedJob(record.JobName, turn.Launch); e != nil {
		return e
	}
	data, e := json.Marshal(record)
	if e != nil {
		return e
	}
	path := r.processPath(turn)
	if e = os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return e
	}
	return statedir.WriteSecret(path, data)
}
func validateRecordedJob(name, launch string) error {
	if name == "" {
		return nil
	}
	prefix := `Local\lugarn-comms-` + launch + "-"
	suffix, ok := strings.CutPrefix(name, prefix)
	if !ok || len(suffix) != 32 {
		return model.NewError(model.ErrorInternal, "recorded job does not belong to the turn's launch")
	}
	if _, e := hex.DecodeString(suffix); e != nil {
		return model.NewError(model.ErrorInternal, "invalid recorded job suffix")
	}
	return nil
}
func (r *Runner) processRecord(turn model.Turn) (runnerProcessRecord, error) {
	var record runnerProcessRecord
	data, e := os.ReadFile(r.processPath(turn))
	if os.IsNotExist(e) {
		return record, nil
	}
	if e != nil {
		return record, e
	}
	if e = json.Unmarshal(data, &record); e != nil {
		return runnerProcessRecord{}, e
	}
	if turn.PID == nil || record.PID != *turn.PID {
		return runnerProcessRecord{}, model.NewError(model.ErrorInternal, "process identity PID does not match turn")
	}
	if e = validateRecordedJob(record.JobName, turn.Launch); e != nil {
		return runnerProcessRecord{}, e
	}
	return record, nil
}
func (r *Runner) processBirth(turn model.Turn) string {
	record, e := r.processRecord(turn)
	if e != nil {
		return ""
	}
	return record.Birth
}

// Named jobs are an optional Windows capability. Fake/POSIX Processes need only
// implement the original PID/birth contract. Never synthesize a name at recovery.
type recordedJobs interface {
	TerminateJob(context.Context, string) (bool, error)
}

func (r *Runner) terminateTurnJob(ctx context.Context, turn model.Turn) (bool, error) {
	record, e := r.processRecord(turn)
	if e != nil {
		return false, e
	}
	return r.terminateRecordedJob(ctx, record)
}
func (r *Runner) terminateRecordedJob(ctx context.Context, record runnerProcessRecord) (bool, error) {
	if record.JobName == "" {
		return false, nil
	}
	if jobs, ok := r.opts.Processes.(recordedJobs); ok {
		return jobs.TerminateJob(ctx, record.JobName)
	}
	return false, nil
}
func (r *Runner) cancelTurnProcess(ctx context.Context, turn model.Turn) error {
	record, e := r.processRecord(turn)
	if e != nil {
		return e
	}
	if found, e := r.terminateRecordedJob(ctx, record); e != nil || found {
		return e
	}
	if turn.PID == nil {
		return nil
	}
	if record.Birth == "" {
		state, e := r.opts.Processes.Inspect(ctx, *turn.PID)
		if e != nil {
			return e
		}
		if !state.Alive {
			return nil
		}
		return model.NewError(model.ErrorInternal, "cannot terminate an unverified turn process").WithDetail("turn", turn.ID)
	}
	return r.opts.Processes.Cancel(ctx, *turn.PID, record.Birth)
}
