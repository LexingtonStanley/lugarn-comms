package broker

import (
	"encoding/json"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/LexingtonStanley/lugarn-comms/internal/harness"
	"github.com/LexingtonStanley/lugarn-comms/internal/model"
	"github.com/LexingtonStanley/lugarn-comms/internal/statedir"
)

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
func (a *harnessAdapter) writePointerHook(l harness.Launch) error {
	if l.Harness != model.HarnessClaude || a.home == "" {
		return nil
	}
	dir := filepath.Join(a.home, ".lugarn", "conversations")
	path := filepath.Join(dir, l.LaunchID)
	script := "umask 077; mkdir -p " + shellQuote(dir) + " && { cat; printf '\\n'; } >> " + shellQuote(path)
	hook := map[string]any{"type": "command", "timeout": 10, "command": "sh -c " + shellQuote(script)}
	if runtime.GOOS == "windows" {
		q := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
		hook["shell"] = "powershell"
		hook["command"] = "[void][IO.Directory]::CreateDirectory(" + q(dir) + "); [IO.File]::AppendAllText(" + q(path) + ", [Console]::In.ReadToEnd().Trim() + [char]10)"
	}
	raw, e := json.Marshal(map[string]any{"hooks": map[string]any{"SessionStart": []any{map[string]any{"hooks": []any{hook}}}}})
	if e != nil {
		return e
	}
	return statedir.WriteSecret(filepath.Join(l.StateDir, "launch", l.LaunchID+".settings.json"), raw)
}
