package broker

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/LexingtonStanley/lugarn-comms/internal/gitwt"
	"github.com/LexingtonStanley/lugarn-comms/internal/model"
	"github.com/LexingtonStanley/lugarn-comms/internal/policy"
	"github.com/LexingtonStanley/lugarn-comms/internal/store"
)

func (i *Integration) prepareWorktree(ctx context.Context, r *Runner, m model.Member) (*model.Member, error) {
	var stored *store.StoredMember
	var team *store.Team
	var recorded []policy.RecordedWorktree
	e := i.b.db.Read(ctx, func(tx *store.Tx) error {
		var e error
		stored, e = tx.GetStoredMember(m.ID)
		if e != nil {
			return e
		}
		team, e = tx.GetTeam(m.Team)
		if e != nil {
			return e
		}
		teams, e := tx.ListTeams()
		if e != nil {
			return e
		}
		for _, t := range teams {
			rows, e := tx.ListWorktrees(t.ID, true)
			if e != nil {
				return e
			}
			for _, row := range rows {
				owner, e := tx.GetMember(row.MemberID)
				if e != nil {
					return e
				}
				recorded = append(recorded, policy.RecordedWorktree{Team: row.TeamID, Member: owner.Name, Path: row.Path, Branch: row.Branch, Removed: row.RemovedAt != nil})
			}
		}
		return nil
	})
	if e != nil {
		return nil, e
	}
	if !stored.Worktree {
		if stored.WorktreeID == nil {
			return &m, nil
		}
		var ready *model.Member
		e := r.write(ctx, &m, func(tx *store.Tx, _ *store.Team) error {
			if e := tx.UpdateMember(m.ID, store.MemberPatch{WorktreeID: ptr((*string)(nil))}, i.b.now()); e != nil {
				return e
			}
			var e error
			ready, e = tx.GetMember(m.ID)
			return e
		})
		return ready, e
	}
	if e = gitwt.CheckVersion(ctx); errors.Is(e, gitwt.ErrGitTooOld) {
		return &m, nil
	} else if e != nil {
		return nil, e
	}
	host := i.b.host(ctx)
	plan, pe := policy.PlanWorktree(policy.WorktreeInput{Base: *m.Folder, TeamID: m.Team, TeamName: team.Name, Member: m.Name, Recorded: recorded}, host)
	if e := hostError(host); e != nil {
		return nil, e
	}
	if pe != nil {
		return nil, pe
	}
	if plan.Share {
		return &m, nil
	}
	if !plan.Reuse {
		if e = gitwt.AddWorktree(ctx, *m.Folder, plan.Path, plan.Branch); e != nil {
			return nil, e
		}
	} else if _, e = os.Stat(plan.Path); e != nil {
		return nil, model.NewError(model.ErrorLaunchFailed, "recorded worktree is missing").WithDetail("cause", e.Error())
	}
	base := "HEAD"
	if !plan.Reuse {
		probe, cancel := context.WithTimeout(ctx, 15*time.Second)
		command := exec.CommandContext(probe, "git", "-C", *m.Folder, "symbolic-ref", "--quiet", "--short", "HEAD")
		raw, err := command.Output()
		cancel()
		if err == nil {
			base = strings.TrimSpace(string(raw))
		}
	}
	var ready *model.Member
	e = r.write(ctx, &m, func(tx *store.Tx, _ *store.Team) error {
		var id string
		if plan.Reuse {
			row, e := tx.GetWorktreeByPath(plan.Path)
			if e != nil {
				return e
			}
			id = row.ID
		} else {
			var e error
			id, e = i.b.opts.ID()
			if e != nil {
				return e
			}
			if e = tx.InsertWorktree(store.WorktreeRecord{ID: id, TeamID: m.Team, MemberID: m.ID, Repo: plan.Repo, Path: plan.Path, Branch: plan.Branch, Base: base, CreatedAt: i.b.now()}); e != nil {
				return e
			}
		}
		if e := tx.UpdateMember(m.ID, store.MemberPatch{WorktreeID: ptr(&id)}, i.b.now()); e != nil {
			return e
		}
		var e error
		ready, e = tx.GetMember(m.ID)
		return e
	})
	return ready, e
}
