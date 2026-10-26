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
			m, e := b.worktreeOwner(tx, w)
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
		refuse := func(reason wire.WorktreeRefusal, msg string) {
			out.Refused = append(out.Refused, wire.RemoveWorktreesResultRefusedItem{Path: path, Reason: reason, Message: msg})
		}
		var owned *store.WorktreeRecord
		e := b.db.Write(ctx, func(tx *store.Tx) error {
			w, e := tx.GetWorktreeByPath(path)
			if e != nil && !missing(e) {
				return e
			}
			if e != nil || w.TeamID != r.Team || w.RemovedAt != nil {
				refuse(wire.WorktreeRefusalUnknownPath, "no live broker worktree at this path for this team")
				return nil
			}
			m, e := b.worktreeOwner(tx, *w)
			if e != nil {
				return e
			}
			if m.State != model.MemberStateStopped {
				refuse(wire.WorktreeRefusalMemberNotStopped, "stop the member before removing its worktree")
				return nil
			}
			owned = w
			return nil
		})
		if e != nil {
			return out, e
		}
		if owned == nil {
			continue
		}

		// Git runs without a store transaction. A member can be woken in this
		// window; C2's runner must check the worktree path exists before launching.
		outcome, e := b.opts.Git.Remove(ctx, owned.Repo, owned.Path, owned.Branch)
		if e != nil {
			reason := wire.WorktreeRefusalGitFailed
			var refusal *gitwt.Refusal
			if errors.As(e, &refusal) {
				reason = wire.WorktreeRefusal(refusal.Reason)
			}
			refuse(reason, e.Error())
			continue
		}
		item := wire.RemoveWorktreesResultRemovedItem{Path: owned.Path, Branch: owned.Branch, BranchOutcome: wire.BranchOutcome(outcome)}
		e = b.db.Write(ctx, func(tx *store.Tx) error {
			// Check this identity rather than the path, which may already have
			// been reused by a new registration. Concurrent completion is success.
			current, e := tx.GetWorktree(owned.ID)
			if e != nil {
				return e
			}
			if current.RemovedAt != nil {
				return nil
			}
			m, e := tx.GetMember(current.MemberID)
			if e != nil {
				return e
			}
			if e = tx.RemoveWorktree(current.ID, b.now()); e != nil {
				return e
			}
			return b.event(tx, &r.Team, model.EventWorktreeRemoved, true, &m.ID, nil, wire.WorktreeRemovedEventData{Path: current.Path, Branch: current.Branch, Member: ref(*m), BranchOutcome: item.BranchOutcome})
		})
		if e != nil {
			return out, e
		}
		out.Removed = append(out.Removed, item)
	}
	return out, nil
}

// A reused worktree may be linked by a later member identity with the same name.
// Resolve its current user from live links, so an older stopped row cannot make
// removal appear safe while a respawned worker is running there.
func (b *Broker) worktreeOwner(tx *store.Tx, w store.WorktreeRecord) (*model.Member, error) {
	members, e := tx.ListMembers(w.TeamID, false)
	if e != nil {
		return nil, e
	}
	var current *model.Member
	for _, m := range members {
		if m.Worktree == nil || !sameNativePath(m.Worktree.Path, w.Path) {
			continue
		}
		if current == nil || current.State == model.MemberStateStopped && m.State != model.MemberStateStopped || (current.State == model.MemberStateStopped) == (m.State == model.MemberStateStopped) && m.CreatedAt.After(current.CreatedAt.Time) {
			copy := m
			current = &copy
		}
	}
	if current != nil {
		return current, nil
	}
	return tx.GetMember(w.MemberID)
}
