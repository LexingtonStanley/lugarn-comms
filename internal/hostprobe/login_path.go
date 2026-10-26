package hostprobe

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/LexingtonStanley/lugarn-comms/internal/statedir"
)

// PathInfo describes a login-shell capture without changing provider configuration.
// Merged is internal to execution; the wire reports the capture and inherited PATH.
type PathInfo struct {
	Shell      *string    `json:"shell"`
	Login      *string    `json:"login"`
	Issue      *string    `json:"issue"`
	CapturedAt *time.Time `json:"captured_at"`
	Inherited  string     `json:"inherited"`
}

const pathBegin = "__LUGARN_PATH_BEGIN__"
const pathEnd = "__LUGARN_PATH_END__"

// MergePath keeps only absolute, nonempty, unique entries in precedence order.
func MergePath(paths ...string) string {
	seen := map[string]bool{}
	var out []string
	for _, path := range paths {
		for _, p := range filepath.SplitList(path) {
			if !filepath.IsAbs(p) {
				continue
			}
			key := filepath.Clean(p)
			if runtime.GOOS == "windows" {
				key = strings.ToLower(key)
			}
			if !seen[key] {
				seen[key] = true
				out = append(out, p)
			}
		}
	}
	return strings.Join(out, string(os.PathListSeparator))
}
func (p PathInfo) Merged() string {
	if runtime.GOOS == "windows" {
		return p.Inherited
	}
	login := ""
	if p.Login != nil {
		login = *p.Login
	}
	return MergePath(login, p.Inherited, "/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin")
}

// LoginPath uses a capture under an hour old unless fresh is requested. The
// caller owns the state dir and must have ensured it before calling. No file
// other than this cache is read. Failure is retained as an issue, not an error.
func LoginPath(ctx context.Context, dir string, fresh bool) (PathInfo, error) {
	inherited := os.Getenv("PATH")
	if runtime.GOOS == "windows" {
		return PathInfo{Inherited: inherited}, nil
	}
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}
	path := filepath.Join(dir, "login-path.json")
	if !fresh {
		if raw, e := os.ReadFile(path); e == nil {
			var p PathInfo
			if json.Unmarshal(raw, &p) == nil && p.Shell != nil && *p.Shell == shell && p.CapturedAt != nil && time.Since(*p.CapturedAt) >= 0 && time.Since(*p.CapturedAt) < time.Hour {
				p.Inherited = inherited
				return p, nil
			}
		}
	}
	p := capturePath(ctx, shell, inherited, 10*time.Second)
	raw, e := json.Marshal(p)
	if e != nil {
		return p, e
	}
	return p, statedir.WriteSecret(path, raw)
}
func capturePath(ctx context.Context, shell, inherited string, timeout time.Duration) PathInfo {
	now := time.Now().UTC()
	p := PathInfo{Shell: &shell, CapturedAt: &now, Inherited: inherited}
	issue := func(s string) PathInfo { p.Issue = &s; return p }
	switch strings.TrimSuffix(strings.ToLower(filepath.Base(shell)), ".exe") {
	case "csh", "tcsh":
		return issue("unsupported_shell")
	}
	script := "printf '\\n" + pathBegin + "\\n%s\\n" + pathEnd + "\\n' \"$PATH\""
	if filepath.Base(shell) == "fish" {
		script = "printf '\\n" + pathBegin + "\\n%s\\n" + pathEnd + "\\n' (string join : $PATH)"
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, shell, "-l", "-i", "-c", script)
	// Startup files can skip what doesn't belong in a capture (an auto-started
	// tmux, an update prompt) by testing LUGARN_PATH_CAPTURE. os/exec keeps the
	// last value of a duplicated key, so these override the inherited ones.
	cmd.Env = append(os.Environ(), "LUGARN_PATH_CAPTURE=1", "TERM=dumb")
	setupCapture(cmd)
	defer finishCapture(cmd)
	var out output
	cmd.Stdout = &out
	cmd.WaitDelay = 100 * time.Millisecond
	if e := cmd.Run(); e != nil {
		if ctx.Err() != nil {
			return issue("timeout")
		}
		return issue("failed")
	}
	// Kill any surviving descendants in the capture's process group too.
	finishCapture(cmd)
	begin := []byte("\n" + pathBegin + "\n")
	end := []byte("\n" + pathEnd + "\n")
	_, tail, ok := bytes.Cut(out, begin)
	if !ok {
		return issue("failed")
	}
	value, _, ok := bytes.Cut(tail, end)
	if !ok {
		return issue("failed")
	}
	// Startup files may print escapes onto the value's line; none of a PATH's
	// characters may be a control character.
	login := strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) {
			return -1
		}
		return r
	}, string(value))
	p.Login = &login
	return p
}
