package hostprobe

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestLoginPathCapture(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no login capture on Windows")
	}
	dir, _ := fixture(t, "shell", "csh")
	shell := filepath.Join(dir, "shell")
	for _, tc := range []struct{ mode, issue string }{{"noise", ""}, {"sleep", "timeout"}, {"exit", "failed"}, {"missing", "failed"}} {
		t.Run(tc.mode, func(t *testing.T) {
			t.Setenv("LUGARN_PATH_FAKE", tc.mode)
			p := capturePath(context.Background(), shell, "", 100*time.Millisecond)
			if tc.issue == "" {
				if p.Login == nil || *p.Login != "/login/bin:/usr/bin" {
					t.Fatal(p)
				}
			} else if p.Issue == nil || *p.Issue != tc.issue {
				t.Fatal(p)
			}
		})
	}
	t.Setenv("LUGARN_PATH_FAKE", "escapes")
	if p := capturePath(context.Background(), shell, "", time.Second); p.Login == nil || *p.Login != "]0;title/login/bin:[0m/usr/bin" {
		t.Fatalf("control characters survived: %+v", p)
	}
	t.Setenv("LUGARN_PATH_FAKE", "env")
	t.Setenv("TERM", "xterm-256color")
	if p := capturePath(context.Background(), shell, "", time.Second); p.Login == nil || *p.Login != "1:dumb" {
		t.Fatalf("capture environment: %+v", p)
	}
	p := capturePath(context.Background(), filepath.Join(dir, "csh"), "", time.Second)
	if p.Issue == nil || *p.Issue != "unsupported_shell" {
		t.Fatal(p)
	}
}
func TestLoginPathMergeAndCache(t *testing.T) {
	dir := t.TempDir()
	abs := filepath.Join(dir, "one")
	sep := string(os.PathListSeparator)
	if got := MergePath("relative"+sep+abs+sep+sep+abs, dir); got != abs+sep+dir {
		t.Fatal(got)
	}
	if runtime.GOOS == "windows" {
		return
	}
	shellDir, _ := fixture(t, "shell")
	t.Setenv("SHELL", filepath.Join(shellDir, "shell"))
	t.Setenv("LUGARN_PATH_FAKE", "noise")
	first, e := LoginPath(context.Background(), dir, false)
	if e != nil || first.Login == nil {
		t.Fatal(first, e)
	}
	t.Setenv("LUGARN_PATH_FAKE", "exit")
	cached, e := LoginPath(context.Background(), dir, false)
	if e != nil || cached.Login == nil || !cached.CapturedAt.Equal(*first.CapturedAt) {
		t.Fatal(cached, e)
	}
	old := time.Now().Add(-time.Hour - time.Second)
	cached.CapturedAt = &old
	raw, _ := json.Marshal(cached)
	if e = os.WriteFile(filepath.Join(dir, "login-path.json"), raw, 0600); e != nil {
		t.Fatal(e)
	}
	stale, e := LoginPath(context.Background(), dir, false)
	if e != nil || stale.Issue == nil || *stale.Issue != "failed" {
		t.Fatal(stale, e)
	}
	if strings.Contains(stale.Merged(), "relative") {
		t.Fatal(stale)
	}
}
