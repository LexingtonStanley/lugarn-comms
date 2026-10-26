package hostprobe

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
)

// Copies of this Go-built executable serve as real child processes on every OS.
// There are no scripts, shell invocations or installed-tool dependencies.
func TestMain(m *testing.M) {
	if os.Getenv("LUGARN_HOSTPROBE_FAKE") == "1" {
		fakeMain()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func fakeMain() {
	name := strings.TrimSuffix(filepath.Base(os.Args[0]), ".exe")
	if name == "shell" {
		if len(os.Args) != 5 || strings.Join(os.Args[1:4], " ") != "-l -i -c" {
			os.Exit(97)
		}
		switch os.Getenv("LUGARN_PATH_FAKE") {
		case "sleep":
			time.Sleep(time.Minute)
		case "exit":
			os.Exit(7)
		case "missing":
			fmt.Print("noise")
		default:
			fmt.Print("noise\n" + pathBegin + "\n/login/bin:/usr/bin\n" + pathEnd + "\nnoise")
		}
		return
	}
	if log := os.Getenv("LUGARN_PROBE_LOG"); log != "" {
		f, err := os.OpenFile(log, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			os.Exit(90)
		}
		_ = json.NewEncoder(f).Encode(append([]string{name}, os.Args[1:]...))
		_ = f.Close()
	}
	if os.Getenv("LUGARN_PROBE_FAIL") == name {
		fmt.Fprintln(os.Stderr, "fixture failed")
		os.Exit(7)
	}
	if os.Getenv("LUGARN_PROBE_SLEEP") == name {
		time.Sleep(30 * time.Second)
	}
	if name == "herdr" {
		if len(os.Args) < 4 || os.Args[1] != "--session" || !strings.HasPrefix(os.Args[2], "lugarn-probe-") {
			os.Exit(91)
		}
		if len(os.Args) == 4 && os.Args[3] == "--version" {
			fmt.Println("herdr 0.9.5")
			return
		}
		if len(os.Args) != 6 || strings.Join(os.Args[3:], " ") != "status server --json" {
			os.Exit(92)
		}
		if status := os.Getenv("LUGARN_PROBE_STATUS"); status != "" {
			fmt.Println(status)
		} else {
			fmt.Println(`{"running":false,"status":"not_running"}`)
		}
		return
	}
	if name == "busctl" {
		if strings.Join(os.Args[1:], " ") != "get-property org.freedesktop.login1 /org/freedesktop/login1 org.freedesktop.login1.Manager KillUserProcesses" {
			os.Exit(93)
		}
		fmt.Print(os.Getenv("LUGARN_PROBE_KILL"))
		return
	}
	if name == "loginctl" {
		if strings.Join(os.Args[1:], " ") == "show-user probe-user -p Linger --value" {
			fmt.Print(os.Getenv("LUGARN_PROBE_LINGER"))
			return
		}
		os.Exit(94)
	}
	if len(os.Args) != 2 || os.Args[1] != "--version" {
		os.Exit(95)
	}
	var versions map[string]string
	if err := json.Unmarshal([]byte(os.Getenv("LUGARN_PROBE_VERSIONS")), &versions); err != nil {
		os.Exit(96)
	}
	fmt.Println(versions[name])
}

func fixture(t *testing.T, names ...string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(t.TempDir(), "calls.jsonl")
	t.Setenv("PATH", dir)
	t.Setenv("LUGARN_HOSTPROBE_FAKE", "1")
	t.Setenv("LUGARN_PROBE_LOG", log)
	t.Setenv("LUGARN_PROBE_FAIL", "")
	t.Setenv("LUGARN_PROBE_SLEEP", "")
	t.Setenv("GORACE", "atexit_sleep_ms=0")
	t.Setenv("LUGARN_PROBE_STATUS", "")
	t.Setenv("LUGARN_PROBE_KILL", "b false\n")
	t.Setenv("LUGARN_PROBE_LINGER", "yes\n")
	t.Setenv("LUGARN_PROBE_VERSIONS", `{"claude":"2.1.310 (Claude Code)","codex":"codex-cli 0.164.0","opencode":"2.1.4","pi":"0.99.0"}`)
	for _, name := range names {
		copyExecutable(t, filepath.Join(dir, executable(name)))
	}
	return dir, log
}

func executable(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}

func copyExecutable(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	source, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	in, err := os.Open(source)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	out, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0755)
	if err != nil {
		t.Fatal(err)
	}
	_, err = io.Copy(out, in)
	closeErr := out.Close()
	if err != nil {
		t.Fatal(err)
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}
}

func TestHost(t *testing.T) {
	got, err := New().Host()
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.Hostname()
	if err != nil {
		t.Fatal(err)
	}
	if got.OS != model.HostOS(runtime.GOOS) || got.Arch != model.HostArch(runtime.GOARCH) || got.Hostname != want {
		t.Fatalf("host: %+v", got)
	}
}

func TestHarnessesAndCache(t *testing.T) {
	_, log := fixture(t, "claude", "codex", "opencode", "pi")
	p := New()
	ctx := context.Background()
	got := p.Harnesses(ctx)
	if len(got) != 4 {
		t.Fatalf("harnesses: %+v", got)
	}
	for _, info := range got {
		if !info.Installed || !info.Supported || info.Issue != nil || info.Version == nil || info.Headless != (info.Harness != model.HarnessPi) {
			t.Errorf("harness: %+v", info)
		}
	}
	*got[0].Version = "mutated"
	got[0].Supported = false
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if !p.CanRunHeadless(model.HarnessClaude) || p.CanRunHeadless(model.HarnessPi) || p.CanRunHeadless("invalid") {
				t.Error("headless capability")
			}
		}()
	}
	wg.Wait()
	if *p.Harnesses(ctx)[0].Version != "2.1.310" {
		t.Fatal("caller mutated cache")
	}
	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(data), "\n") != 4 {
		t.Fatalf("cache did not suppress probes: %s", data)
	}
	p.expires = time.Now().Add(-time.Second)
	p.Harnesses(ctx)
	data, err = os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(data), "\n") != 8 {
		t.Fatalf("cache did not refresh: %s", data)
	}
}

