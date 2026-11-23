package hc

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// Info is the optional hello hc record. Unknown observations remain null.
type Info struct {
	Installed               bool    `json:"installed"`
	Version                 *string `json:"version"`
	Path                    *string `json:"path"`
	Running                 *bool   `json:"running"`
	Generation              *int    `json:"generation"`
	Supported               *bool   `json:"supported"`
	Core                    *Core   `json:"core"`
	KeepsRunningAfterLogout *bool   `json:"keeps_running_after_logout"`
}

func SocketExists(session string) *bool {
	if runtime.GOOS != "linux" || !sessionPattern.MatchString(session) {
		return nil
	}
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		return nil
	}
	st, e := os.Stat(filepath.Join(dir, "herdr-core", session+".sock"))
	if e != nil && !os.IsNotExist(e) {
		return nil
	}
	exists := e == nil && st.Mode()&os.ModeSocket != 0
	return &exists
}
func Probe(ctx context.Context, binary, session string) Info {
	var info Info
	path, e := Resolve(binary)
	if e != nil {
		return info
	}
	info.Installed = true
	info.Path = &path
	cmd := exec.CommandContext(ctx, path, "--version")
	cmd.Env = CleanEnv(os.Environ())
	if raw, e := cmd.Output(); e == nil {
		version := strings.TrimSpace(string(raw))
		info.Version = &version
	}
	info.Running = SocketExists(session)
	if info.Running == nil || !*info.Running {
		return info
	}
	c, e := Connect(ctx, path, session)
	var generation *GenerationError
	if errors.As(e, &generation) {
		info.Generation = &generation.Hello.Generation
		info.Core = &generation.Hello.Core
		info.Version = &generation.Hello.Version
		info.KeepsRunningAfterLogout = generation.Hello.KeepsRunningAfterLogout
		supported := false
		info.Supported = &supported
		return info
	}
	if e != nil {
		return info
	}
	defer c.Close()
	info.Generation = &c.Hello.Generation
	supported := true
	info.Supported = &supported
	info.Core = &c.Hello.Core
	info.Version = &c.Hello.Version
	info.KeepsRunningAfterLogout = c.Hello.KeepsRunningAfterLogout
	return info
}
