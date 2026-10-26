package broker

import (
	"context"
	"os"
	"runtime"

	"github.com/LexingtonStanley/lugarn-comms/internal/gitwt"
	"github.com/LexingtonStanley/lugarn-comms/internal/model"
	"github.com/LexingtonStanley/lugarn-comms/internal/policy"
)

type realGit struct{}

func (realGit) RepoInfo(c context.Context, p string) (gitwt.Info, error) { return gitwt.RepoInfo(c, p) }
func (realGit) BranchExists(c context.Context, p, n string) (bool, error) {
	return gitwt.BranchExists(c, p, n)
}
func (realGit) State(c context.Context, p string) (gitwt.GitState, error) { return gitwt.State(c, p) }
func (realGit) Remove(c context.Context, r, p, n string) (gitwt.BranchOutcome, error) {
	return gitwt.Remove(c, r, p, n)
}

type host struct {
	ctx       context.Context
	b         *Broker
	harnesses []model.HarnessInfo
	repos     map[string]gitwt.Info
}

func (b *Broker) host(ctx context.Context) policy.Host {
	if b.opts.Host != nil {
		return b.opts.Host
	}
	return &host{ctx: ctx, b: b, harnesses: b.opts.Probe.Harnesses(ctx), repos: map[string]gitwt.Info{}}
}
func (*host) OS() model.HostOS           { return model.HostOS(runtime.GOOS) }
func (*host) FolderExists(p string) bool { s, e := os.Stat(p); return e == nil && s.IsDir() }
func (h *host) RepoRoot(p string) (string, bool) {
	v, ok := h.repos[p]
	if !ok {
		v, _ = h.b.opts.Git.RepoInfo(h.ctx, p)
		h.repos[p] = v
	}
	return v.MainWorktree, v.InGit
}
func (h *host) HarnessIssue(v model.Harness) model.HarnessIssue {
	for _, x := range h.harnesses {
		if x.Harness == v {
			if x.Issue != nil {
				return *x.Issue
			}
			if x.Supported {
				return ""
			}
		}
	}
	return model.HarnessIssueNotInstalled
}

// Capability is independent of installation; unavailable harnesses produce apply warnings.
func (*host) CanRunHeadless(v model.Harness) bool { return v.Valid() && v != model.HarnessPi }
func (*host) PathExists(p string) bool            { _, e := os.Lstat(p); return !os.IsNotExist(e) }
func (h *host) BranchExists(r, n string) bool {
	v, e := h.b.opts.Git.BranchExists(h.ctx, r, n)
	return e != nil || v
}
