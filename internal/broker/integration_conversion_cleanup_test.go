package broker

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/LexingtonStanley/lugarn-comms/internal/herdr"
	"github.com/LexingtonStanley/lugarn-comms/internal/model"
	"github.com/LexingtonStanley/lugarn-comms/internal/store"
)

type failedConversionHerdr struct {
	lifecycleHerdr
	oldPaths []string
	t        *testing.T
}

func (h *failedConversionHerdr) OpenTab(context.Context, string, string, string, map[string]string) (herdr.Tab, error) {
	// Assert deletion already happened before the failing external launch step.
	for _, path := range h.oldPaths {
		if _, e := os.Lstat(path); !os.IsNotExist(e) {
			h.t.Errorf("ended launch reached new pane setup: %s", path)
		}
	}
	return herdr.Tab{}, errors.New("test tab creation failure")
}
func TestFailedPaneConversionRemovesPreviousLaunchFiles(t *testing.T) {
	b, r, m := headlessFixture(t, &headlessStub{})
	configureWindowsPaneMember(t, b, &m)
	old := *m.Lease.Launch
	paths := []string{}
	for _, suffix := range []string{".token", ".json", ".settings.json", ".opencode.json"} {
		path := filepath.Join(b.opts.StateDir, "launch", old+suffix)
		must(t, os.WriteFile(path, []byte("test old launch file"), 0600))
		paths = append(paths, path)
	}
	paths = append(paths, filepath.Join(b.opts.StateDir, "launch", old))
	binary, e := os.Executable()
	must(t, e)
	client := &failedConversionHerdr{oldPaths: paths, t: t}
	i, e := NewIntegration(b, IntegrationOptions{Binary: fakePlainBrokerPath(binary), HomeDir: t.TempDir(), Herdr: func(string) HerdrAPI { return client }})
	must(t, e)
	i.windowsPanes = true
	r.opts.Files = i.adapter
	r.opts.Binary = func(model.Harness) (string, error) { return binary, nil }
	l := &integrationLifecycle{i: i, r: r}
	if e = l.Open(context.Background(), m, model.LeaseCauseOpenTerminal); e == nil {
		t.Fatal("expected conversion failure")
	}
	for _, path := range paths {
		if _, e = os.Lstat(path); !os.IsNotExist(e) {
			t.Fatalf("ended launch remains: %s: %v", path, e)
		}
	}
	must(t, b.db.Read(context.Background(), func(tx *store.Tx) error {
		launch, e := tx.GetLaunch(old)
		if e != nil {
			return e
		}
		if launch.EndedAt == nil || launch.EndReason == nil || *launch.EndReason != model.LaunchEndReasonConverted {
			t.Fatal(launch)
		}
		member, e := tx.GetMember(m.ID)
		if e == nil && (member.State != model.MemberStateStopped || member.StopReason == nil || *member.StopReason != model.StopReasonLaunchFailed) {
			t.Fatal(member)
		}
		return e
	}))
}
