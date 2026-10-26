package broker

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

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

type blockingGit struct {
	fakeGit
	entered chan struct{}
	release chan struct{}
}

func (g *blockingGit) Remove(ctx context.Context, _, _, _ string) (gitwt.BranchOutcome, error) {
	close(g.entered)
	select {
	case <-g.release:
		return gitwt.BranchOutcomeDeleted, nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func ownedStoppedWorktree(t *testing.T, b *Broker) store.WorktreeRecord {
	t.Helper()
	id, err := model.NewULID()
	must(t, err)
	w := store.WorktreeRecord{ID: id, TeamID: "t", MemberID: "writer", Repo: t.TempDir(), Path: filepath.Join(t.TempDir(), "tree"), Branch: "lugarn/test/writer", Base: "main", CreatedAt: b.now()}
	must(t, b.db.Write(context.Background(), func(tx *store.Tx) error {
		if err := tx.SetMemberState("writer", model.MemberStateStopped, nil, nil, nil, b.now()); err != nil {
			return err
		}
		return tx.InsertWorktree(w)
	}))
	return w
}

func TestRemoveWorktreeDoesNotBlockOtherWriters(t *testing.T) {
	b, _ := setup(t)
	w := ownedStoppedWorktree(t, b)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	git := &blockingGit{entered: make(chan struct{}), release: make(chan struct{})}
	b.opts.Git = git
	type result struct {
		out wire.RemoveWorktreesResult
		err error
	}
	done := make(chan result, 1)
	go func() {
		out, err := b.RemoveWorktrees(ctx, wire.RemoveWorktreesRequest{Team: "t", Paths: []string{w.Path}})
		done <- result{out, err}
	}()
	defer func() {
		close(git.release)
		r := <-done
		must(t, r.err)
		if len(r.out.Removed) != 1 {
			t.Errorf("removal result: %#v", r.out)
		}
	}()
	select {
	case <-git.entered:
	case <-ctx.Done():
		t.Fatal("git removal did not start")
	}
	writeCtx, stopWrite := context.WithTimeout(ctx, time.Second)
	defer stopWrite()
	_, err := b.Send(writeCtx, wire.SendRequest{Team: "t", To: ptr("reviewer"), Text: "send while git is blocked"})
	must(t, err)
}

type concurrentRemovalGit struct {
	fakeGit
	b        *Broker
	worktree store.WorktreeRecord
}

func (g *concurrentRemovalGit) Remove(ctx context.Context, _, _, _ string) (gitwt.BranchOutcome, error) {
	// Stand in for another process completing the same removal before this
	// process can record it. This also proves git is outside the write lock.
	err := g.b.db.Write(ctx, func(tx *store.Tx) error {
		if err := tx.RemoveWorktree(g.worktree.ID, g.b.now()); err != nil {
			return err
		}
		return g.b.event(tx, ptr("t"), model.EventWorktreeRemoved, true, ptr("writer"), nil, wire.WorktreeRemovedEventData{Path: g.worktree.Path, Branch: g.worktree.Branch, Member: model.MemberRef{ID: "writer", Name: "writer"}, BranchOutcome: wire.BranchOutcomeDeleted})
	})
	return gitwt.BranchOutcomeDeleted, err
}

func TestRemoveWorktreeConcurrentCompletionEmitsOneEvent(t *testing.T) {
	b, _ := setup(t)
	w := ownedStoppedWorktree(t, b)
	b.opts.Git = &concurrentRemovalGit{b: b, worktree: w}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := b.RemoveWorktrees(ctx, wire.RemoveWorktreesRequest{Team: "t", Paths: []string{w.Path}})
	must(t, err)
	if len(out.Removed) != 1 || len(out.Refused) != 0 {
		t.Fatal(out)
	}
	must(t, b.db.Read(ctx, func(tx *store.Tx) error {
		events, err := tx.ReplayEvents(0, ptr("t"), 0)
		if err != nil {
			return err
		}
		if len(events) != 1 || events[0].Type != model.EventWorktreeRemoved {
			t.Fatalf("removal events: %#v", events)
		}
		return nil
	}))
}