func TestSupportedRanges(t *testing.T) {
	fixture(t, "claude", "codex", "opencode", "pi")
	cases := []struct {
		h       model.Harness
		version string
		want    bool
	}{
		{model.HarnessClaude, "2.1.0", true}, {model.HarnessClaude, "2.2.0", false}, {model.HarnessClaude, "2.0.99", false},
		{model.HarnessClaude, "v2.1.310", true},
		{model.HarnessCodex, "0.162.9", false}, {model.HarnessCodex, "0.163.0", true}, {model.HarnessCodex, "0.164.99", true}, {model.HarnessCodex, "0.165.0", false},
		{model.HarnessOpenCode, "1.0.0", true}, {model.HarnessOpenCode, "2.99.0", true}, {model.HarnessOpenCode, "3.0.0", false},
		{model.HarnessClaude, "2.1.0-beta.1", false}, {model.HarnessClaude, "not a version", false},
		{model.HarnessPi, "development-build", true},
	}
	for _, tc := range cases {
		t.Run(string(tc.h)+"/"+tc.version, func(t *testing.T) {
			data, _ := json.Marshal(map[string]string{string(tc.h): tc.version})
			t.Setenv("LUGARN_PROBE_VERSIONS", string(data))
			for _, got := range New().Harnesses(context.Background()) {
				if got.Harness == tc.h {
					if got.Supported != tc.want {
						t.Errorf("%+v; supported want %v", got, tc.want)
					}
					if !tc.want && (got.Issue == nil || *got.Issue != model.HarnessIssueUnsupportedVersion) {
						t.Errorf("issue: %+v", got)
					}
				}
			}
		})
	}
}

func TestMissingFailedAndCancelledHarnesses(t *testing.T) {
	fixture(t)
	for _, info := range New().Harnesses(context.Background()) {
		if info.Installed || info.Supported || info.Headless || info.Version != nil || info.Issue == nil || *info.Issue != model.HarnessIssueNotInstalled {
			t.Errorf("missing: %+v", info)
		}
	}
	fixture(t, "claude")
	t.Setenv("LUGARN_PROBE_FAIL", "claude")
	got := New().Harnesses(context.Background())[0]
	if !got.Installed || got.Supported || got.Issue == nil || *got.Issue != model.HarnessIssueUnsupportedVersion {
		t.Fatalf("failed version: %+v", got)
	}
	t.Setenv("LUGARN_PROBE_FAIL", "")
	t.Setenv("LUGARN_PROBE_SLEEP", "claude")
	p := New()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p.Harnesses(ctx)
	if !p.expires.IsZero() {
		t.Fatal("cancelled probe cached")
	}
}

