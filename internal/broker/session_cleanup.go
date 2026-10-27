package broker

import (
	"context"
	"os"
	"path/filepath"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
	"github.com/LexingtonStanley/lugarn-comms/internal/store"
)

// cleanRemovedSessions runs after the removal transaction commits. Subsequent
// apply/start/reconcile/stop calls retry cleanup left by a crash or I/O failure.
func (b *Broker) cleanRemovedSessions(ctx context.Context, team string) error {
	if b.opts.StateDir == "" {
		return nil
	}
	root := filepath.Join(b.opts.StateDir, "sessions")
	info, err := os.Lstat(root)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return model.NewError(model.ErrorInternal, "sessions path is not a directory").WithDetail("path", root)
	}
	// List before consulting the store, as with per-launch reconciliation.
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	removed := map[string]bool{}
	err = b.db.Read(ctx, func(tx *store.Tx) error {
		members, err := tx.ListMembers(team, true)
		if err != nil {
			return err
		}
		for _, m := range members {
			if m.RemovedAt != nil {
				removed[m.ID] = true
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	for _, entry := range entries {
		id := entry.Name()
		if !removed[id] || model.ValidateOpaqueID(id) != nil || filepath.Base(id) != id || id == "." || id == ".." {
			continue
		}
		path := filepath.Join(root, id)
		info, err := os.Lstat(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			err = os.Remove(path)
		} else if info.IsDir() {
			err = os.RemoveAll(path)
		}
		if err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}
