package broker

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
	"github.com/LexingtonStanley/lugarn-comms/internal/statedir"
)

type nativeProcesses struct{}
type runnerProcessRecord struct {
	PID   int64  `json:"pid"`
	Birth string `json:"birth"`
}

func (r *Runner) processPath(turn model.Turn) string {
	return filepath.Join(r.b.opts.StateDir, "launch", turn.Launch, turn.ID+".process.json")
}
func (r *Runner) recordProcess(ctx context.Context, turn model.Turn, pid int64) error {
	state, e := r.opts.Processes.Inspect(ctx, pid)
	if e != nil {
		return e
	}
	data, e := json.Marshal(runnerProcessRecord{PID: pid, Birth: state.Birth})
	if e != nil {
		return e
	}
	path := r.processPath(turn)
	if e = os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return e
	}
	return statedir.WriteSecret(path, data)
}
func (r *Runner) processBirth(turn model.Turn) string {
	data, e := os.ReadFile(r.processPath(turn))
	if e != nil {
		return ""
	}
	var p runnerProcessRecord
	if json.Unmarshal(data, &p) != nil || turn.PID == nil || p.PID != *turn.PID {
		return ""
	}
	return p.Birth
}
