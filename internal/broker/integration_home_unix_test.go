//go:build !windows

package broker

import (
	"os"
	"path/filepath"
	"testing"
)

var startupPaths = []string{".zshenv", ".zprofile", ".zshrc", ".zlogin", ".bash_profile", ".bashrc", ".profile", ".config/fish/config.fish"}

func TestShellHomePrivateEmptyAndIdempotent(t *testing.T) {
	state := t.TempDir()
	must(t, ensureShellHome(state))
	home := filepath.Join(state, "shell-home")
	for _, relative := range startupPaths {
		must(t, os.WriteFile(filepath.Join(home, relative), []byte("read answer"), 0644))
	}
	must(t, os.Chmod(home, 0755))
	must(t, ensureShellHome(state))
	must(t, ensureShellHome(state))
	for _, relative := range append([]string{"", ".config", ".config/fish"}, startupPaths...) {
		path := filepath.Join(home, relative)
		info, e := os.Lstat(path)
		must(t, e)
		want := os.FileMode(0600)
		if info.IsDir() {
			want = 0700
		} else if info.Size() != 0 {
			t.Fatal("nonempty startup file", path)
		}
		if info.Mode().Perm() != want {
			t.Fatal(path, info.Mode())
		}
	}
}
func TestShellHomeRefusesLinks(t *testing.T) {
	for _, relative := range append([]string{"", ".config", ".config/fish"}, startupPaths...) {
		t.Run(relative, func(t *testing.T) {
			state := t.TempDir()
			must(t, ensureShellHome(state))
			path := filepath.Join(state, "shell-home", relative)
			must(t, os.RemoveAll(path))
			outside := t.TempDir()
			sentinel := filepath.Join(outside, "sentinel")
			must(t, os.WriteFile(sentinel, []byte("untouched"), 0644))
			target := sentinel
			if relative == "" || relative == ".config" || relative == ".config/fish" {
				target = outside
			}
			must(t, os.Symlink(target, path))
			if e := ensureShellHome(state); e == nil {
				t.Fatal("accepted symlink", path)
			}
			raw, e := os.ReadFile(sentinel)
			must(t, e)
			info, e := os.Stat(sentinel)
			must(t, e)
			if string(raw) != "untouched" || info.Mode().Perm() != 0644 {
				t.Fatal("modified symlink target")
			}
			entries, e := os.ReadDir(outside)
			must(t, e)
			if len(entries) != 1 {
				t.Fatal("wrote through linked directory")
			}
		})
	}
}
func TestShellHomeRefusesHardLinksAndDirectories(t *testing.T) {
	for _, kind := range []string{"hardlink", "directory"} {
		t.Run(kind, func(t *testing.T) {
			state := t.TempDir()
			must(t, ensureShellHome(state))
			path := filepath.Join(state, "shell-home", ".zshrc")
			must(t, os.Remove(path))
			target := filepath.Join(t.TempDir(), "rc")
			must(t, os.WriteFile(target, []byte("untouched"), 0644))
			if kind == "hardlink" {
				must(t, os.Link(target, path))
			} else {
				must(t, os.Mkdir(path, 0700))
			}
			if e := ensureShellHome(state); e == nil {
				t.Fatal("accepted", kind)
			}
			raw, e := os.ReadFile(target)
			must(t, e)
			if string(raw) != "untouched" {
				t.Fatal("modified hardlink target")
			}
		})
	}
}
func TestRunnerRestoresStartupSelectorsFromPlacement(t *testing.T) {
	for _, value := range []string{"unset", "", "/tmp/real home/'$() 日本語"} {
		t.Run(value, func(t *testing.T) {
			b, _ := setup(t)
			i, e := NewIntegration(b, IntegrationOptions{})
			must(t, e)
			for _, key := range []string{"HOME", "ZDOTDIR", "XDG_CONFIG_HOME"} {
				t.Setenv(key, value)
				if value == "unset" {
					must(t, os.Unsetenv(key))
				}
			}
			original := captureShellEnvironment()
			must(t, i.savePlacement("t", runnerPlacement{Environment: original}))
			env := i.rootEnv()
			must(t, i.paneHome(env))
			for _, key := range []string{"HOME", "ZDOTDIR", "XDG_CONFIG_HOME"} {
				t.Setenv(key, env[key])
			}
			t.Setenv("LUGARN_COMMS_RESTORE_ENV", "1")
			t.Setenv("LUGARN_COMMS_HOME", b.opts.StateDir)
			t.Setenv("HERDR_PANE_ID", "owned-pane")
			must(t, RestoreRunnerEnvironment("t"))
			for _, key := range []string{"HOME", "ZDOTDIR", "XDG_CONFIG_HOME"} {
				got, set := os.LookupEnv(key)
				if value == "unset" {
					if set {
						t.Fatal(key, "should be unset")
					}
				} else if !set || got != value {
					t.Fatal(key, got, set)
				}
			}
			if os.Getenv("HERDR_PANE_ID") != "owned-pane" || os.Getenv("LUGARN_COMMS_RESTORE_ENV") != "" {
				t.Fatal("changed Herdr context or retained restore flag")
			}
		})
	}
}
