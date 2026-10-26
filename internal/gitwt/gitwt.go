// Package gitwt manages broker-created worktrees. Store ownership and member
// state checks belong to the caller; this package checks the filesystem and git.
package gitwt

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// GitState is the protocol enum, currently absent from internal/model.
type GitState string

const (
	GitStateMissing GitState = "missing"
	GitStateClean   GitState = "clean"
	GitStateDirty   GitState = "dirty"
)

// BranchOutcome is the protocol enum, currently absent from internal/model.
type BranchOutcome string

const (
	BranchOutcomeDeleted      BranchOutcome = "deleted"
	BranchOutcomeKeptUnmerged BranchOutcome = "kept_unmerged"
	BranchOutcomeMissing      BranchOutcome = "missing"
)

// WorktreeRefusal is the protocol enum, currently absent from internal/model.
type WorktreeRefusal string

const (
	WorktreeRefusalMemberNotStopped WorktreeRefusal = "member_not_stopped"
	WorktreeRefusalDirty            WorktreeRefusal = "dirty"
	WorktreeRefusalUnknownPath      WorktreeRefusal = "unknown_path"
	WorktreeRefusalGitFailed        WorktreeRefusal = "git_failed"
)

// Refusal carries a remove-worktrees refusal and the underlying git reason.
type Refusal struct {
	Reason WorktreeRefusal
	Err    error
}

func (e *Refusal) Error() string { return string(e.Reason) + ": " + e.Err.Error() }
func (e *Refusal) Unwrap() error { return e.Err }

// Info identifies the selected working tree and the repository's main tree.
type Info struct {
	InGit                  bool
	TopLevel, MainWorktree string
}

// RepoInfo returns InGit false for missing folders and folders outside git.
// Other failures, including unavailable git, are errors rather than false facts.
func RepoInfo(ctx context.Context, folder string) (Info, error) {
	folder, err := filepath.Abs(folder)
	if err != nil {
		return Info{}, err
	}
	if _, err := os.Stat(folder); errors.Is(err, os.ErrNotExist) {
		return Info{}, nil
	} else if err != nil {
		return Info{}, err
	}
	out, err := git(ctx, folder, "rev-parse", "--show-toplevel")
	if err != nil {
		if strings.Contains(err.Error(), "not a git repository") || strings.Contains(err.Error(), "must be run in a work tree") {
			return Info{}, nil
		}
		return Info{}, err
	}
	top := strings.TrimSuffix(string(out), "\n")
	out, err = git(ctx, folder, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return Info{}, err
	}
	return Info{InGit: true, TopLevel: filepath.Clean(top), MainWorktree: filepath.Dir(strings.TrimSuffix(string(out), "\n"))}, nil
}

// BranchExists checks a local branch, not a tag or remote-tracking ref.
func BranchExists(ctx context.Context, repo, branch string) (bool, error) {
	if err := validBranch(ctx, repo, branch); err != nil {
		return false, err
	}
	_, err := git(ctx, repo, "show-ref", "--verify", "--quiet", "--", "refs/heads/"+branch)
	if exitCode(err) == 1 {
		return false, nil
	}
	return err == nil, err
}

// PathTaken counts files, directories and dangling symbolic links as taken.
func PathTaken(path string) (bool, error) {
	_, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}

// AddWorktree creates a new branch from base's HEAD. Git atomically refuses an
// existing branch; no preflight check is relied on for this guarantee.
func AddWorktree(ctx context.Context, base, path, branch string) error {
	if err := validBranch(ctx, base, branch); err != nil {
		return err
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if taken, err := PathTaken(path); err != nil {
		return err
	} else if taken {
		return fmt.Errorf("worktree path is taken: %s", path)
	}
	_, err = git(ctx, base, "worktree", "add", "-b", branch, "--", path, "HEAD")
	return err
}

// State includes untracked files and refuses to call an existing non-repository
// clean. Missing is reserved for paths that do not exist.
func State(ctx context.Context, path string) (GitState, error) {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return GitStateMissing, nil
	} else if err != nil {
		return "", err
	}
	out, err := git(ctx, path, "status", "--porcelain", "--untracked-files=all", "--ignore-submodules=none")
	if err != nil {
		return "", err
	}
	if len(out) > 0 {
		return GitStateDirty, nil
	}
	return GitStateClean, nil
}

