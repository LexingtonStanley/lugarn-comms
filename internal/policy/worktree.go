package policy

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
)

// maxWorktreeSuffix bounds the -2, -3… search.
const maxWorktreeSuffix = 1000

// RecordedWorktree is a row of the store's worktrees table.
type RecordedWorktree struct {
	// Team is the team id.
	Team string
	// Member is the name of the member it was made for. A worker spawned
	// again under the same name gets a new id, and reuses its worktree.
	Member  string
	Path    string
	Branch  string
	Removed bool
}

// WorktreeInput asks where a member's worktree goes.
type WorktreeInput struct {
	// Base is a spec member's folder, or a spawned worker's spawner's workdir.
	Base     string
	TeamID   string
	TeamName string
	Member   string
	// Recorded is every worktree the store recorded, removed ones included.
	Recorded []RecordedWorktree
}

// WorktreePlan says where a member runs.
type WorktreePlan struct {
	// Share: Base is outside git, so the member runs in Base itself.
	Share bool
	// Repo is the top level of Base's repository.
	Repo   string
	Path   string
	Branch string
	// Reuse: the store recorded this worktree for this team and member and
	// it hasn't been removed. Otherwise the runner creates it (branch from
	// Base's HEAD) and records it.
	Reuse bool
}

// PlanWorktree places a worktree per Worktrees: path
// <parent of the repo's top level>/<repo name>-lugarn/<team>-<member>, branch
// lugarn/<team>/<member>. A live recorded worktree of this team and member
// at that path, or at one with a -2, -3… suffix, is reused (the lowest
// suffix if several). Otherwise the first candidate that is free is chosen.
// A candidate is taken when the store records a live worktree at its path,
// anything is on disk at its path, or its branch already exists; then -2,
// -3… is appended to both. The broker never takes over a person's worktree.
func PlanWorktree(in WorktreeInput, host Host) (WorktreePlan, *model.Error) {
	os := host.OS()
	root, ok := host.RepoRoot(in.Base)
	if !ok {
		return WorktreePlan{Share: true}, nil
	}
	parent, repo, ok := splitPath(os, root)
	if !ok {
		return WorktreePlan{}, model.NewError(model.ErrorLaunchFailed,
			fmt.Sprintf("the repository at %s has no parent folder for its worktrees", root)).
			WithDetail("cause", "repository at a filesystem root")
	}
	root = nativePath(os, root)
	path := joinPath(os, parent, repo+"-lugarn", in.TeamName+"-"+in.Member)
	branch := "lugarn/" + in.TeamName + "/" + in.Member

	best := 0
	var bestRec RecordedWorktree
	livePaths := map[string]bool{}
	for _, r := range in.Recorded {
		if r.Removed {
			continue
		}
		livePaths[nativePath(os, r.Path)] = true
		if r.Team != in.TeamID || r.Member != in.Member {
			continue
		}
		if n := worktreeSuffix(nativePath(os, r.Path), path); n > 0 && (best == 0 || n < best) {
			best, bestRec = n, r
		}
	}
	if best > 0 {
		return WorktreePlan{Repo: root, Path: nativePath(os, bestRec.Path), Branch: bestRec.Branch, Reuse: true}, nil
	}

	for n := 1; n <= maxWorktreeSuffix; n++ {
		p, b := path, branch
		if n > 1 {
			suffix := "-" + strconv.Itoa(n)
			p, b = path+suffix, branch+suffix
		}
		if livePaths[p] || host.PathExists(p) || host.BranchExists(root, b) {
			continue
		}
		return WorktreePlan{Repo: root, Path: p, Branch: b}, nil
	}
	return WorktreePlan{}, model.NewError(model.ErrorLaunchFailed,
		fmt.Sprintf("no free worktree path for %s after %s-%d", in.Member, path, maxWorktreeSuffix)).
		WithDetail("cause", "worktree paths taken")
}

// worktreeSuffix returns 1 when p is base, n when p is base-n with n ≥ 2,
// and 0 otherwise.
func worktreeSuffix(p, base string) int {
	if p == base {
		return 1
	}
	rest, ok := strings.CutPrefix(p, base+"-")
	if !ok || rest == "" || rest[0] == '0' || strings.Trim(rest, "0123456789") != "" {
		return 0
	}
	n, err := strconv.Atoi(rest)
	if err != nil || n < 2 {
		return 0
	}
	return n
}