func TestFiveSecondTimeout(t *testing.T) {
	dir, _ := fixture(t, "claude")
	t.Setenv("LUGARN_PROBE_SLEEP", "claude")
	start := time.Now()
	_, err := run(context.Background(), filepath.Join(dir, executable("claude")), "--version")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout: %v", err)
	}
	if elapsed := time.Since(start); elapsed < 5*time.Second || elapsed > 9*time.Second {
		t.Fatalf("timeout took %s", elapsed)
	}
}

func TestHerdr(t *testing.T) {
	_, log := fixture(t, "herdr")
	id, err := model.NewHostID()
	if err != nil {
		t.Fatal(err)
	}
	session := "lugarn-probe-" + id
	p := New()
	info, err := p.Herdr(context.Background(), session)
	if err != nil || !info.Installed || info.Running || info.Version == nil || *info.Version != "0.9.5" || info.Server != session {
		t.Fatalf("Herdr: %+v, %v", info, err)
	}
	t.Setenv("LUGARN_PROBE_STATUS", `{"running":true}`)
	info, err = p.Herdr(context.Background(), session)
	if err != nil || !info.Running {
		t.Fatalf("running: %+v, %v", info, err)
	}
	t.Setenv("LUGARN_PROBE_STATUS", `{"status":"unknown"}`)
	if _, err := p.Herdr(context.Background(), session); err == nil {
		t.Fatal("malformed status accepted")
	}
	t.Setenv("LUGARN_PROBE_STATUS", `not json`)
	if _, err := p.Herdr(context.Background(), session); err == nil {
		t.Fatal("invalid JSON accepted")
	}
	t.Setenv("LUGARN_PROBE_FAIL", "herdr")
	if _, err := p.Herdr(context.Background(), session); err == nil {
		t.Fatal("command failure hidden")
	}
	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "server start") || strings.Contains(string(data), "stop") {
		t.Fatalf("mutating call: %s", data)
	}
	fixture(t)
	info, err = p.Herdr(context.Background(), session)
	if err != nil || info.Installed {
		t.Fatalf("missing Herdr: %+v, %v", info, err)
	}
}

func TestLogind(t *testing.T) {
	_, log := fixture(t, "busctl")
	// Unknown KillUserProcesses must stop the public Linux probe before it
	// reads host files; other OSes must not invoke any command at all.
	t.Setenv("LUGARN_PROBE_KILL", "unknown\n")
	info := New().Logind(context.Background())
	if info != nil {
		t.Fatalf("unknown logind: %+v", info)
	}
	if runtime.GOOS != "linux" {
		if _, err := os.Stat(log); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("non-Linux probe invoked a command: %v", err)
		}
	}
}

func TestLogindSources(t *testing.T) {
	type testCase struct {
		name, dir, kill, linger, fail, missing string
		record, fallback                       bool
		want                                   *LogindInfo
	}
	cases := []testCase{
		{name: "linger file", dir: "present", kill: "b false\n", record: true, fail: "loginctl", want: &LogindInfo{Linger: true}},
		{name: "no linger file", dir: "present", kill: "b true\n", fail: "loginctl", want: &LogindInfo{KillUserProcesses: true}},
		{name: "fallback yes", dir: "missing", kill: "b true\n", linger: "yes\n", fallback: true, want: &LogindInfo{KillUserProcesses: true, Linger: true}},
		{name: "fallback no", dir: "missing", kill: "b false", linger: "no", fallback: true, want: &LogindInfo{}},
		{name: "directory is a file", dir: "file", kill: "b false\n"},
		{name: "busctl fails", dir: "present", kill: "b false\n", fail: "busctl"},
		{name: "busctl missing", dir: "present", missing: "busctl"},
		{name: "fallback fails", dir: "missing", kill: "b false\n", fail: "loginctl", fallback: true},
		{name: "fallback missing", dir: "missing", kill: "b false\n", missing: "loginctl"},
		{name: "fallback unknown", dir: "missing", kill: "b false\n", linger: "unknown\n", fallback: true},
		{name: "fallback boolean alias", dir: "missing", kill: "b false\n", linger: "true\n", fallback: true},
	}
	for _, invalid := range []string{"", "false\n", "b 1\n", "b True\n", "b  false\n", " b false\n", "b false \n", "b true extra\n", "b true\nb false\n", "b false\n\n"} {
		cases = append(cases, testCase{name: "invalid kill " + fmt.Sprintf("%q", invalid), dir: "missing", kill: invalid})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			names := []string{}
			for _, name := range []string{"busctl", "loginctl"} {
				if name != tc.missing {
					names = append(names, name)
				}
			}
			_, log := fixture(t, names...)
			t.Setenv("LUGARN_PROBE_KILL", tc.kill)
			t.Setenv("LUGARN_PROBE_LINGER", tc.linger)
			t.Setenv("LUGARN_PROBE_FAIL", tc.fail)
			dir := filepath.Join(t.TempDir(), "linger")
			switch tc.dir {
			case "present":
				if err := os.Mkdir(dir, 0700); err != nil {
					t.Fatal(err)
				}
				if tc.record {
					if err := os.WriteFile(filepath.Join(dir, "probe-user"), nil, 0600); err != nil {
						t.Fatal(err)
					}
				}
			case "file":
				if err := os.WriteFile(dir, nil, 0600); err != nil {
					t.Fatal(err)
				}
			}
			got := probeLogind(context.Background(), "probe-user", dir)
			if (got == nil) != (tc.want == nil) || (got != nil && *got != *tc.want) {
				t.Fatalf("logind: %+v; want %+v", got, tc.want)
			}
			data, err := os.ReadFile(log)
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				t.Fatal(err)
			}
			if called := strings.Contains(string(data), `"loginctl"`); called != tc.fallback {
				t.Fatalf("loginctl called = %v; want %v: %s", called, tc.fallback, data)
			}
		})
	}
}

