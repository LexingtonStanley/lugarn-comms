// Package hostprobe inspects host capabilities without starting services.
package hostprobe

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
)

// HostInfo is the host identity supplied by the OS, independent of the store.
type HostInfo struct {
	OS       model.HostOS   `json:"os"`
	Arch     model.HostArch `json:"arch"`
	Hostname string         `json:"hostname"`
}

// LogindInfo is absent from model; it describes Linux logout behavior.
type LogindInfo struct {
	KillUserProcesses bool `json:"kill_user_processes"`
	Linger            bool `json:"linger"`
}

// Probe lets service and policy callers replace all host inspection in tests.
type Probe interface {
	Host() (HostInfo, error)
	Harnesses(context.Context) []model.HarnessInfo
	Herdr(context.Context, string) (model.HerdrInfo, error)
	Logind(context.Context) *LogindInfo
	CanRunHeadless(model.Harness) bool
	Binary(model.Harness) (string, error)
}

// Real caches harness results for sixty seconds. Its zero value is ready to use.
type Real struct {
	mu        sync.Mutex
	expires   time.Time
	harnesses []model.HarnessInfo
}

var _ Probe = (*Real)(nil)

// New returns a real host probe.
func New() *Real { return &Real{} }

func (*Real) Host() (HostInfo, error) {
	hostname, err := os.Hostname()
	info := HostInfo{OS: model.HostOS(runtime.GOOS), Arch: model.HostArch(runtime.GOARCH), Hostname: hostname}
	if err == nil && (!info.OS.Valid() || !info.Arch.Valid()) {
		err = fmt.Errorf("unsupported host: %s/%s", info.OS, info.Arch)
	}
	return info, err
}

type version struct {
	major, minor, patch int
	prerelease          bool
}

var versionRE = regexp.MustCompile(`(?:^|[^0-9A-Za-z]|v)([0-9]+)\.([0-9]+)\.([0-9]+)(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?`)

// Initial driver compatibility only; C2 will tighten this table after testing.
var supported = map[model.Harness]func(version) bool{
	// Claude Code's verified flags are from the 2.1 line.
	model.HarnessClaude: func(v version) bool { return v.major == 2 && v.minor == 1 },
	// Codex's verified exec interface spans 0.163 and 0.164.
	model.HarnessCodex: func(v version) bool { return v.major == 0 && v.minor >= 163 && v.minor <= 164 },
	// Both OpenCode major versions are explicitly allowed by protocol v1.
	model.HarnessOpenCode: func(v version) bool { return v.major == 1 || v.major == 2 },
	// pi remains CLI-only; its version does not restrict availability.
	model.HarnessPi: func(version) bool { return true },
}

func parseVersion(s string) (string, version, bool) {
	m := versionRE.FindStringSubmatch(s)
	if m == nil {
		return "", version{}, false
	}
	var v version
	fields := []*int{&v.major, &v.minor, &v.patch}
	for i, field := range fields {
		n, err := strconv.Atoi(m[i+1])
		if err != nil {
			return "", version{}, false
		}
		*field = n
	}
	v.prerelease = m[4] != ""
	return m[1] + "." + m[2] + "." + m[3] + m[4] + m[5], v, true
}

func (p *Real) Harnesses(ctx context.Context) []model.HarnessInfo {
	p.mu.Lock()
	defer p.mu.Unlock()
	if time.Now().Before(p.expires) {
		return cloneHarnesses(p.harnesses)
	}
	results := make([]model.HarnessInfo, 0, 4)
	for _, h := range (model.Harness("")).Values() {
		info := model.HarnessInfo{Harness: h}
		binary, shim, err := findBinary(h, runtime.GOOS, runtime.GOARCH)
		issue := model.HarnessIssueNotInstalled
		if shim {
			info.Installed = true
			issue = model.HarnessIssueShimOnly
		}
		if err == nil {
			info.Installed = true
			issue = model.HarnessIssueUnsupportedVersion
			out, err := run(ctx, binary, "--version")
			if err == nil {
				name, v, ok := parseVersion(string(out))
				if h == model.HarnessPi && !ok {
					name = strings.TrimSpace(string(out))
					ok = name != ""
				}
				if ok {
					info.Version = &name
					info.Supported = (h == model.HarnessPi || !v.prerelease) && supported[h](v)
				}
			}
		}
		if !info.Supported {
			info.Issue = &issue
		}
		info.Headless = info.Supported && h != model.HarnessPi
		results = append(results, info)
	}
	// An interrupted lookup must not poison the next caller's cache.
	if ctx.Err() == nil {
		p.harnesses = results
		p.expires = time.Now().Add(60 * time.Second)
	}
	return cloneHarnesses(results)
}

