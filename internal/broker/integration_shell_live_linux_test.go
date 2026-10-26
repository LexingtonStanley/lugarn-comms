//go:build linux

package broker

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/LexingtonStanley/lugarn-comms/internal/harness"
	"github.com/LexingtonStanley/lugarn-comms/internal/herdr"
	"github.com/LexingtonStanley/lugarn-comms/internal/model"
	"github.com/LexingtonStanley/lugarn-comms/internal/statedir"
)

func privateIntegrationSession(t *testing.T) string {
	t.Helper()
	var b [8]byte
	_, e := rand.Read(b[:])
	must(t, e)
	return "lugarn-test-" + hex.EncodeToString(b[:])
}
func stopIntegrationServer(t *testing.T, bin, session string, pid int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "--session", session, "server", "stop")
	out, e := cmd.CombinedOutput()
	t.Logf("herdr --session %s server stop: %s (%v)", session, out, e)
	if e != nil && pid > 0 {
		state, inspect := (nativeProcesses{}).Inspect(ctx, int64(pid))
		if inspect == nil && state.Alive {
			_ = cancelOwnedTree(ctx, int64(pid), state.Birth)
		}
	}
}
func TestIntegrationLiveShellPinning(t *testing.T) {
	if os.Getenv("LUGARN_INTEGRATION_SHELL_LIVE") != "1" {
		t.Skip("opt in to isolated Herdr shell probes")
	}
	herdrBin, e := exec.LookPath("herdr")
	must(t, e)
	goBin, e := exec.LookPath("go")
	must(t, e)
	brokerBin := filepath.Join(t.TempDir(), "lugarn-comms")
	build := exec.Command(goBin, "build", "-o", brokerBin, "../../cmd/lugarn-comms")
	out, e := build.CombinedOutput()
	if e != nil {
		t.Fatalf("build: %s: %v", out, e)
	}
	fakeBinary, e := os.Executable()
	must(t, e)
	for _, shell := range []string{"zsh", "bash", "fish"} {
		t.Run(shell, func(t *testing.T) {
			shellBin, e := exec.LookPath(shell)
			if e != nil {
				t.Skip("shell is not installed")
			}
			// Herdr's Unix socket path must fit sockaddr_un; Go's long test
			// temp prefix would exceed it. This short private home holds fake
			// shell rc/config only, never provider credentials or copied homes.
			home, e := os.MkdirTemp("", "lc-")
			must(t, e)
			t.Cleanup(func() { os.RemoveAll(home) })
			t.Setenv("HOME", home)
			t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
			configDir := filepath.Join(home, ".config", "herdr")
			must(t, os.MkdirAll(configDir, 0700))
			must(t, os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(fmt.Sprintf("[terminal]\ndefault_shell = %q\nshell_mode = \"non_login\"\n", shellBin)), 0600))
			shadow := filepath.Join(home, "shadow")
			must(t, os.Mkdir(shadow, 0700))
			native, e := os.ReadFile(fakeBinary)
			must(t, e)
			for _, name := range []string{"claude", "codex"} {
				must(t, os.WriteFile(filepath.Join(shadow, name), native, 0700))
			}
			hijacked := filepath.Join(home, "hijacked")
			rc := fmt.Sprintf("alias claude='printf hijacked >> %s'\ncodex() { printf hijacked >> %s; }\nexport PATH=%s:$PATH\n", shellQuote(hijacked), shellQuote(hijacked), shellQuote(shadow))
			switch shell {
			case "zsh":
				must(t, os.WriteFile(filepath.Join(home, ".zshrc"), []byte(rc), 0600))
			case "bash":
				must(t, os.WriteFile(filepath.Join(home, ".bashrc"), []byte(rc), 0600))
			case "fish":
				must(t, os.MkdirAll(filepath.Join(home, ".config", "fish"), 0700))
				must(t, os.WriteFile(filepath.Join(home, ".config", "fish", "config.fish"), []byte("alias claude 'echo hijacked'; function codex; echo hijacked; end; set -gx PATH "+shellQuote(shadow)+" $PATH\n"), 0600))
			}
			session := privateIntegrationSession(t)
			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()
			client := herdr.New(herdrBin, session)
			status, e := client.Status(ctx)
			must(t, e)
			if status.Running {
				t.Fatal("fresh private session already exists")
			}
			started, e := herdr.StartServer(ctx, herdrBin, session)
			must(t, e)
			defer stopIntegrationServer(t, herdrBin, session, started.PID)
			t.Logf("private server %s pid=%d shell=%s", session, started.PID, shell)
			b, _ := setup(t)
			b.opts.StateDir = filepath.Join(t.TempDir(), "state with spaces")
			must(t, statedir.Ensure(b.opts.StateDir))
			integration, e := NewIntegration(b, IntegrationOptions{Binary: brokerBin, HomeDir: home})
			must(t, e)
			work := filepath.Join(t.TempDir(), "work with spaces")
			must(t, os.Mkdir(work, 0700))
			ws, e := client.CreateWorkspace(ctx, "lugarn-shell-test", work, nil)
			must(t, e)
			for _, kind := range []model.Harness{model.HarnessClaude, model.HarnessCodex} {
				id := cleanupID(t)
				sessionID := "00000000-0000-4000-8000-000000000001"
				l := harness.Launch{Harness: kind, Profile: model.ProfileReadOnly, WorkDir: work, StateDir: b.opts.StateDir, LaunchID: id, MemberID: "probe", Binary: brokerBin, SessionID: sessionID, Resume: kind == model.HarnessCodex, ExtraEnv: map[string]string{"LUGARN_INTEGRATION_HELPER": "argv-agent"}}
				args, e := harness.Args(l, model.ModePane)
				must(t, e)
				env, boot, e := integration.preparePaneShell(ctx, l, fakeBinary)
				must(t, e)
				log := filepath.Join(home, string(kind)+".argv.json")
				env["LUGARN_INTEGRATION_ARGV_LOG"] = log
				tab, e := client.OpenTab(ctx, ws.ID, string(kind), work, env)
				must(t, e)
				must(t, client.RunCommand(ctx, tab.RootPane, boot...))
				must(t, integration.waitShell(ctx, id))
				_, startErr := client.StartAgent(ctx, herdr.StartOptions{Name: herdr.AgentName("probe", string(kind)), Harness: kind, Pane: tab.RootPane, Args: args, Timeout: 4 * time.Second})
				raw, e := os.ReadFile(log)
				if e != nil {
					t.Errorf("%s never reached pinned executable: %v (start=%v)", kind, e, startErr)
				} else {
					var got struct {
						Binary string
						Args   []string
						Pane   string
					}
					must(t, json.Unmarshal(raw, &got))
					t.Logf("%s argv=%q HERDR_PANE_ID=%s (ready=%v)", kind, got.Args, got.Pane, startErr)
					if !sameNativePath(got.Binary, fakeBinary) || !reflect.DeepEqual(got.Args, args) {
						t.Errorf("%s quoting/pinning mismatch: executable=%q args=%q, want %q", kind, got.Binary, got.Args, args)
					}
				}
				must(t, client.CloseTab(ctx, tab.ID))
			}
			if _, e = os.Stat(hijacked); !os.IsNotExist(e) {
				t.Error("hostile rc command ran")
			}
		})
	}
}
