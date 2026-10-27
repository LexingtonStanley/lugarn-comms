//go:build !windows

package herdr

import (
	"path/filepath"
	"syscall"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
)

func checkServerSocketPaths(apiPath string) error {
	if apiPath == "" {
		return nil
	}
	// Named sessions also bind herdr-client.sock, seven bytes longer. Both
	// paths must fit, including the terminating NUL (108 bytes on Linux,
	// 104 on macOS). Measure bytes, not runes.
	limit := len(syscall.RawSockaddrUnix{}.Path) - 1
	for _, path := range []string{apiPath, filepath.Join(filepath.Dir(apiPath), "herdr-client.sock")} {
		if len(path) > limit {
			return model.NewError(model.ErrorHerdrUnavailable, "Herdr's socket path is too long; use a shorter HOME, XDG_CONFIG_HOME or session name").WithDetails(map[string]any{
				"reason": "socket_path_too_long", "path": path, "length": len(path), "limit": limit,
			})
		}
	}
	return nil
}
