//go:build windows

package herdr

// Herdr uses named pipes on Windows; the Unix sun_path limit does not apply.
func checkServerSocketPaths(string) error { return nil }
