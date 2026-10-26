package broker

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
	"github.com/LexingtonStanley/lugarn-comms/internal/store"
)

func bridgeFixture(t *testing.T) (*Integration, string) {
	t.Helper()
	temp := t.TempDir()
	for _, key := range []string{"TMPDIR", "TMP", "TEMP"} {
		t.Setenv(key, temp)
	}
	b, _ := setup(t)
	i, e := NewIntegration(b, IntegrationOptions{HomeDir: t.TempDir()})
	must(t, e)
	return i, temp
}
func makeBridge(t *testing.T, i *Integration, root string, age time.Duration) string {
	t.Helper()
	dir, e := os.MkdirTemp(root, "lugarn-runner-")
	must(t, e)
	must(t, i.markBridge(dir))
	name := "lugarn-comms"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	must(t, os.WriteFile(filepath.Join(dir, name), []byte("fake executable"), 0600))
	at := i.b.opts.Clock().Add(-age)
	must(t, os.Chtimes(dir, at, at))
	return dir
}
func TestRunnerBridgeSweepOwnershipAgeAndPlacements(t *testing.T) {
	i, temp := bridgeFixture(t)
	other, _ := NewIntegration(New(i.b.db, Options{StateDir: t.TempDir()}), IntegrationOptions{})
	staleTemp := makeBridge(t, i, temp, 2*time.Minute)
	staleState := makeBridge(t, i, i.b.opts.StateDir, 2*time.Minute)
	young := makeBridge(t, i, temp, 0)
	recorded := makeBridge(t, i, temp, 2*time.Minute)
	foreign := makeBridge(t, other, temp, 2*time.Minute)
	unmarked := makeBridge(t, i, temp, 2*time.Minute)
	must(t, os.Remove(filepath.Join(unmarked, bridgeOwnerFile)))
	at := i.b.opts.Clock().Add(-2 * time.Minute)
	must(t, os.Chtimes(unmarked, at, at))
	must(t, i.savePlacement("another-team", runnerPlacement{BridgeDir: recorded}))
	i.sweepRunnerBridges()
	for _, dir := range []string{staleTemp, staleState} {
		if _, e := os.Lstat(dir); !os.IsNotExist(e) {
			t.Fatalf("stale owned bridge survives: %s: %v", dir, e)
		}
	}
	for _, dir := range []string{young, recorded, foreign, unmarked} {
		if _, e := os.Lstat(dir); e != nil {
			t.Fatalf("removed protected bridge %s: %v", dir, e)
		}
	}
}
func TestRunnerBridgeSweepRefusesLinksAndUnreadablePlacements(t *testing.T) {
	for _, mode := range []string{"marker-link", "directory-link", "malformed-placement"} {
		t.Run(mode, func(t *testing.T) {
			i, temp := bridgeFixture(t)
			dir := makeBridge(t, i, temp, 2*time.Minute)
			switch mode {
			case "marker-link":
				marker := filepath.Join(dir, bridgeOwnerFile)
				raw, e := os.ReadFile(marker)
				must(t, e)
				outside := filepath.Join(t.TempDir(), "owner")
				must(t, os.WriteFile(outside, raw, 0600))
				must(t, os.Remove(marker))
				if e := os.Symlink(outside, marker); e != nil {
					t.Skip(e)
				}
			case "directory-link":
				outside := filepath.Join(t.TempDir(), "bridge")
				must(t, os.Rename(dir, outside))
				if e := os.Symlink(outside, dir); e != nil {
					t.Skip(e)
				}
			case "malformed-placement":
				must(t, os.MkdirAll(filepath.Dir(i.placementPath("bad")), 0700))
				must(t, os.WriteFile(i.placementPath("bad"), []byte("invalid"), 0600))
			}
			at := i.b.opts.Clock().Add(-2 * time.Minute)
			if mode != "directory-link" {
				must(t, os.Chtimes(dir, at, at))
			}
			i.sweepRunnerBridges()
			if _, e := os.Lstat(dir); e != nil {
				t.Fatal("removed unsafe candidate", e)
			}
		})
	}
}
func TestRunnerBridgeFailedRemovalKeepsMarkerForRetry(t *testing.T) {
	i, temp := bridgeFixture(t)
	dir := makeBridge(t, i, temp, 2*time.Minute)
	name := "lugarn-comms"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	exe := filepath.Join(dir, name)
	must(t, os.Remove(exe))
	must(t, os.Mkdir(exe, 0700))
	must(t, os.WriteFile(filepath.Join(exe, "busy"), []byte("busy"), 0600))
	at := i.b.opts.Clock().Add(-2 * time.Minute)
	must(t, os.Chtimes(dir, at, at))
	i.sweepRunnerBridges()
	if !i.ownsBridge(dir) {
		t.Fatal("failed deletion lost owner marker")
	}
	must(t, os.RemoveAll(exe))
	must(t, os.Chtimes(dir, at, at))
	i.sweepRunnerBridges()
	if _, e := os.Lstat(dir); !os.IsNotExist(e) {
		t.Fatal("retry left bridge", e)
	}
}
func TestRunnerTakeoverSweepsBridges(t *testing.T) {
	i, temp := bridgeFixture(t)
	dir := makeBridge(t, i, temp, 2*time.Minute)
	// No recorded pane is needed for the takeover sweep.
	r := i.Runner("t", RunnerOptions{})
	must(t, r.onAcquire(context.Background()))
	if _, e := os.Lstat(dir); !os.IsNotExist(e) {
		t.Fatal(e)
	}
}

type failedRunnerHerdr struct{ launcherHerdr }

func (h *failedRunnerHerdr) RunCommand(context.Context, string, ...string) error {
	return errors.New(launchSecret)
}
func TestRunnerLaunchFailurePreservesRecordedBridge(t *testing.T) {
	i, _ := bridgeFixture(t)
	b := i.b
	ctx := context.Background()
	runnerReady(t, b)
	source := filepath.Join(t.TempDir(), "broker with spaces")
	if runtime.GOOS == "windows" {
		source += ".exe"
	}
	must(t, os.WriteFile(source, []byte("fake broker executable"), 0700))
	i.opts.Binary = source
	h := &failedRunnerHerdr{launcherHerdr: launcherHerdr{b: b}}
	i.opts.Herdr = func(string) HerdrAPI { return h }
	var team *store.Team
	must(t, b.db.Read(ctx, func(tx *store.Tx) error { var e error; team, e = tx.GetTeam("t"); return e }))
	err := i.Launch(ctx, *team)
	code(t, err, model.ErrorRunnerUnavailable)
	raw, e := json.Marshal(model.AsError(err))
	must(t, e)
	if strings.Contains(err.Error()+string(raw), launchSecret) {
		t.Fatal("runner launch diagnostics leaked payload")
	}
	p, e := i.placement("t")
	must(t, e)
	if p.BridgeDir == "" || !i.ownsBridge(p.BridgeDir) {
		t.Fatal("removed bridge while still recorded", p)
	}
	must(t, i.Close(ctx, *team, nil))
	if _, e := os.Lstat(p.BridgeDir); !os.IsNotExist(e) {
		t.Fatal("stop-team left bridge", e)
	}
}
