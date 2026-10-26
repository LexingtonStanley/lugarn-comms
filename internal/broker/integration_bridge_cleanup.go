package broker

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/LexingtonStanley/lugarn-comms/internal/statedir"
)

const bridgeOwnerFile = "owner.sha256"
const bridgeGrace = 60 * time.Second

func (i *Integration) bridgeOwner() string {
	path, err := filepath.Abs(i.b.opts.StateDir)
	if err != nil {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	if runtime.GOOS == "windows" {
		path = strings.ToLower(path)
	}
	sum := sha256.Sum256([]byte(filepath.Clean(path)))
	return hex.EncodeToString(sum[:])
}

func (i *Integration) markBridge(dir string) error {
	return statedir.WriteSecret(filepath.Join(dir, bridgeOwnerFile), []byte(i.bridgeOwner()))
}

func (i *Integration) ownsBridge(dir string) bool {
	if !strings.HasPrefix(filepath.Base(dir), "lugarn-runner-") {
		return false
	}
	if !sameNativePath(filepath.Dir(dir), os.TempDir()) && !sameNativePath(filepath.Dir(dir), i.b.opts.StateDir) {
		return false
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return false
	}
	path := filepath.Join(dir, bridgeOwnerFile)
	info, err = os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() != 64 {
		return false
	}
	raw, err := os.ReadFile(path)
	return err == nil && string(raw) == i.bridgeOwner()
}

// recordedBridges is conservative: an unreadable placement prevents deletion.
func (i *Integration) recordedBridges() (map[string]bool, error) {
	result := map[string]bool{}
	root := filepath.Join(i.b.opts.StateDir, "runner")
	info, err := os.Lstat(root)
	if os.IsNotExist(err) {
		return result, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, os.ErrInvalid
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(root, entry.Name())
		info, err := os.Lstat(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() {
			return nil, os.ErrInvalid
		}
		raw, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		var p runnerPlacement
		if err = json.Unmarshal(raw, &p); err != nil {
			return nil, err
		}
		if p.BridgeDir != "" {
			result[filepath.Clean(p.BridgeDir)] = true
		}
	}
	return result, nil
}

func bridgeRecorded(recorded map[string]bool, dir string) bool {
	for path := range recorded {
		if sameNativePath(path, dir) {
			return true
		}
	}
	return false
}

// Remove the executable first. On Windows a sharing violation leaves the owner
// marker intact, so a later sweep can retry after the executable is released.
func removeBridge(dir string) error {
	name := "lugarn-comms"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	if err := os.Remove(filepath.Join(dir, name)); err != nil && !os.IsNotExist(err) {
		return err
	}
	// Unexpected entries are never recursively removed.
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Name() != bridgeOwnerFile {
			return os.ErrInvalid
		}
	}
	if err = os.Remove(filepath.Join(dir, bridgeOwnerFile)); err != nil {
		return err
	}
	return os.Remove(dir)
}

func (i *Integration) sweepRunnerBridges() {
	if i.b.opts.StateDir == "" {
		return
	}
	// Snapshot candidates before placements. A concurrent new bridge is either
	// absent from this list or protected by its age until savePlacement commits.
	var candidates []string
	for _, root := range []string{i.b.opts.StateDir, os.TempDir()} {
		info, err := os.Lstat(root)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), "lugarn-runner-") {
				candidates = append(candidates, filepath.Join(root, entry.Name()))
			}
		}
	}
	recorded, err := i.recordedBridges()
	if err != nil {
		return
	}
	for _, dir := range candidates {
		info, err := os.Lstat(dir)
		if err != nil || i.b.opts.Clock().Sub(info.ModTime()) <= bridgeGrace || bridgeRecorded(recorded, dir) || !i.ownsBridge(dir) {
			continue
		}
		// Includes Windows executables still in use: leave them for the next pass.
		_ = removeBridge(dir)
	}
}
