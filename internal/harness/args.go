package harness

import (
	"encoding/json"
	"strings"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
)

var commsTools = []string{"send", "reply", "inbox", "team", "spawn", "stop"}

// Args builds fixed harness arguments for a headless turn or pane. It never
// accepts a prompt. Pane callers pass these after Herdr's -- separator.
func Args(l Launch, mode model.Mode) ([]string, error) {
	if err := l.Validate(); err != nil {
		return nil, err
	}
	if !mode.Valid() {
		return nil, model.NewError(model.ErrorInvalidRequest, "invalid launch mode")
	}
	headless := mode == model.ModeHeadless
	if headless && l.Harness == model.HarnessPi {
		return nil, model.NewError(model.ErrorHarnessUnavailable, "pi headless is unavailable in v1")
	}
	if !headless && (l.Harness == model.HarnessCodex || l.Harness == model.HarnessOpenCode) && !l.Resume {
		return nil, model.NewError(model.ErrorInvalidRequest, "pane requires a reported session from a headless turn")
	}
	var a []string
	if l.Model != "" && l.Harness != model.HarnessOpenCode {
		a = append(a, "--model", l.Model)
	}
	switch l.Harness {
	case model.HarnessClaude:
		if headless {
			a = append(a, "-p", "--output-format", "stream-json", "--verbose")
		}
		flag := "--session-id"
		if l.Resume {
			flag = "--resume"
		}
		a = append(a, flag, l.SessionID)
		a = append(a, "--mcp-config="+l.path(".json"))
		tools := make([]string, 0, 9)
		for _, name := range commsTools {
			tools = append(tools, "mcp__lugarn__"+name)
		}
		permission := "plan"
		if l.Profile == model.ProfileWorkspace {
			permission = "acceptEdits"
			tools = append(tools, "Edit", "Write", "Bash")
		}
		if l.Profile == model.ProfileBypass {
			permission = "bypassPermissions"
		}
		a = append(a, "--permission-mode", permission, "--allowedTools="+strings.Join(tools, ","))
		if !headless {
			a = append(a, "--settings", l.path(".settings.json"))
		}
	case model.HarnessCodex:
		if headless {
			a = append(a, "--no-daemon")
		}
		if l.Profile == model.ProfileBypass {
			a = append(a, "--dangerously-bypass-approvals-and-sandbox")
		} else {
			sandbox := "read-only"
			if l.Profile == model.ProfileWorkspace {
				sandbox = "workspace-write"
			}
			a = append(a, "-s", sandbox)
		}
		config := func(key string, v any) { b, _ := json.Marshal(v); a = append(a, "-c", key+"="+string(b)) }
		config("mcp_servers.lugarn.command", l.Binary)
		config("mcp_servers.lugarn.args", l.mcpArgs())
		for _, name := range commsTools {
			config("mcp_servers.lugarn.tools."+name+".approval_mode", "approve")
		}
		if headless {
			a = append(a, "exec", "--json")
			if l.Resume {
				a = append(a, "resume", l.SessionID)
			}
			a = append(a, "--skip-git-repo-check", "-")
		} else {
			a = append(a, "resume", l.SessionID)
		}
	case model.HarnessOpenCode:
		if headless {
			a = append(a, "run", "--format", "json")
		}
		a = append(a, "--standalone")
		if l.Model != "" {
			a = append(a, "--model", l.Model)
		}
		if l.Resume {
			a = append(a, "--session", l.SessionID)
		}
		if l.Profile != model.ProfileReadOnly {
			a = append(a, "--auto")
		}
	case model.HarnessPi: // No MCP; the brief uses the CLI, identity is in the env.
	}
	return a, nil
}

// Environment returns launch overrides, to merge over the parent's environment.
// An inherited OPENCODE_CONFIG_CONTENT must be removed before applying these.
func Environment(l Launch) (map[string]string, error) {
	if err := l.Validate(); err != nil {
		return nil, err
	}
	env := map[string]string{"LUGARN_COMMS_MEMBER": l.MemberID, "LUGARN_COMMS_TOKEN_FILE": l.TokenPath(), "PWD": l.WorkDir, "NO_COLOR": "1"}
	for k, v := range l.ExtraEnv {
		env[k] = v
	}
	if l.Harness == model.HarnessOpenCode {
		env["OPENCODE_CONFIG"] = l.path(".opencode.json")
		env["OPENCODE_DISABLE_AUTOUPDATE"] = "1"
	}
	return env, nil
}
