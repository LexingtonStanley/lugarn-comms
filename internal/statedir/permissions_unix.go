//go:build !windows

package statedir

import (
	"fmt"
	"os"
	"syscall"
)

func protectDir(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Geteuid()) {
		return fmt.Errorf("state directory is owned by another user: %s", path)
	}
	return f.Chmod(0700)
}

func protectFile(f *os.File) error { return f.Chmod(0600) }
