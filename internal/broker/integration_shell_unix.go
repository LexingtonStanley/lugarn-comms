//go:build !windows

package broker

import "syscall"

func execPaneShell(env []string) error {
	// sh -c and its final interactive exec see no ENV/BASH_ENV and an empty
	// HISTFILE: bash otherwise invents a default history path on shell exit.
	// The fixed script only writes our ready marker; no agent text enters a shell.
	return syscall.Exec("/usr/bin/env", cleanShellArgs(), env)
}

func cleanShellArgs() []string {
	return []string{"env", "-u", "ENV", "-u", "BASH_ENV", "HISTFILE=", "/bin/sh", "-c", `printf ready > "$LUGARN_COMMS_PANE_READY"; exec /bin/sh`}
}