func cloneHarnesses(in []model.HarnessInfo) []model.HarnessInfo {
	out := append([]model.HarnessInfo(nil), in...)
	for i := range out {
		if out[i].Version != nil {
			value := *out[i].Version
			out[i].Version = &value
		}
		if out[i].Issue != nil {
			value := *out[i].Issue
			out[i].Issue = &value
		}
	}
	return out
}

// CanRunHeadless includes installation and version compatibility.
func (p *Real) CanRunHeadless(h model.Harness) bool {
	if !h.Valid() || h == model.HarnessPi {
		return false
	}
	for _, info := range p.Harnesses(context.Background()) {
		if info.Harness == h {
			return info.Headless
		}
	}
	return false
}

// Binary resolves an executable without running it. Windows never returns a shim.
// Callers must also check Harnesses for driver compatibility before launching.
func (*Real) Binary(h model.Harness) (string, error) {
	path, shim, err := findBinary(h, runtime.GOOS, runtime.GOARCH)
	if shim {
		return "", fmt.Errorf("%s: shim_only", h)
	}
	return path, err
}

// Herdr probes only version and status. An empty session selects default. The
// session is explicit even for default, so HERDR_SESSION cannot change it.
func (*Real) Herdr(ctx context.Context, session string) (model.HerdrInfo, error) {
	if session == "" {
		session = model.DefaultHerdrServer
	}
	info := model.HerdrInfo{Server: session}
	binary, err := exec.LookPath("herdr")
	if err != nil {
		return info, nil
	}
	info.Installed = true
	out, err := run(ctx, binary, "--session", session, "--version")
	if err != nil {
		return info, err
	}
	name, _, ok := parseVersion(string(out))
	if !ok {
		return info, fmt.Errorf("unrecognized Herdr version: %q", out)
	}
	info.Version = &name
	out, err = run(ctx, binary, "--session", session, "status", "server", "--json")
	if err != nil {
		return info, err
	}
	var status struct {
		Running *bool `json:"running"`
	}
	if err := json.Unmarshal(out, &status); err != nil {
		return info, err
	}
	if status.Running == nil {
		return info, fmt.Errorf("Herdr status is missing running")
	}
	info.Running = *status.Running
	return info, nil
}

func (*Real) Logind(ctx context.Context) *LogindInfo {
	if runtime.GOOS != "linux" {
		return nil
	}
	binary, err := exec.LookPath("loginctl")
	if err != nil {
		return nil
	}
	u, err := user.Current()
	if err != nil {
		return nil
	}
	kill, err := run(ctx, binary, "show", "-p", "KillUserProcesses", "--value")
	if err != nil {
		return nil
	}
	linger, err := run(ctx, binary, "show-user", u.Uid, "-p", "Linger", "--value")
	if err != nil {
		return nil
	}
	k, ok := parseBool(kill)
	if !ok {
		return nil
	}
	l, ok := parseBool(linger)
	if !ok {
		return nil
	}
	return &LogindInfo{KillUserProcesses: k, Linger: l}
}

func parseBool(s []byte) (bool, bool) {
	switch strings.TrimSpace(string(s)) {
	case "yes", "true", "1":
		return true, true
	case "no", "false", "0":
		return false, true
	default:
		return false, false
	}
}

// A bounded writer avoids unlimited memory use from a broken --version command.
type output []byte

func (b *output) Write(p []byte) (int, error) {
	n := len(p)
	if left := 64*1024 - len(*b); left > 0 {
		if len(p) > left {
			p = p[:left]
		}
		*b = append(*b, p...)
	}
	return n, nil
}

func run(ctx context.Context, binary string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.WaitDelay = 100 * time.Millisecond
	var stdout, stderr output
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("%s: %w: %s", filepath.Base(binary), err, strings.TrimSpace(string(stderr)))
	}
	return stdout, nil
}
