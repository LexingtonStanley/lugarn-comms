//go:build !windows

package herdr

import (
	"os"
	"strings"
	"syscall"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
)

// Herdr 0.9.5 src/config/io.rs and src/session.rs. Explicit --session strips
// inherited HERDR_* overrides; HERDR_CONFIG_PATH changes only the config file,
// not this directory. LookupEnv preserves Herdr's treatment of an empty value.
func sessionSocketPath(session string) string {
	config, ok := os.LookupEnv("XDG_CONFIG_HOME")
	if !ok {
		if home, ok := os.LookupEnv("HOME"); ok {
			config = herdrJoin(home, ".config")
		} else {
			return socketInSession(herdrJoin(os.TempDir(), "herdr"), session)
		}
	}
	return socketInSession(herdrJoin(config, "herdr"), session)
}

// Rust PathBuf::join preserves dot components and repeated separators in the
// supplied base. Cleaning those would undercount the actual socket name.
func herdrJoin(base, name string) string {
	if base == "" || strings.HasSuffix(base, "/") {
		return base + name
	}
	return base + "/" + name
}

func socketInSession(dir, session string) string {
	if session != model.DefaultHerdrServer {
		dir = herdrJoin(herdrJoin(dir, "sessions"), session)
	}
	return herdrJoin(dir, "herdr.sock")
}

func checkSessionSocketPaths(session string) error {
	return checkServerSocketPaths(sessionSocketPath(session))
}

func checkServerSocketPaths(apiPath string) error {
	if apiPath == "" {
		return nil
	}
	// Named sessions also bind herdr-client.sock, seven bytes longer. Both
	// paths must fit, including the terminating NUL (108 bytes on Linux,
	// 104 on macOS). Measure bytes, not runes.
	limit := len(syscall.RawSockaddrUnix{}.Path) - 1
	clientPath := apiPath[:strings.LastIndex(apiPath, "/")+1] + "herdr-client.sock"
	for _, path := range []string{apiPath, clientPath} {
		if len(path) > limit {
			return model.NewError(model.ErrorHerdrUnavailable, "Herdr's socket path is too long; use a shorter HOME, XDG_CONFIG_HOME or session name").WithDetails(map[string]any{
				"reason": "socket_path_too_long", "path": path, "length": len(path), "limit": limit,
			})
		}
	}
	return nil
}
