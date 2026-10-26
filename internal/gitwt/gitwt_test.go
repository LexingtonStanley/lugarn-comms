package gitwt

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testGit(t *testing.T, repo string, args ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", repo}, args...)...)
	cmd.Env = append(os.Environ(), "LC_ALL=C", "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func newRepo(t *testing.T) string {
	t.Helper()
	parent := t.TempDir()
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(parent, "no-global-config"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_AUTHOR_NAME", "Worktree Test")
	t.Setenv("GIT_AUTHOR_EMAIL", "test@example.invalid")
	t.Setenv("GIT_COMMITTER_NAME", "Worktree Test")
	t.Setenv("GIT_COMMITTER_EMAIL", "test@example.invalid")
	repo := filepath.Join(parent, "repo with spaces")
	if err := os.Mkdir(repo, 0700); err != nil {
		t.Fatal(err)
	}
	testGit(t, repo, "init", "-b", "main")
	testGit(t, repo, "config", "user.name", "Worktree Test")
	testGit(t, repo, "config", "user.email", "test@example.invalid")
	testGit(t, repo, "config", "commit.gpgsign", "false")
	write(t, filepath.Join(repo, "tracked.txt"), "initial\n")
	testGit(t, repo, "add", "tracked.txt")
	testGit(t, repo, "commit", "-m", "initial")
	return repo
}

func write(t *testing.T, path, value string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(value), 0600); err != nil {
		t.Fatal(err)
	}
}

func add(t *testing.T, repo, name string) string {
	t.Helper()
	path := filepath.Join(filepath.Dir(repo), name+" worktree")
	if err := AddWorktree(context.Background(), repo, path, "lugarn/test/"+name); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestParseGitVersion(t *testing.T) {
	for _, tc := range []struct {
		output string
		want   gitVersion
	}{
		{output: "git version 2.20.1\n", want: gitVersion{major: 2, minor: 20}},
		{output: "git version 2.39.3 (Apple Git-145)\n", want: gitVersion{major: 2, minor: 39}},
		{output: "git version 2.45.1.windows.1\n", want: gitVersion{major: 2, minor: 45}},
		{output: "git version 3.0\n", want: gitVersion{major: 3, minor: 0}},
	} {
		t.Run(strings.TrimSpace(tc.output), func(t *testing.T) {
			got, err := parseGitVersion(tc.output)
			if err != nil || got != tc.want {
				t.Fatalf("parseGitVersion(%q) = %+v, %v; want %+v", tc.output, got, err, tc.want)
			}
		})
	}
	for _, output := range []string{"", "git version", "git version 2", "git version two.twenty", "not git version 2.20.1", "git version -2.20.1"} {
		t.Run("invalid_"+strings.ReplaceAll(output, " ", "_"), func(t *testing.T) {
			if _, err := parseGitVersion(output); err == nil {
				t.Fatalf("parseGitVersion(%q) accepted invalid output", output)
			}
		})
	}
}

func TestCheckGitVersion(t *testing.T) {
	for _, output := range []string{"git version 2.20", "git version 2.20.1", "git version 2.39.3 (Apple Git-145)", "git version 2.45.1.windows.1", "git version 3.0.0"} {
		if err := checkGitVersion(output); err != nil {
			t.Errorf("checkGitVersion(%q): %v", output, err)
		}
	}
	for _, output := range []string{"git version 1.9.5", "git version 2.19.3"} {
		if err := checkGitVersion(output); !errors.Is(err, ErrGitTooOld) {
			t.Errorf("checkGitVersion(%q) = %v; want ErrGitTooOld", output, err)
		}
	}
}

func TestCheckVersion(t *testing.T) {
	if err := CheckVersion(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestRepoInfo(t *testing.T) {
	repo := newRepo(t)
	ctx := context.Background()
	path := add(t, repo, "linked")
	sub := filepath.Join(path, "sub")
	if err := os.Mkdir(sub, 0700); err != nil {
		t.Fatal(err)
	}
	for _, folder := range []string{repo, path, sub} {
		info, err := RepoInfo(ctx, folder)
		if err != nil {
			t.Fatal(err)
		}
		wantTop := repo
		if folder != repo {
			wantTop = path
		}
		if !info.InGit || !samePath(info.TopLevel, wantTop) || !samePath(info.MainWorktree, repo) {
			t.Fatalf("RepoInfo(%s): %+v", folder, info)
		}
	}
	for _, folder := range []string{t.TempDir(), filepath.Join(t.TempDir(), "missing")} {
		info, err := RepoInfo(ctx, folder)
		if err != nil || info.InGit {
			t.Fatalf("not git: %+v, %v", info, err)
		}
	}
}

func TestAddAndBranchExists(t *testing.T) {
	repo := newRepo(t)
	ctx := context.Background()
	if exists, err := BranchExists(ctx, repo, "lugarn/test/new"); err != nil || exists {
		t.Fatalf("missing branch: %v, %v", exists, err)
	}
	path := add(t, repo, "new")
	if exists, err := BranchExists(ctx, repo, "lugarn/test/new"); err != nil || !exists {
		t.Fatalf("created branch: %v, %v", exists, err)
	}
	if testGit(t, path, "rev-parse", "HEAD") != testGit(t, repo, "rev-parse", "HEAD") {
		t.Fatal("not branched from base HEAD")
	}
	other := filepath.Join(filepath.Dir(repo), "other")
	if err := AddWorktree(ctx, repo, other, "lugarn/test/new"); err == nil {
		t.Fatal("existing branch accepted")
	}
	if taken, err := PathTaken(other); err != nil || taken {
		t.Fatalf("failed add left path: %v, %v", taken, err)
	}
	for _, branch := range []string{"-bad", "a..b", "@{-1}", "bad\nname", ""} {
		if err := AddWorktree(ctx, repo, other, branch); err == nil {
			t.Errorf("invalid branch accepted: %q", branch)
		}
	}
	empty := filepath.Join(filepath.Dir(repo), "empty")
	if err := os.Mkdir(empty, 0700); err != nil {
		t.Fatal(err)
	}
	if err := AddWorktree(ctx, repo, empty, "lugarn/test/taken"); err == nil {
		t.Fatal("existing empty directory taken over")
	}
	// Creating from a linked tree must preserve its HEAD, not the main HEAD.
	write(t, filepath.Join(path, "tracked.txt"), "linked commit\n")
	testGit(t, path, "commit", "-am", "linked commit")
	child := filepath.Join(filepath.Dir(repo), "child")
	if err := AddWorktree(ctx, path, child, "lugarn/test/child"); err != nil {
		t.Fatal(err)
	}
	if testGit(t, child, "rev-parse", "HEAD") != testGit(t, path, "rev-parse", "HEAD") {
		t.Fatal("wrong base HEAD")
	}
}

func TestPathTaken(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "file")
	write(t, file, "data")
	for _, path := range []string{dir, file} {
		if taken, err := PathTaken(path); err != nil || !taken {
			t.Errorf("taken(%s): %v, %v", path, taken, err)
		}
	}
	if taken, err := PathTaken(filepath.Join(dir, "missing")); err != nil || taken {
		t.Errorf("missing: %v, %v", taken, err)
	}
	link := filepath.Join(dir, "dangling")
	if err := os.Symlink(filepath.Join(dir, "absent"), link); err != nil {
		t.Logf("symlink unavailable on this host: %v", err)
	} else if taken, err := PathTaken(link); err != nil || !taken {
		t.Errorf("dangling symlink: %v, %v", taken, err)
	}
}

func TestState(t *testing.T) {
	repo := newRepo(t)
	path := add(t, repo, "states")
	ctx := context.Background()
	check := func(want GitState) {
		t.Helper()
		got, err := State(ctx, path)
		if err != nil || got != want {
			t.Fatalf("State = %s, %v; want %s", got, err, want)
		}
	}
	check(GitStateClean)
	write(t, filepath.Join(path, "tracked.txt"), "changed\n")
	check(GitStateDirty)
	testGit(t, path, "restore", "tracked.txt")
	check(GitStateClean)
	write(t, filepath.Join(path, "untracked.txt"), "new\n")
	check(GitStateDirty)
	if err := os.RemoveAll(path); err != nil {
		t.Fatal(err)
	}
	check(GitStateMissing)
	if _, err := State(ctx, t.TempDir()); err == nil {
		t.Fatal("non-repository reported clean")
	}
}

func TestRemoveRefusesDirty(t *testing.T) {
	for _, tracked := range []bool{true, false} {
		t.Run(map[bool]string{true: "tracked", false: "untracked"}[tracked], func(t *testing.T) {
			repo := newRepo(t)
			path := add(t, repo, "dirty")
			name := "new.txt"
			if tracked {
				name = "tracked.txt"
			}
			write(t, filepath.Join(path, name), "work\n")
			_, err := Remove(context.Background(), repo, path, "lugarn/test/dirty")
			var refusal *Refusal
			if !errors.As(err, &refusal) || refusal.Reason != WorktreeRefusalDirty {
				t.Fatalf("refusal: %v", err)
			}
			if _, err := os.Stat(path); err != nil {
				t.Fatalf("dirty worktree removed: %v", err)
			}
			if exists, err := BranchExists(context.Background(), repo, "lugarn/test/dirty"); err != nil || !exists {
				t.Fatalf("dirty branch lost: %v, %v", exists, err)
			}
		})
	}
}

func TestRemoveMergedAndMissingDirectory(t *testing.T) {
	for _, missing := range []bool{false, true} {
		t.Run(map[bool]string{true: "missing", false: "merged"}[missing], func(t *testing.T) {
			repo := newRepo(t)
			path := add(t, repo, "remove")
			if missing {
				if err := os.RemoveAll(path); err != nil {
					t.Fatal(err)
				}
			} else {
				write(t, filepath.Join(path, "tracked.txt"), "merged change\n")
				testGit(t, path, "commit", "-am", "change")
				testGit(t, repo, "merge", "--ff-only", "lugarn/test/remove")
			}
			outcome, err := Remove(context.Background(), repo, path, "lugarn/test/remove")
			if err != nil || outcome != BranchOutcomeDeleted {
				t.Fatalf("Remove: %s, %v", outcome, err)
			}
			if taken, err := PathTaken(path); err != nil || taken {
				t.Fatalf("path retained: %v, %v", taken, err)
			}
			if strings.Contains(testGit(t, repo, "worktree", "list", "--porcelain"), "lugarn/test/remove") {
				t.Fatal("worktree registration not removed")
			}
		})
	}
}

func TestRemoveOnlySelectedMissingWorktree(t *testing.T) {
	repo := newRepo(t)
	selected := add(t, repo, "selected")
	other := add(t, repo, "other")
	for _, path := range []string{selected, other} {
		if err := os.RemoveAll(path); err != nil {
			t.Fatal(err)
		}
	}
	outcome, err := Remove(context.Background(), repo, selected, "lugarn/test/selected")
	if err != nil || outcome != BranchOutcomeDeleted {
		t.Fatalf("Remove: %s, %v", outcome, err)
	}
	listed := testGit(t, repo, "worktree", "list", "--porcelain")
	if strings.Contains(listed, "branch refs/heads/lugarn/test/selected") {
		t.Fatal("selected worktree is still registered")
	}
	if !strings.Contains(listed, "branch refs/heads/lugarn/test/other") {
		t.Fatalf("other missing worktree registration was removed: %s", listed)
	}
	if exists, err := BranchExists(context.Background(), repo, "lugarn/test/other"); err != nil || !exists {
		t.Fatalf("other branch lost: %v, %v", exists, err)
	}
}

func TestRemoveKeepsUnmergedBranch(t *testing.T) {
	repo := newRepo(t)
	path := add(t, repo, "unmerged")
	write(t, filepath.Join(path, "tracked.txt"), "unmerged commit\n")
	testGit(t, path, "commit", "-am", "unmerged")
	outcome, err := Remove(context.Background(), repo, path, "lugarn/test/unmerged")
	if err != nil || outcome != BranchOutcomeKeptUnmerged {
		t.Fatalf("Remove: %s, %v", outcome, err)
	}
	if exists, err := BranchExists(context.Background(), repo, "lugarn/test/unmerged"); err != nil || !exists {
		t.Fatalf("unmerged branch lost: %v, %v", exists, err)
	}
	if taken, err := PathTaken(path); err != nil || taken {
		t.Fatalf("worktree retained: %v, %v", taken, err)
	}
}

func TestRemoveMissingBranch(t *testing.T) {
	repo := newRepo(t)
	path := add(t, repo, "gone")
	// Delete only the ref; the worktree still records this branch in its HEAD.
	testGit(t, repo, "update-ref", "-d", "refs/heads/lugarn/test/gone")
	if err := os.RemoveAll(path); err != nil {
		t.Fatal(err)
	}
	outcome, err := Remove(context.Background(), repo, path, "lugarn/test/gone")
	if err != nil || outcome != BranchOutcomeMissing {
		t.Fatalf("Remove: %s, %v", outcome, err)
	}
}

func TestRemoveRejectsWrongBranchAndMainTree(t *testing.T) {
	repo := newRepo(t)
	path := add(t, repo, "owned")
	for _, tc := range []struct{ path, branch string }{{path, "main"}, {repo, "main"}, {t.TempDir(), "main"}} {
		_, err := Remove(context.Background(), repo, tc.path, tc.branch)
		var refusal *Refusal
		if !errors.As(err, &refusal) || refusal.Reason != WorktreeRefusalGitFailed {
			t.Errorf("Remove(%s, %s): %v", tc.path, tc.branch, err)
		}
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("wrong-branch request removed worktree")
	}
}

func TestRemoveMissingThroughDirectoryAlias(t *testing.T) {
	repo := newRepo(t)
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(filepath.Dir(repo), alias); err != nil {
		t.Skipf("directory symlink unavailable: %v", err)
	}
	path := add(t, repo, "aliased")
	if err := os.RemoveAll(path); err != nil {
		t.Fatal(err)
	}
	aliasedPath := filepath.Join(alias, filepath.Base(path))
	outcome, err := Remove(context.Background(), repo, aliasedPath, "lugarn/test/aliased")
	if err != nil || outcome != BranchOutcomeDeleted {
		t.Fatalf("Remove through alias: %s, %v", outcome, err)
	}
}

func TestRemoveLockedTreeWithoutForce(t *testing.T) {
	repo := newRepo(t)
	path := add(t, repo, "locked")
	testGit(t, repo, "worktree", "lock", path)
	_, err := Remove(context.Background(), repo, path, "lugarn/test/locked")
	var refusal *Refusal
	if !errors.As(err, &refusal) || refusal.Reason != WorktreeRefusalGitFailed {
		t.Fatalf("locked refusal: %v", err)
	}
	if taken, err := PathTaken(path); err != nil || !taken {
		t.Fatalf("locked tree removed: %v, %v", taken, err)
	}
}
