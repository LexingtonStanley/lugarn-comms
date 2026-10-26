package harness

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
	"github.com/LexingtonStanley/lugarn-comms/internal/statedir"
)

func (l Launch) mcpArgs() []string {
	return []string{"mcp", "--member", l.MemberID, "--token-file", l.TokenPath()}
}

// OwnedPaths lists the token, generated configuration, delivery directory, and
// Claude pointer belonging to this launch. The delivery directory is recursive;
// no other path is. home is explicit so cleanup never guesses the person's home.
func (l Launch) OwnedPaths(home string) ([]string, error) {
	if err := l.Validate(); err != nil {
		return nil, err
	}
	if !filepath.IsAbs(home) {
		return nil, model.NewError(model.ErrorInvalidRequest, "home must be absolute")
	}
	paths := []string{l.TokenPath(), l.path("")}
	switch l.Harness {
	case model.HarnessClaude:
		paths = append(paths, l.path(".json"), l.path(".settings.json"), filepath.Join(home, ".lugarn", "conversations", l.LaunchID))
	case model.HarnessOpenCode:
		paths = append(paths, l.path(".opencode.json"))
	}
	return paths, nil
}

// WriteFiles writes launch-only configuration with private permissions. It never
// writes the token or pointer, and never reads or edits global harness settings.
func WriteFiles(l Launch) error { return writeFiles(l, runtime.GOOS) }
func writeFiles(l Launch, platform string) error {
	if err := l.Validate(); err != nil {
		return err
	}
	if l.Harness != model.HarnessClaude && l.Harness != model.HarnessOpenCode {
		return nil
	}
	if err := statedir.Ensure(l.StateDir); err != nil {
		return err
	}
	write := func(path string, v any) error {
		b, err := json.Marshal(v)
		if err != nil {
			return err
		}
		return statedir.WriteSecret(path, append(b, '\n'))
	}
	switch l.Harness {
	case model.HarnessClaude:
		config := map[string]any{"mcpServers": map[string]any{"lugarn": map[string]any{"command": l.Binary, "args": l.mcpArgs()}}}
		if err := write(l.path(".json"), config); err != nil {
			return err
		}
		settings, err := ClaudeHookSettings(l.LaunchID, platform)
		if err != nil {
			return err
		}
		return statedir.WriteSecret(l.path(".settings.json"), settings)
	case model.HarnessOpenCode:
		permission := map[string]string{"edit": "deny", "bash": "deny"}
		if l.Profile != model.ProfileReadOnly {
			permission = map[string]string{"edit": "allow", "bash": "allow", "external_directory": "deny"}
			if l.Profile == model.ProfileBypass {
				permission["external_directory"] = "allow"
			}
		}
		major, err := openCodeMajor(l.Version)
		if err != nil {
			return err
		}
		command := append([]string{l.Binary}, l.mcpArgs()...)
		// 1.x reads mcp as a map of server names to flat entries, and 1.0's
		// entry is strict (zod .strict()): it has no Code Mode, so no
		// codemode key, and a "servers" key would be a server's name.
		legacy := map[string]any{"type": "local", "command": command, "enabled": true}
		mcp := map[string]any{"lugarn": legacy}
		if major == 2 {
			// OpenCode 2.1.4 translates flat entries through its legacy adapter,
			// which drops codemode. Its native servers map preserves that setting
			// and takes precedence for the same server name.
			legacy["codemode"] = false
			mcp["servers"] = map[string]any{"lugarn": map[string]any{
				"type": "local", "command": command, "disabled": false, "codemode": false,
				"timeout": map[string]int{"startup": 30000, "catalog": 30000},
			}}
		}
		return write(l.path(".opencode.json"), map[string]any{"mcp": mcp, "permission": permission})
	}
	return nil
}

// ClaudeHookSettings returns #136's silent SessionStart hook, without pruning.
// It is installed only for pane launches. Merely generating it touches no home.
func ClaudeHookSettings(launchID, platform string) ([]byte, error) {
	if err := model.ValidateULID(launchID); err != nil {
		return nil, err
	}
	command := `sh -c 'mkdir -p "$HOME/.lugarn/conversations" && cat >> "$HOME/.lugarn/conversations/` + launchID + `"'`
	hook := map[string]any{"type": "command", "command": command, "timeout": 10}
	if platform == "windows" {
		hook["shell"] = "powershell"
		hook["command"] = `$d = Join-Path $HOME '.lugarn\conversations'; [void][IO.Directory]::CreateDirectory($d); [IO.File]::AppendAllText((Join-Path $d '` + launchID + `'), [Console]::In.ReadToEnd().Trim() + [char]10)`
	}
	return json.Marshal(map[string]any{"hooks": map[string]any{"SessionStart": []any{map[string]any{"hooks": []any{hook}}}}})
}

// ReadPointer reads the last complete, valid non-subagent SessionStart line.
// A missing pointer returns an empty id; malformed and trailing partial lines
// are ignored. This function never creates a file or directory.
func ReadPointer(home, launchID string) (string, error) {
	if err := model.ValidateULID(launchID); err != nil {
		return "", err
	}
	if !filepath.IsAbs(home) {
		return "", model.NewError(model.ErrorInvalidRequest, "home must be absolute")
	}
	f, err := os.Open(filepath.Join(home, ".lugarn", "conversations", launchID))
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	defer f.Close()
	r := bufio.NewReaderSize(f, 64*1024)
	id := ""
	for {
		line, err := r.ReadSlice('\n')
		if err == io.EOF {
			return id, nil
		}
		if err != nil {
			return "", err
		}
		var record map[string]json.RawMessage
		if json.Unmarshal(line, &record) != nil {
			continue
		}
		if _, sub := record["agent_id"]; sub {
			continue
		}
		var candidate string
		if json.Unmarshal(record["session_id"], &candidate) == nil && validSession(model.HarnessClaude, candidate) {
			id = candidate
		}
	}
}
