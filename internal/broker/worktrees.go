package broker

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/LexingtonStanley/lugarn-comms/internal/gitwt"
	"github.com/LexingtonStanley/lugarn-comms/internal/model"
	"github.com/LexingtonStanley/lugarn-comms/internal/store"
	"github.com/LexingtonStanley/lugarn-comms/internal/wire"
)

// BranchStatus compares a broker branch with its recorded base. Null values
// mean the branch is gone. A missing/invalid base is an error.
type BranchStatus struct {
	Merged        *bool
	Ahead, Behind *int64
}

func (realGit) Compare(ctx context.Context, repo, branch, base string) (BranchStatus, error) {
	exists, e := gitwt.BranchExists(ctx, repo, branch)
	if e != nil || !exists {
		return BranchStatus{}, e
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	// --end-of-options treats stored refs as data, including unusual base names.
	run := func(args ...string) ([]byte, error) {
		cmd := exec.CommandContext(ctx, "git", append([]string{"-C", repo}, args...)...)
		cmd.Env = append(os.Environ(), "LC_ALL=C", "GIT_TERMINAL_PROMPT=0")
		cmd.WaitDelay = time.Second
		return cmd.CombinedOutput()
	}
	out, e := run("rev-list", "--left-right", "--count", "--end-of-options", base+"...refs/heads/"+branch)
	if e != nil {
		return BranchStatus{}, e
	}
	parts := strings.Fields(string(out))
	if len(parts) != 2 {
		return BranchStatus{}, errors.New("git returned invalid ahead/behind counts")
	}
	behind, e := strconv.ParseInt(parts[0], 10, 64)
	if e != nil {
		return BranchStatus{}, e
	}
	ahead, e := strconv.ParseInt(parts[1], 10, 64)
	if e != nil {
		return BranchStatus{}, e
	}
	return BranchStatus{Merged: ptr(ahead == 0), Ahead: &ahead, Behind: &behind}, nil
}
func (b *Broker) Worktrees(ctx context.Context, r wire.WorktreesRequest) (wire.WorktreesResult, error) {
	out := wire.WorktreesResult{Worktrees: []wire.WorktreeItem{}}
	if e := b.contact(ctx, &r.Team); e != nil {
		return out, e
	}
	e := b.db.Read(ctx, func(tx *store.Tx) error {
		ws, e := tx.ListWorktrees(r.Team, false)
		if e != nil {
			return e
		}
		for _, w := range ws {
			m, e := tx.GetMember(w.MemberID)
			if e != nil {
				return e
			}
			state, e := b.opts.Git.State(ctx, w.Path)
			if e != nil {
				return model.AsError(e)
			}
			stats, e := b.opts.Git.Compare(ctx, w.Repo, w.Branch, w.Base)
			if e != nil {
				return model.AsError(e)
			}
			out.Worktrees = append(out.Worktrees, wire.WorktreeItem{Path: w.Path, Branch: w.Branch, Repo: w.Repo, Base: w.Base, Member: ref(*m), MemberState: m.State, CreatedAt: w.CreatedAt, Git: wire.WorktreeItemGit{State: wire.GitState(state), Merged: stats.Merged, Ahead: stats.Ahead, Behind: stats.Behind}})
		}
		return nil
	})
	return out, e
}
func (b *Broker) RemoveWorktrees(ctx context.Context, r wire.RemoveWorktreesRequest) (wire.RemoveWorktreesResult, error) {
	out := wire.RemoveWorktreesResult{Removed: []wire.RemoveWorktreesResultRemovedItem{}, Refused: []wire.RemoveWorktreesResultRefusedItem{}}
	if e := b.contact(ctx, &r.Team); e != nil {
		return out, e
	}
	if len(r.Paths) < 1 || len(r.Paths) > 64 {
		return out, invalid("paths must contain 1–64 paths")
	}
	for _, path := range r.Paths {
		e := b.db.Write(ctx, func(tx *store.Tx) error {
			refuse := func(reason wire.WorktreeRefusal, msg string) error {
				out.Refused = append(out.Refused, wire.RemoveWorktreesResultRefusedItem{Path: path, Reason: reason, Message: msg})
				return nil
			}
			w, e := tx.GetWorktreeByPath(path)
			if e != nil && !missing(e) {
				return e
			}
			if e != nil || w.TeamID != r.Team || w.RemovedAt != nil {
				return refuse(wire.WorktreeRefusalUnknownPath, "no live broker worktree at this path for this team")
			}
			m, e := tx.GetMember(w.MemberID)
			if e != nil {
				return e
			}
			if m.State != model.MemberStateStopped {
				return refuse(wire.WorktreeRefusalMemberNotStopped, "stop the member before removing its worktree")
			}
			// Hold BEGIN IMMEDIATE across git so no concurrent broker can launch or
			// claim the member while its stopped worktree is being removed.
			outcome, e := b.opts.Git.Remove(ctx, w.Repo, w.Path, w.Branch)
			if e != nil {
				reason := wire.WorktreeRefusalGitFailed
				var refusal *gitwt.Refusal
				if errors.As(e, &refusal) {
					reason = wire.WorktreeRefusal(refusal.Reason)
				}
				return refuse(reason, e.Error())
			}
			if e = tx.RemoveWorktree(w.ID, b.now()); e != nil {
				return e
			}
			item := wire.RemoveWorktreesResultRemovedItem{Path: w.Path, Branch: w.Branch, BranchOutcome: wire.BranchOutcome(outcome)}
			if e = b.event(tx, &r.Team, model.EventWorktreeRemoved, true, &m.ID, nil, wire.WorktreeRemovedEventData{Path: w.Path, Branch: w.Branch, Member: ref(*m), BranchOutcome: item.BranchOutcome}); e != nil {
				return e
			}
			out.Removed = append(out.Removed, item)
			return nil
		})
		if e != nil {
			return out, e
		}
	}
	return out, nil
}
