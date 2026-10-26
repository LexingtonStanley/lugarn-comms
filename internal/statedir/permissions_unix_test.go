//go:build !windows

package statedir

import (
	"os"
	"path/filepath"
	"testing"
)

func assertPrivate(t *testing.T, path string, dir bool) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	want := os.FileMode(0600)
	if dir {
		want = 0700
	}
	if info.Mode().Perm() != want {
		t.Errorf("%s: mode %o; want %o", path, info.Mode().Perm(), want)
	}
}

func TestEnsureRejectsSymlink(t *testing.T) {
	parent := t.TempDir()
	target := filepath.Join(parent, "target")
	link := filepath.Join(parent, "link")
	if err := os.Mkdir(target, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := Ensure(link); err == nil {
		t.Fatal("symlink accepted")
	}
	dir := filepath.Join(parent, "state")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, "launch")); err != nil {
		t.Fatal(err)
	}
	if err := Ensure(dir); err == nil {
		t.Fatal("launch symlink accepted")
	}
}

func TestEnsureRefusesOtherOwner(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("changing ownership requires root; mode tests still run")
	}
	dir := filepath.Join(t.TempDir(), "state")
	if err := os.Mkdir(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(dir, 65534, -1); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chown(dir, 0, -1) })
	if err := Ensure(dir); err == nil {
		t.Fatal("another user's directory accepted")
	}
}
