package broker

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/LexingtonStanley/lugarn-comms/internal/gitwt"
	"github.com/LexingtonStanley/lugarn-comms/internal/model"
	"github.com/LexingtonStanley/lugarn-comms/internal/store"
	"github.com/LexingtonStanley/lugarn-comms/internal/wire"
)

type fakeGit struct {
	realGit
	removed []string
	err     error
}

func (*fakeGit) State(context.Context, string) (gitwt.GitState, error) {
	return gitwt.GitStateClean, nil
}
func (*fakeGit) Compare(context.Context, string, string, string) (BranchStatus, error) {
	return BranchStatus{Merged: ptr(true), Ahead: ptr(int64(0)), Behind: ptr(int64(0))}, nil
}
func (g *fakeGit) Remove(_ context.Context, repo, path, branch string) (gitwt.BranchOutcome, error) {
	g.removed = append(g.removed, path)
	return gitwt.BranchOutcomeKeptUnmerged, g.err
}
func TestWorktrees(t *testing.T) {
	b, _ := setup(t)
	g := &fakeGit{}
	b.opts.Git = g
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "tree")
	id, e := model.NewULID()
	must(t, e)
	must(t, b.db.Write(ctx, func(tx *store.Tx) error {
		return tx.InsertWorktree(store.WorktreeRecord{ID: id, TeamID: "t", MemberID: "writer", Repo: t.TempDir(), Path: path, Branch: "lugarn/test/writer", Base: "main", CreatedAt: b.now()})
	}))
	out, e := b.Worktrees(ctx, wire.WorktreesRequest{Team: "t"})
	must(t, e)
	if len(out.Worktrees) != 1 || out.Worktrees[0].Git.Merged == nil {
		t.Fatal(out)
	}
	r, e := b.RemoveWorktrees(ctx, wire.RemoveWorktreesRequest{Team: "t", Paths: []string{path, "unknown"}})
	must(t, e)
	if len(g.removed) != 0 || len(r.Refused) != 2 || r.Refused[0].Reason != wire.WorktreeRefusalMemberNotStopped || r.Refused[1].Reason != wire.WorktreeRefusalUnknownPath {
		t.Fatal(r)
	}
	must(t, b.db.Write(ctx, func(tx *store.Tx) error {
		return tx.SetMemberState("writer", model.MemberStateStopped, nil, nil, nil, b.now())
	}))
	g.err = &gitwt.Refusal{Reason: gitwt.WorktreeRefusalDirty, Err: errors.New("dirty")}
	r, e = b.RemoveWorktrees(ctx, wire.RemoveWorktreesRequest{Team: "t", Paths: []string{path}})
	must(t, e)
	if r.Refused[0].Reason != wire.WorktreeRefusalDirty {
		t.Fatal(r)
	}
	g.err = nil
	r, e = b.RemoveWorktrees(ctx, wire.RemoveWorktreesRequest{Team: "t", Paths: []string{path}})
	must(t, e)
	if len(r.Removed) != 1 || r.Removed[0].BranchOutcome != wire.BranchOutcomeKeptUnmerged {
		t.Fatal(r)
	}
	r, e = b.RemoveWorktrees(ctx, wire.RemoveWorktreesRequest{Team: "t", Paths: []string{path}})
	must(t, e)
	if len(r.Refused) != 1 {
		t.Fatal(r)
	}
}