func TestWindowsShimResolution(t *testing.T) {
	for _, h := range []model.Harness{model.HarnessCodex, model.HarnessOpenCode} {
		for _, arch := range []string{"amd64", "arm64"} {
			for _, local := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/local=%v", h, arch, local), func(t *testing.T) {
					dir, _ := fixture(t)
					if local {
						dir = filepath.Join(dir, "node_modules", ".bin")
						if err := os.MkdirAll(dir, 0700); err != nil {
							t.Fatal(err)
						}
						t.Setenv("PATH", dir)
					}
					if err := os.WriteFile(filepath.Join(dir, string(h)+".cmd"), []byte("must never execute"), 0600); err != nil {
						t.Fatal(err)
					}
					if path, shim, err := findBinary(h, "windows", arch); err == nil || !shim || path != "" {
						t.Fatalf("shim: %s, %v, %v", path, shim, err)
					}
					paths := npmCandidates(dir, h, arch)
					if len(paths) == 0 {
						t.Fatal("no package layouts")
					}
					for _, candidate := range paths {
						if err := os.MkdirAll(filepath.Dir(candidate), 0700); err != nil {
							t.Fatal(err)
						}
						if err := os.WriteFile(candidate, nil, 0600); err != nil {
							t.Fatal(err)
						}
						if path, shim, err := findBinary(h, "windows", arch); err != nil || shim || path != candidate {
							t.Fatalf("native: %s, %v, %v; want %s", path, shim, err, candidate)
						}
						if err := os.Remove(candidate); err != nil {
							t.Fatal(err)
						}
					}
				})
			}
		}
	}
}

func TestWindowsNativeAndPS1Only(t *testing.T) {
	dir, _ := fixture(t)
	if err := os.WriteFile(filepath.Join(dir, "claude.ps1"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if _, shim, err := findBinary(model.HarnessClaude, "windows", "amd64"); err == nil || !shim {
		t.Fatalf("ps1: %v, %v", shim, err)
	}
	path := filepath.Join(dir, "claude.exe")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if got, shim, err := findBinary(model.HarnessClaude, "windows", "amd64"); err != nil || shim || got != path {
		t.Fatalf("exe: %s, %v, %v", got, shim, err)
	}
}

func TestWindowsShimOnlyPublicProbe(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows public path lookup; layouts are tested on every OS")
	}
	dir, log := fixture(t)
	if err := os.WriteFile(filepath.Join(dir, "codex.cmd"), []byte("must never run"), 0600); err != nil {
		t.Fatal(err)
	}
	p := New()
	info := p.Harnesses(context.Background())[1]
	if !info.Installed || info.Supported || info.Headless || info.Version != nil || info.Issue == nil || *info.Issue != model.HarnessIssueShimOnly {
		t.Fatalf("shim-only: %+v", info)
	}
	if _, err := p.Binary(model.HarnessCodex); err == nil {
		t.Fatal("shim returned as executable")
	}
	if _, err := os.Stat(log); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("shim was executed: %v", err)
	}
}
