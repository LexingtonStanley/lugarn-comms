// Package statedir owns the broker's private state directory and secret files.
package statedir

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// Resolve returns the configured state directory. It does not create it.
func Resolve() (string, error) {
	if dir := os.Getenv("LUGARN_COMMS_HOME"); dir != "" {
		return dir, nil
	}
	if runtime.GOOS == "windows" {
		dir := os.Getenv("LOCALAPPDATA")
		if dir == "" {
			return "", fmt.Errorf("LOCALAPPDATA is unset")
		}
		return filepath.Join(dir, "Lugarn", "comms"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".lugarn", "comms"), nil
}

// Ensure creates dir and launch, or tightens their existing permissions. It
// rejects symbolic links and non-directories at either path.
func Ensure(dir string) error {
	for _, path := range []string{dir, filepath.Join(dir, "launch")} {
		if err := os.MkdirAll(path, 0700); err != nil {
			return err
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("state path is not a directory: %s", path)
		}
		if err := protectDir(path); err != nil {
			return err
		}
	}
	return nil
}

// WriteSecret atomically replaces path with data in a private file. The parent
// must already exist; callers normally create it with Ensure. Temporary files
// are private before any secret bytes are written and are removed on failure.
func WriteSecret(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".lugarn-secret-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	defer f.Close()
	if err := protectFile(f); err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
