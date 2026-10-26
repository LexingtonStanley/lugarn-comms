package harness

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
)

func TestArgs(t *testing.T) {
	for _, h := range (model.Harness("")).Values() {
		for _, p := range (model.Profile("")).Values() {
			for _, kind := range []string{"first", "later", "pane-new", "pane-resume"} {
				t.Run(string(h)+"/"+string(p)+"/"+kind, func(t *testing.T) {
					l := testLaunch(t, h)
					l.Profile = p
					l.Resume = kind == "later" || kind == "pane-resume"
					if l.Resume {
						switch h {
						case model.HarnessCodex:
							l.SessionID = testSessionID
						case model.HarnessOpenCode:
							l.SessionID = "ses_example123"
						case model.HarnessPi:
							l.Resume = false
						}
					}
					mode := model.ModeHeadless
					if strings.HasPrefix(kind, "pane") {
						mode = model.ModePane
					}
					got, err := Args(l, mode)
					if (h == model.HarnessPi && mode == model.ModeHeadless) || ((h == model.HarnessCodex || h == model.HarnessOpenCode) && kind == "pane-new") {
						if err == nil {
							t.Fatal("expected unsupported launch")
						}
						return
					}
					if err != nil {
						t.Fatal(err)
					}
					var want []string
					switch h {
					case model.HarnessClaude:
						if mode == model.ModeHeadless {
							want = []string{"-p", "--output-format", "stream-json", "--verbose"}
						}
						flag := "--session-id"
						if l.Resume {
							flag = "--resume"
						}
						want = append(want, flag, l.SessionID, "--mcp-config="+l.path(".json"))
						permission := map[model.Profile]string{model.ProfileReadOnly: "plan", model.ProfileWorkspace: "acceptEdits", model.ProfileBypass: "bypassPermissions"}[p]
						allowed := "mcp__lugarn__send,mcp__lugarn__reply,mcp__lugarn__inbox,mcp__lugarn__team,mcp__lugarn__spawn,mcp__lugarn__stop"
						if p == model.ProfileWorkspace {
							allowed += ",Edit,Write,Bash"
						}
						want = append(want, "--permission-mode", permission, "--allowedTools="+allowed)
						if mode == model.ModePane {
							want = append(want, "--settings", l.path(".settings.json"))
						}
					case model.HarnessCodex:
						want = append(want, "--no-daemon")
						if p == model.ProfileBypass {
							want = append(want, "--dangerously-bypass-approvals-and-sandbox")
						} else {
							sandbox := "read-only"
							if p == model.ProfileWorkspace {
								sandbox = "workspace-write"
							}
							want = append(want, "-s", sandbox)
						}
						b, _ := json.Marshal(l.Binary)
						args, _ := json.Marshal([]string{"mcp", "--member", l.MemberID, "--token-file", l.TokenPath()})
						want = append(want, "-c", "mcp_servers.lugarn.command="+string(b), "-c", "mcp_servers.lugarn.args="+string(args))
						for _, name := range []string{"send", "reply", "inbox", "team", "spawn", "stop"} {
							want = append(want, "-c", "mcp_servers.lugarn.tools."+name+`.approval_mode="approve"`)
						}
						if mode == model.ModeHeadless {
							want = append(want, "exec", "--json")
							if l.Resume {
								want = append(want, "resume", l.SessionID)
							}
							want = append(want, "--skip-git-repo-check", "-")
						} else {
							want = append(want, "resume", l.SessionID)
						}
					case model.HarnessOpenCode:
						if mode == model.ModeHeadless {
							want = []string{"run", "--format", "json"}
						}
						want = append(want, "--standalone")
						if l.Resume {
							want = append(want, "--session", l.SessionID)
						}
						if p != model.ProfileReadOnly {
							want = append(want, "--auto")
						}
					}
					if !reflect.DeepEqual(got, want) {
						t.Fatalf("got  %#v\nwant %#v", got, want)
					}
					env, e := Environment(l)
					if e != nil || env["LUGARN_COMMS_MEMBER"] != l.MemberID || env["LUGARN_COMMS_TOKEN_FILE"] != l.TokenPath() {
						t.Fatal(env, e)
					}
					if h == model.HarnessOpenCode && env["OPENCODE_CONFIG"] != l.path(".opencode.json") {
						t.Fatal(env)
					}
				})
			}
		}
	}
}

func TestConfigQuoting(t *testing.T) {
	l := testLaunch(t, model.HarnessCodex)
	l.Binary += ` space"quote\backslash`
	l.ExtraEnv = map[string]string{"EXAMPLE": "value"}
	args, err := Args(l, model.ModeHeadless)
	if err != nil {
		t.Fatal(err)
	}
	var decoded string
	if err = json.Unmarshal([]byte(strings.TrimPrefix(args[4], "mcp_servers.lugarn.command=")), &decoded); err != nil || decoded != l.Binary {
		t.Fatal(args, decoded, err)
	}
	if _, err = Args(l, "unknown"); err == nil {
		t.Fatal("invalid mode accepted")
	}
	env, err := Environment(l)
	if err != nil || env["EXAMPLE"] != "value" {
		t.Fatal(env, err)
	}
}

func TestModelSelectors(t *testing.T) {
	for _, h := range []model.Harness{model.HarnessClaude, model.HarnessCodex, model.HarnessOpenCode} {
		l := testLaunch(t, h)
		l.Model = "provider/model"
		a, err := Args(l, model.ModeHeadless)
		if err != nil {
			t.Fatal(err)
		}
		at := 0
		if h == model.HarnessOpenCode {
			at = 4
		}
		if a[at] != "--model" || a[at+1] != l.Model {
			t.Fatal(a)
		}
		l.Model = "--model bad\n"
		if l.Validate() == nil {
			t.Fatal("accepted unsafe model")
		}
	}
}

func TestOpenCodeModelFlagByVersionAndMode(t *testing.T) {
	for _, version := range []string{"1.19.4", "2.1.4"} {
		for _, mode := range []model.Mode{model.ModePane, model.ModeHeadless} {
			l := testLaunch(t, model.HarnessOpenCode)
			l.Version = version
			l.Model = "opencode-go/kimi-k3.1-pro"
			l.Resume = true
			l.SessionID = "ses_fixture"
			args, e := Args(l, mode)
			if e != nil {
				t.Fatal(e)
			}
			found := false
			session := false
			for n, arg := range args {
				if arg == "--model" {
					found = n+1 < len(args) && args[n+1] == l.Model
				}
				if arg == "--session" {
					session = n+1 < len(args) && args[n+1] == l.SessionID
				}
			}
			if found != (mode == model.ModeHeadless || version[0] == '1') || !session {
				t.Fatal(version, mode, args)
			}
		}
	}
}
