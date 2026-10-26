package policy

import "github.com/LexingtonStanley/lugarn-comms/internal/model"

// Host answers the questions policy has about the machine the broker runs on.
// The service layer implements it over the real filesystem, git and the
// harness probes; tests use a fake. Every method must be deterministic for the
// length of one decision.
type Host interface {
	// OS is the host's operating system. It decides what an absolute path is
	// and which separator new paths use, so Windows paths work when the broker
	// runs on Windows whatever the build machine was.
	OS() model.HostOS
	// FolderExists reports whether path is an existing directory.
	FolderExists(path string) bool
	// RepoRoot returns the top level of the git repository that contains path
	// (what `git rev-parse --show-toplevel` prints), and false when path is
	// outside git or doesn't exist.
	RepoRoot(path string) (root string, ok bool)
	// HarnessIssue returns why harness h can't run on this host:
	// not_installed, unsupported_version or shim_only. It returns "" when h is
	// installed at a supported version.
	HarnessIssue(h model.Harness) model.HarnessIssue
	// PathExists reports whether anything (file, directory, link) is at path.
	PathExists(path string) bool
	// BranchExists reports whether the repository whose top level is root has
	// a local branch with this name.
	BranchExists(root, branch string) bool
}
