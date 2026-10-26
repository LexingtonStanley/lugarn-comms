package broker

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
	"github.com/LexingtonStanley/lugarn-comms/internal/store"
	"github.com/LexingtonStanley/lugarn-comms/internal/wire"
)

func TestIntegrationWorktreeCreationAndReuse(t *testing.T) {
	if _, e := exec.LookPath("git"); e != nil {
		t.Skip("git not installed")
	}
	b, _ := setup(t)
	b.opts.Host = nil
	repo := filepath.Join(t.TempDir(), "repo")
	must(t, os.Mkdir(repo, 0700))
	git := func(args ...string) {
		t.Helper()
		out, e := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput()
		if e != nil {
			t.Fatalf("git %v: %s: %v", args, out, e)
		}
	}
	git("init")
	git("symbolic-ref", "HEAD", "refs/heads/main")
	git("-c", "user.name=Lugarn test", "-c", "user.email=test@example.invalid", "commit", "--allow-empty", "-m", "initial")
	ctx := context.Background()
	must(t, b.db.Write(ctx, func(tx *store.Tx) error {
		return tx.UpdateMember("writer", store.MemberPatch{Folder: ptr(&repo), Worktree: ptr(true)}, b.now())
	}))
	runnerReady(t, b)
	i, e := NewIntegration(b, IntegrationOptions{HomeDir: t.TempDir()})
	must(t, e)
	r := i.Runner("t", RunnerOptions{})
	must(t, r.acquire(ctx))
	var m *model.Member
	must(t, b.db.Read(ctx, func(tx *store.Tx) error { var e error; m, e = tx.GetMember("writer"); return e }))
	first, e := i.prepareWorktree(ctx, r, *m)
	must(t, e)
	if first.Worktree == nil || *first.Workdir == repo {
		t.Fatal(first)
	}
	again, e := i.prepareWorktree(ctx, r, *first)
	must(t, e)
	if again.Worktree.Path != first.Worktree.Path || again.Worktree.Branch != first.Worktree.Branch {
		t.Fatal(again)
	}
	// Removal must consult the member actually linked to the tree, not a stale
	// original owner after a same-name worker has been respawned.
	must(t, b.db.Write(ctx, func(tx *store.Tx) error {
		stored, e := tx.GetStoredMember("writer")
		if e != nil {
			return e
		}
		if e = tx.SetMemberState("writer", model.MemberStateStopped, nil, nil, nil, b.now()); e != nil {
			return e
		}
		p := store.MemberParams{ID: "new-user", TeamID: "t", Name: "new-user", Kind: model.MemberKindAgent, Origin: model.MemberOriginSpec, Role: ptr(model.RoleWorker), Harness: ptr(model.HarnessCodex), Profile: ptr(model.ProfileWorkspace), Mode: ptr(model.ModeHeadless), Folder: &repo, Brief: ptr("brief"), State: model.MemberStateRunning, WorktreeID: stored.WorktreeID}
		return tx.InsertMember(p)
	}))
	result, e := b.RemoveWorktrees(ctx, wire.RemoveWorktreesRequest{Team: "t", Paths: []string{first.Worktree.Path}})
	must(t, e)
	if len(result.Refused) != 1 || result.Refused[0].Reason != wire.WorktreeRefusalMemberNotStopped {
		t.Fatal(result)
	}
	b.opts.Git = &fakeGit{}
	must(t, b.db.Write(ctx, func(tx *store.Tx) error {
		old, e := tx.GetStoredMember("new-user")
		if e != nil {
			return e
		}
		return tx.InsertMember(store.MemberParams{ID: "newer-stopped", TeamID: "t", Name: "newer-stopped", Kind: model.MemberKindAgent, Origin: model.MemberOriginSpec, Role: ptr(model.RoleWorker), Harness: ptr(model.HarnessCodex), Profile: ptr(model.ProfileReadOnly), Mode: ptr(model.ModeHeadless), Folder: &repo, Brief: ptr("brief"), State: model.MemberStateStopped, WorktreeID: old.WorktreeID, CreatedAt: ptr(model.Time{Time: b.now().Add(time.Second)})})
	}))
	result, e = b.RemoveWorktrees(ctx, wire.RemoveWorktreesRequest{Team: "t", Paths: []string{first.Worktree.Path}})
	must(t, e)
	if len(result.Refused) != 1 || result.Refused[0].Reason != wire.WorktreeRefusalMemberNotStopped {
		t.Fatal("a newer stopped link hid an active worktree user", result)
	}

}

func TestIntegrationWorktreeProbeFailureDoesNotSilentlyShare(t *testing.T) {
	if _, e := exec.LookPath("git"); e != nil {
		t.Skip("git not installed")
	}
	b, _ := setup(t)
	b.opts.Host = nil
	b.opts.Git = &brokenGit{}
	ctx := context.Background()
	must(t, b.db.Write(ctx, func(tx *store.Tx) error {
		return tx.UpdateMember("writer", store.MemberPatch{Worktree: ptr(true)}, b.now())
	}))
	runnerReady(t, b)
	i, e := NewIntegration(b, IntegrationOptions{HomeDir: t.TempDir()})
	must(t, e)
	r := i.Runner("t", RunnerOptions{})
	must(t, r.acquire(ctx))
	var m *model.Member
	must(t, b.db.Read(ctx, func(tx *store.Tx) error { var e error; m, e = tx.GetMember("writer"); return e }))
	_, e = i.prepareWorktree(ctx, r, *m)
	code(t, e, model.ErrorInternal)
}
