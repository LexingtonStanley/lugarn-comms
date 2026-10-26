package hostprobe

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
)

func findBinary(h model.Harness, goos, arch string) (string, bool, error) {
	if !h.Valid() {
		return "", false, fmt.Errorf("unknown harness: %s", h)
	}
	name := string(h)
	if goos != "windows" {
		path, err := exec.LookPath(name)
		return path, false, err
	}
	var shims []string
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		// Do not allow a relative PATH entry to select a binary in an agent's cwd.
		if !filepath.IsAbs(dir) {
			continue
		}
		path := filepath.Join(dir, name+".exe")
		if regular(path) {
			return path, false, nil
		}
		for _, ext := range []string{".cmd", ".ps1", ".bat", ""} {
			if regular(filepath.Join(dir, name+ext)) {
				shims = append(shims, dir)
				break
			}
		}
	}
	for _, dir := range shims {
		for _, path := range npmCandidates(dir, h, arch) {
			if regular(path) {
				return path, false, nil
			}
		}
	}
	return "", len(shims) > 0, fmt.Errorf("%s: no native executable on PATH", h)
}

func regular(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

// These are fixed npm layouts, never parsed or executed shim contents. Both
// global prefix shims and local node_modules/.bin shims are supported.
func npmCandidates(shimDir string, h model.Harness, arch string) []string {
	if arch != "amd64" && arch != "arm64" {
		return nil
	}
	modules := filepath.Join(shimDir, "node_modules")
	if strings.EqualFold(filepath.Base(shimDir), ".bin") && strings.EqualFold(filepath.Base(filepath.Dir(shimDir)), "node_modules") {
		modules = filepath.Dir(shimDir)
	}
	platformArch := "x64"
	triple := "x86_64-pc-windows-msvc"
	if arch == "arm64" {
		platformArch = "arm64"
		triple = "aarch64-pc-windows-msvc"
	}
	var paths []string
	switch h {
	case model.HarnessCodex:
		root := filepath.Join(modules, "@openai", "codex")
		platformPackage := "codex-win32-" + platformArch
		for _, vendor := range []string{
			filepath.Join(root, "node_modules", "@openai", platformPackage, "vendor"),
			filepath.Join(modules, "@openai", platformPackage, "vendor"),
			filepath.Join(root, "vendor"),
		} {
			// bin is the current layout; codex is the older bundled layout.
			for _, binDir := range []string{"bin", "codex"} {
				paths = append(paths, filepath.Join(vendor, triple, binDir, "codex.exe"))
			}
		}
	case model.HarnessOpenCode:
		for _, root := range []string{filepath.Join(modules, "opencode-ai", "node_modules"), modules} {
			names := []string{"opencode-windows-" + platformArch}
			if arch == "amd64" {
				names = []string{"opencode-windows-x64-baseline", "opencode-windows-x64"}
			}
			for _, name := range names {
				paths = append(paths, filepath.Join(root, name, "bin", "opencode.exe"))
			}
		}
	}
	return paths
}
