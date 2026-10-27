//go:build !windows

package broker

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// Open each component relative to a held directory fd, with O_NOFOLLOW. This
// refuses links even when another process swaps a component during preparation.
func ensureShellHome(state string) error {
	fd, e := unix.Open(state, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if e != nil {
		return e
	}
	defer unix.Close(fd)
	return shellHomeDir(fd, "shell-home", func(home int) error {
		for _, name := range []string{".zshenv", ".zprofile", ".zshrc", ".zlogin", ".bash_profile", ".bashrc", ".profile"} {
			if e := emptyStartupFile(home, name); e != nil {
				return e
			}
		}
		return shellHomeDir(home, ".config", func(config int) error {
			return shellHomeDir(config, "fish", func(fish int) error { return emptyStartupFile(fish, "config.fish") })
		})
	})
}
func shellHomeDir(parent int, name string, prepare func(int) error) error {
	if e := unix.Mkdirat(parent, name, 0700); e != nil && !errors.Is(e, unix.EEXIST) {
		return e
	}
	fd, e := unix.Openat(parent, name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if e != nil {
		return e
	}
	defer unix.Close(fd)
	if e = unix.Fchmod(fd, 0700); e != nil {
		return e
	}
	return prepare(fd)
}
func emptyStartupFile(parent int, name string) error {
	fd, e := unix.Openat(parent, name, unix.O_WRONLY|unix.O_CREAT|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0600)
	if e != nil {
		return e
	}
	f := os.NewFile(uintptr(fd), name)
	defer f.Close()
	info, e := f.Stat()
	if e != nil {
		return e
	}
	// Refuse hard links as well: truncation must never modify another file.
	var stat unix.Stat_t
	if e = unix.Fstat(fd, &stat); e != nil {
		return e
	}
	if !info.Mode().IsRegular() || stat.Nlink != 1 {
		return fmt.Errorf("unsafe shell startup file: %s", name)
	}
	if e = f.Chmod(0600); e != nil {
		return e
	}
	return f.Truncate(0)
}
