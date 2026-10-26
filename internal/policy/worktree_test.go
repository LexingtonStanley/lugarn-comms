package policy

import (
	"strconv"
	"testing"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
)

const (
	repoRoot = "/home/lex/code/flare"
	wtPath   = "/home/lex/code/flare-lugarn/auth-rewrite-web"
	wtBranch = "lugarn/auth-rewrite/web"
)

func wtInput(recorded ...RecordedWorktree) WorktreeInput {
	return WorktreeInput{Base: repoRoot + "/src", TeamID: "t1", TeamName: "auth-rewrite", Member: "web", Recorded: recorded}
}

func wtHost() *fakeHost { return newHost(model.HostOSLinux).repo(repoRoot+"/src", repoRoot) }

func TestPlanWorktree(t *testing.T) {
	branchTaken := func(n string) func(*fakeHost) {
		return func(h *fakeHost) { h.branches[repoRoot+"|"+n] = true }
	}
	pathTaken := func(p string) func(*fakeHost) {
		return func(h *fakeHost) { h.paths[p] = true }
	}
	rec := func(team, member, path, branch string, gone bool) RecordedWorktree {
		return RecordedWorktree{Team: team, Member: member, Path: path, Branch: branch, Removed: gone}
	}
	cases := []struct {
		name     string
		host     []func(*fakeHost)
		recorded []RecordedWorktree
		path     string
		branch   string
		reuse    bool
	}{
		{name: "fresh", path: wtPath, branch: wtBranch},
		{name: "path taken on disk", host: []func(*fakeHost){pathTaken(wtPath)}, path: wtPath + "-2", branch: wtBranch + "-2"},
		{name: "branch taken: a person's worktree on the same branch elsewhere", host: []func(*fakeHost){branchTaken(wtBranch)},
			path: wtPath + "-2", branch: wtBranch + "-2"},
		{name: "path and -2 taken", host: []func(*fakeHost){pathTaken(wtPath), branchTaken(wtBranch + "-2")},
			path: wtPath + "-3", branch: wtBranch + "-3"},
		{name: "recorded for this team and member: reused", recorded: []RecordedWorktree{rec("t1", "web", wtPath, wtBranch, false)},
			host: []func(*fakeHost){pathTaken(wtPath), branchTaken(wtBranch)}, path: wtPath, branch: wtBranch, reuse: true},
		{name: "recorded at -3, -2 free: reuse -3", recorded: []RecordedWorktree{rec("t1", "web", wtPath+"-3", wtBranch+"-3", false)},
			host: []func(*fakeHost){pathTaken(wtPath), pathTaken(wtPath + "-3")}, path: wtPath + "-3", branch: wtBranch + "-3", reuse: true},
		{name: "two recorded: the lowest suffix", recorded: []RecordedWorktree{
			rec("t1", "web", wtPath+"-4", wtBranch+"-4", false), rec("t1", "web", wtPath+"-2", wtBranch+"-2", false)},
			path: wtPath + "-2", branch: wtBranch + "-2", reuse: true},
		{name: "recorded but removed: made again", recorded: []RecordedWorktree{rec("t1", "web", wtPath, wtBranch, true)},
			path: wtPath, branch: wtBranch},
		{name: "recorded for another member at the path: taken", recorded: []RecordedWorktree{rec("t1", "api", wtPath, "x", false)},
			path: wtPath + "-2", branch: wtBranch + "-2"},
		{name: "recorded for another team at the path: taken", recorded: []RecordedWorktree{rec("t2", "web", wtPath, wtBranch, false)},
			path: wtPath + "-2", branch: wtBranch + "-2"},
		{name: "this member's record at an unrelated path is not reused", recorded: []RecordedWorktree{rec("t1", "web", "/elsewhere/x", "b", false)},
			path: wtPath, branch: wtBranch},
		{name: "a lookalike suffix is not this worktree", recorded: []RecordedWorktree{
			rec("t1", "web", wtPath+"-02", "b", false), rec("t1", "web", wtPath+"-x", "b", false), rec("t1", "web", wtPath+"-+3", "b", false)},
			path: wtPath, branch: wtBranch},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			host := wtHost()
			for _, f := range c.host {
				f(host)
			}
			plan, err := PlanWorktree(wtInput(c.recorded...), host)
			requireNoError(t, err)
			equal(t, "plan", plan, WorktreePlan{Repo: repoRoot, Path: c.path, Branch: c.branch, Reuse: c.reuse})
		})
	}
}

