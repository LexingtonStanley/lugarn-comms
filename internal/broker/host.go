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
	ctx            context.Context
	b              *Broker
	harnesses      []model.HarnessInfo
	repos          map[string]gitwt.Info
	folders, paths map[string]bool
	branches       map[[2]string]bool
	err            error
}

func (b *Broker) host(ctx context.Context) policy.Host {
	if b.opts.Host != nil {
		return b.opts.Host
	}
	return &host{ctx: ctx, b: b, harnesses: b.opts.Probe.Harnesses(ctx), repos: map[string]gitwt.Info{}, folders: map[string]bool{}, paths: map[string]bool{}, branches: map[[2]string]bool{}}
}
func (*host) OS() model.HostOS { return model.HostOS(runtime.GOOS) }
func (h *host) FolderExists(p string) bool {
	if v, ok := h.folders[p]; ok {
		return v
	}
	s, e := os.Stat(p)
	v := e == nil && s.IsDir()
	if e != nil && !os.IsNotExist(e) {
		h.err = e
	}
	h.folders[p] = v
	return v
}
func (h *host) RepoRoot(p string) (string, bool) {
	v, ok := h.repos[p]
	if !ok {
		var e error
		v, e = h.b.opts.Git.RepoInfo(h.ctx, p)
		if e != nil {
			h.err = e
		}
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
func (h *host) PathExists(p string) bool {
	if v, ok := h.paths[p]; ok {
		return v
	}
	_, e := os.Lstat(p)
	v := !os.IsNotExist(e)
	if e != nil && !os.IsNotExist(e) {
		h.err = e
	}
	h.paths[p] = v
	return v
}
func (h *host) BranchExists(r, n string) bool {
	key := [2]string{r, n}
	if v, ok := h.branches[key]; ok {
		return v
	}
	v, e := h.b.opts.Git.BranchExists(h.ctx, r, n)
	if e != nil {
		h.err = e
		v = true
	}
	h.branches[key] = v
	return v
}
func hostError(h policy.Host) error {
	if real, ok := h.(*host); ok && real.err != nil {
		return model.AsError(real.err)
	}
	return nil
}
