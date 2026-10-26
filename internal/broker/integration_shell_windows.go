//go:build windows

package broker

// Windows cannot exec over Herdr's shell; nesting PowerShell makes agent start
// report agent_pane_busy. Windows launches use RunPaneLaunch instead.
func execPaneShell([]string) error { return invalid("pane-shell is POSIX-only; use pane-launch") }