func TestPlanWorktreeEdges(t *testing.T) {
	// Outside git: share the folder.
	plan, err := PlanWorktree(wtInput(), newHost(model.HostOSLinux).dir(repoRoot+"/src"))
	requireNoError(t, err)
	equal(t, "share", plan, WorktreePlan{Share: true})

	// A repository at the filesystem root has nowhere to put worktrees.
	_, err = PlanWorktree(wtInput(), newHost(model.HostOSLinux).repo(repoRoot+"/src", "/"))
	requireCode(t, err, model.ErrorLaunchFailed)

	// A repository just under the root.
	plan, err = PlanWorktree(wtInput(), newHost(model.HostOSLinux).repo(repoRoot+"/src", "/flare"))
	requireNoError(t, err)
	equal(t, "under root", plan.Path, "/flare-lugarn/auth-rewrite-web")

	// Every candidate taken.
	host := wtHost()
	for n := 1; n <= maxWorktreeSuffix; n++ {
		p := wtPath
		if n > 1 {
			p += "-" + strconv.Itoa(n)
		}
		host.paths[p] = true
	}
	_, err = PlanWorktree(wtInput(), host)
	requireCode(t, err, model.ErrorLaunchFailed)
	host.paths[wtPath+"-1000"] = false
	plan, err = PlanWorktree(wtInput(), host)
	requireNoError(t, err)
	equal(t, "last candidate", plan.Path, wtPath+"-1000")
}

func TestPlanWorktreeWindows(t *testing.T) {
	for _, root := range []string{`C:\Users\lex\code\flare`, `C:/Users/lex/code/flare`} {
		host := newHost(model.HostOSWindows).repo(`C:\Users\lex\code\flare\src`, root)
		in := WorktreeInput{Base: `C:\Users\lex\code\flare\src`, TeamID: "t1", TeamName: "auth-rewrite", Member: "web"}
		plan, err := PlanWorktree(in, host)
		requireNoError(t, err)
		equal(t, "plan", plan, WorktreePlan{Repo: `C:\Users\lex\code\flare`,
			Path: `C:\Users\lex\code\flare-lugarn\auth-rewrite-web`, Branch: "lugarn/auth-rewrite/web"})

		host.paths[`C:\Users\lex\code\flare-lugarn\auth-rewrite-web`] = true
		in.Recorded = []RecordedWorktree{{Team: "t1", Member: "web", Path: `C:/Users/lex/code/flare-lugarn/auth-rewrite-web-3`, Branch: "lugarn/auth-rewrite/web-3"}}
		plan, err = PlanWorktree(in, host)
		requireNoError(t, err)
		equal(t, "reuse, with native separators", plan, WorktreePlan{Repo: `C:\Users\lex\code\flare`,
			Path: `C:\Users\lex\code\flare-lugarn\auth-rewrite-web-3`, Branch: "lugarn/auth-rewrite/web-3", Reuse: true})
	}
	// A drive root and a UNC share root have no parent for worktrees.
	for _, root := range []string{`D:\`, `\\nas\code`} {
		_, err := PlanWorktree(WorktreeInput{Base: "b", TeamName: "t", Member: "m"}, newHost(model.HostOSWindows).repo("b", root))
		requireCode(t, err, model.ErrorLaunchFailed)
	}
	plan, err := PlanWorktree(WorktreeInput{Base: "b", TeamName: "t", Member: "m"}, newHost(model.HostOSWindows).repo("b", `\\nas\code\flare`))
	requireNoError(t, err)
	equal(t, "UNC", plan.Path, `\\nas\code\flare-lugarn\t-m`)
}

func TestWorktreeSuffix(t *testing.T) {
	for p, want := range map[string]int{
		"/a/t-web": 1, "/a/t-web-2": 2, "/a/t-web-17": 17, "/a/t-web-1": 0, "/a/t-web-0": 0, "/a/t-web-02": 0,
		"/a/t-web-": 0, "/a/t-web-x": 0, "/a/t-web-+2": 0, "/a/t-web2": 0, "/a/t-webb": 0, "/a/t-we": 0,
	} {
		if got := worktreeSuffix(p, "/a/t-web"); got != want {
			t.Errorf("worktreeSuffix(%q) = %d; want %d", p, got, want)
		}
	}
}