// Remove removes a registered linked worktree, then attempts branch deletion
// with -d only. repo must remain accessible even when path is missing. The
// caller must first verify store ownership and that the member is stopped.
func Remove(ctx context.Context, repo, path, branch string) (BranchOutcome, error) {
	outcome, err := remove(ctx, repo, path, branch)
	if err != nil {
		var refusal *Refusal
		if !errors.As(err, &refusal) {
			err = &Refusal{Reason: WorktreeRefusalGitFailed, Err: err}
		}
	}
	return outcome, err
}

func remove(ctx context.Context, repo, path, branch string) (BranchOutcome, error) {
	if err := validBranch(ctx, repo, branch); err != nil {
		return "", err
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	// Refuse a main tree, an unregistered path, or a mismatched branch before
	// removing anything. The first porcelain record is the main working tree.
	out, err := git(ctx, repo, "worktree", "list", "--porcelain", "-z")
	if err != nil {
		return "", err
	}
	found := false
	for i, record := range strings.Split(string(out), "\x00\x00") {
		fields := strings.Split(record, "\x00")
		if len(fields) == 0 || !strings.HasPrefix(fields[0], "worktree ") {
			continue
		}
		listed := filepath.FromSlash(strings.TrimPrefix(fields[0], "worktree "))
		if !samePath(listed, path) {
			continue
		}
		if i == 0 {
			return "", fmt.Errorf("refusing to remove the main working tree")
		}
		for _, field := range fields[1:] {
			if field == "branch refs/heads/"+branch {
				found = true
			}
		}
		// A deleted branch can still be recorded in the worktree's HEAD.
		if !found {
			return "", fmt.Errorf("worktree branch does not match %s", branch)
		}
	}
	if !found {
		return "", fmt.Errorf("worktree is not registered: %s", path)
	}
	state, err := State(ctx, path)
	if err != nil {
		return "", err
	}
	if state == GitStateDirty {
		return "", &Refusal{Reason: WorktreeRefusalDirty, Err: fmt.Errorf("worktree has uncommitted or untracked files")}
	}
	if state == GitStateMissing {
		_, err = git(ctx, repo, "worktree", "prune", "--expire", "now")
	} else {
		_, err = git(ctx, repo, "worktree", "remove", "--", path)
	}
	if err != nil {
		return "", err
	}
	exists, err := BranchExists(ctx, repo, branch)
	if err != nil {
		return "", err
	}
	if !exists {
		return BranchOutcomeMissing, nil
	}
	_, err = git(ctx, repo, "branch", "-d", "--", branch)
	if err == nil {
		return BranchOutcomeDeleted, nil
	}
	if exitCode(err) >= 0 {
		// The protocol calls a branch retained after -d refuses kept_unmerged.
		// Recheck for a concurrent deletion rather than claiming it still exists.
		exists, checkErr := BranchExists(ctx, repo, branch)
		if checkErr != nil {
			return "", checkErr
		}
		if !exists {
			return BranchOutcomeMissing, nil
		}
		return BranchOutcomeKeptUnmerged, nil
	}
	return "", err
}

func samePath(a, b string) bool {
	a, b = canonicalPath(a), canonicalPath(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

func canonicalPath(path string) string {
	// Resolve the closest existing ancestor, so a missing tree under macOS's
	// /var -> /private/var alias still matches git's canonical registration.
	path = filepath.Clean(path)
	var suffix []string
	for current := path; ; current = filepath.Dir(current) {
		if resolved, err := filepath.EvalSymlinks(current); err == nil {
			for i := len(suffix) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, suffix[i])
			}
			return resolved
		}
		if filepath.Dir(current) == current {
			return path
		}
		suffix = append(suffix, filepath.Base(current))
	}
}

func validBranch(ctx context.Context, repo, branch string) error {
	if branch == "" || strings.HasPrefix(branch, "-") {
		return fmt.Errorf("invalid branch name: %q", branch)
	}
	_, err := git(ctx, repo, "check-ref-format", "refs/heads/"+branch)
	return err
}

func exitCode(err error) int {
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode()
	}
	return -1
}

// All argv is fixed syntax plus host paths and validated broker branch names.
// No prompts, tasks or other agent text is accepted by this API.
func git(ctx context.Context, repo string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	repo, err := filepath.Abs(repo)
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", repo}, args...)...)
	cmd.WaitDelay = time.Second
	cmd.Env = append(os.Environ(), "LC_ALL=C", "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		return nil, fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(string(out)))
	}
	return out, nil
}
