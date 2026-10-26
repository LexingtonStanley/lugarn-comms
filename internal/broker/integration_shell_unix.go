//go:build !windows

package broker

import "syscall"

func execPaneShell(env []string) error {
	// sh -c and its final interactive exec both see ENV/BASH_ENV/HISTFILE removed.
	// The fixed script only writes our ready marker; no agent text enters a shell.
	return syscall.Exec("/usr/bin/env", []string{"env", "-u", "ENV", "-u", "BASH_ENV", "-u", "HISTFILE", "/bin/sh", "-c", `printf ready > "$LUGARN_COMMS_PANE_READY"; exec /bin/sh`}, env)
}
