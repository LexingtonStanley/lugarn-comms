//go:build linux

package broker

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/LexingtonStanley/lugarn-comms/internal/herdr"
	"github.com/LexingtonStanley/lugarn-comms/internal/hostprobe"
	"github.com/LexingtonStanley/lugarn-comms/internal/wire"
)

func init() {
	integrationHelpers["path-shape"] = func() int {
		home, _ := os.UserHomeDir()
		has := slices.Contains(filepath.SplitList(os.Getenv("PATH")), filepath.Join(home, ".local", "bin"))
		if os.WriteFile(os.Getenv("LUGARN_PATH_RESULT"), []byte(fmt.Sprint(has)), 0600) != nil {
			return 1
		}
		return 0
	}
}
func TestIntegrationLiveLoginPath(t *testing.T) {
	if os.Getenv("LUGARN_INTEGRATION_PATH_LIVE") != "1" {
		t.Skip("explicit login PATH/systemd probe opt-in")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	b, _ := setup(t)
	b.opts.Probe = &logindProbe{}
	b.opts.LoginPath = &hostprobe.PathInfo{Inherited: os.Getenv("PATH")}
	bin, e := exec.LookPath("herdr")
	must(t, e)
	i, e := NewIntegration(b, IntegrationOptions{HerdrBinary: bin, HomeDir: t.TempDir()})
	must(t, e)
	i.Attach()
	session := privateIntegrationSession(t)
	startedAt := time.Now()
	result, e := b.StartHerdr(ctx, wire.StartHerdrRequest{Server: &session})
	if result.Started {
		defer stopIntegrationServer(t, bin, session, 0)
	}
	must(t, e)
	if result.Method == nil || *result.Method != "systemd" {
		t.Fatal(result)
	}
	home, e := os.UserHomeDir()
	must(t, e)
	p := b.opts.LoginPath
	if p.Login == nil {
		t.Fatalf("capture issue: %v", p.Issue)
	}
	t.Logf("capture shape: entries=%d local_bin=%v elapsed_with_server_ms=%d", len(filepath.SplitList(*p.Login)), slices.Contains(filepath.SplitList(*p.Login), filepath.Join(home, ".local", "bin")), time.Since(startedAt).Milliseconds())
	outPath := filepath.Join(t.TempDir(), "path-result")
	c := herdr.New(bin, session)
	ws, e := c.CreateWorkspace(ctx, "login-path-probe", b.opts.StateDir, map[string]string{"LUGARN_INTEGRATION_HELPER": "path-shape", "LUGARN_PATH_RESULT": outPath})
	must(t, e)
	self, e := os.Executable()
	must(t, e)
	must(t, c.RunCommand(ctx, ws.RootPane, self))
	deadline := time.Now().Add(20 * time.Second)
	for {
		raw, e := os.ReadFile(outPath)
		if e == nil {
			t.Logf("server pane local_bin=%s", raw)
			if string(raw) != "true" {
				t.Fatal("missing login entry")
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("pane helper did not run")
		}
		time.Sleep(50 * time.Millisecond)
	}
	cmd := exec.CommandContext(ctx, "systemd-run", "--user", "--wait", "--pipe", "--quiet", "/bin/sh", "-c", `printf %s "$PATH"`)
	raw, e := cmd.Output()
	must(t, e)
	has := slices.Contains(strings.Split(string(raw), ":"), filepath.Join(home, ".local", "bin"))
	t.Logf("bare user-manager command local_bin=%v", has)
	if has {
		t.Fatal("baseline unexpectedly already includes local bin")
	}
}
