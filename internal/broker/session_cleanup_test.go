package broker

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
	"github.com/LexingtonStanley/lugarn-comms/internal/store"
	"github.com/LexingtonStanley/lugarn-comms/internal/wire"
)

func TestApplyRemovalCleansMemberSessions(t *testing.T) {
	b, _ := setup(t)
	ctx := context.Background()
	spec := model.TeamSpec{Version: 1, ID: "sessions-team", Name: "sessions-team", Pattern: model.PatternCustom, Members: []model.MemberSpec{
		{ID: "removed", Name: "removed", Role: model.RoleWorker, Harness: model.HarnessCodex, Folder: t.TempDir(), Profile: model.ProfileReadOnly, Mode: model.ModeHeadless, Brief: "brief"},
		{ID: "retained", Name: "retained", Role: model.RoleWorker, Harness: model.HarnessCodex, Folder: t.TempDir(), Profile: model.ProfileReadOnly, Mode: model.ModeHeadless, Brief: "brief"},
	}, Grid: []model.GridEdge{}}
	_, e := b.ApplyTeam(ctx, wire.ApplyTeamRequest{Spec: spec})
	must(t, e)
	for _, id := range []string{"removed", "retained", "writer"} {
		dir := filepath.Dir(b.originPath(id, "session"))
		must(t, os.MkdirAll(dir, 0700))
		must(t, os.WriteFile(b.originPath(id, "session"), []byte("private metadata"), 0600))
	}
	outside := filepath.Join(t.TempDir(), "keep")
	must(t, os.WriteFile(outside, []byte("keep"), 0600))
	// A link inside the removed directory must not affect its target.
	if e := os.Symlink(outside, filepath.Join(filepath.Dir(b.originPath("removed", "session")), "link")); e != nil && os.PathSeparator != '\\' {
		t.Fatal(e)
	}
	spec.Members = spec.Members[1:]
	_, e = b.ApplyTeam(ctx, wire.ApplyTeamRequest{Spec: spec, IfRevision: 1})
	must(t, e)
	if _, e := os.Lstat(filepath.Dir(b.originPath("removed", "session"))); !os.IsNotExist(e) {
		t.Fatal("removed sessions remain", e)
	}
	for _, path := range []string{outside, b.originPath("retained", "session"), b.originPath("writer", "session")} {
		if _, e := os.Stat(path); e != nil {
			t.Fatal("removed live/foreign metadata", e)
		}
	}
}
func TestSessionCleanupDoesNotFollowLinks(t *testing.T) {
	for _, mode := range []string{"root-link", "member-link"} {
		t.Run(mode, func(t *testing.T) {
			b, _ := setup(t)
			ctx := context.Background()
			outside := t.TempDir()
			keep := filepath.Join(outside, "keep")
			must(t, os.WriteFile(keep, []byte("keep"), 0600))
			must(t, b.db.Write(ctx, func(tx *store.Tx) error {
				return tx.RemoveMember("writer", model.RemovedReasonRemovedFromSpec, b.now())
			}))
			root := filepath.Join(b.opts.StateDir, "sessions")
			link := root
			if mode == "member-link" {
				must(t, os.Mkdir(root, 0700))
				link = filepath.Join(root, "writer")
			}
			if e := os.Symlink(outside, link); e != nil {
				t.Skip(e)
			}
			e := b.cleanRemovedSessions(ctx, "t")
			if mode == "root-link" && e == nil {
				t.Fatal("accepted linked sessions root")
			}
			if mode == "member-link" {
				must(t, e)
				if _, e := os.Lstat(link); !os.IsNotExist(e) {
					t.Fatal("member link remains")
				}
			}
			if _, e := os.Stat(keep); e != nil {
				t.Fatal("followed cleanup link", e)
			}
		})
	}
}

func TestNewRunRemovalCleansSpawnedMemberSessions(t *testing.T) {
	b, _ := setup(t)
	ctx := context.Background()
	must(t, b.db.Write(ctx, func(tx *store.Tx) error {
		if e := tx.UpdateTeam("t", store.TeamPatch{State: ptr(model.TeamStateStopped)}, b.now()); e != nil {
			return e
		}
		return tx.InsertMember(store.MemberParams{ID: "retired", TeamID: "t", Name: "retired", Kind: model.MemberKindAgent, Role: ptr(model.RoleWorker), Origin: model.MemberOriginSpawn, SpawnedBy: ptr("writer"), SpawnedRun: ptr(int64(1)), Harness: ptr(model.HarnessCodex), Profile: ptr(model.ProfileReadOnly), Mode: ptr(model.ModeHeadless), Folder: ptr(b.opts.StateDir), Brief: ptr("brief"), Task: ptr("task"), State: model.MemberStateStopped})
	}))
	dir := filepath.Dir(b.originPath("retired", "old-session"))
	must(t, os.MkdirAll(dir, 0700))
	must(t, os.WriteFile(b.originPath("retired", "old-session"), []byte("metadata"), 0600))
	// The default launch seam fails after removal commits; cleanup must already
	// have run even though the new runner never starts.
	_, e := b.StartTeam(ctx, wire.StartTeamRequest{Team: "t"})
	code(t, e, model.ErrorRunnerUnavailable)
	if _, e := os.Lstat(dir); !os.IsNotExist(e) {
		t.Fatal("retired worker sessions remain", e)
	}
	must(t, b.db.Read(ctx, func(tx *store.Tx) error {
		m, e := tx.GetMember("retired")
		if e != nil {
			return e
		}
		if m.RemovedReason == nil || *m.RemovedReason != model.RemovedReasonNewRun {
			t.Fatal("worker was not retired", m)
		}
		return nil
	}))
}

func TestSessionCleanupFailureDoesNotStrandStartingTeam(t *testing.T) {
	b, _ := setup(t)
	ctx := context.Background()
	must(t, b.db.Write(ctx, func(tx *store.Tx) error {
		return tx.UpdateTeam("t", store.TeamPatch{State: ptr(model.TeamStateStopped)}, b.now())
	}))
	// A file at the sessions root refuses cleanup without following any path.
	must(t, os.WriteFile(filepath.Join(b.opts.StateDir, "sessions"), []byte("invalid root"), 0600))
	_, err := b.StartTeam(ctx, wire.StartTeamRequest{Team: "t"})
	code(t, err, model.ErrorInternal)
	must(t, b.db.Read(ctx, func(tx *store.Tx) error {
		team, e := tx.GetTeam("t")
		if e != nil {
			return e
		}
		if team.State != model.TeamStateStopped {
			t.Fatal("cleanup failure stranded starting team", team.State)
		}
		return nil
	}))
}
