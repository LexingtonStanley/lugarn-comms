//go:build linux

package broker

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func cancelOwnedTree(ctx context.Context, pid int64, birth string) error {
	p := nativeProcesses{}
	root, e := p.Inspect(ctx, pid)
	if e != nil || !root.Alive {
		return e
	}
	if birth == "" || root.Birth != birth {
		return nil
	} // Reused PID: never signal it.
	entries, e := os.ReadDir("/proc")
	if e != nil {
		return e
	}
	parents := map[int64]int64{}
	for _, entry := range entries {
		child, e := strconv.ParseInt(entry.Name(), 10, 64)
		if e != nil {
			continue
		}
		raw, e := os.ReadFile(filepath.Join("/proc", entry.Name(), "stat"))
		if e != nil {
			continue
		}
		end := strings.LastIndexByte(string(raw), ')')
		if end < 0 {
			continue
		}
		fields := strings.Fields(string(raw[end+1:]))
		if len(fields) < 2 {
			continue
		}
		parent, e := strconv.ParseInt(fields[1], 10, 64)
		if e == nil {
			parents[child] = parent
		}
	}
	var visit func(int64) error
	seen := map[int64]bool{}
	visit = func(parent int64) error {
		if seen[parent] {
			return nil
		}
		seen[parent] = true
		for child, ancestor := range parents {
			if ancestor != parent {
				continue
			}
			state, e := p.Inspect(ctx, child)
			if e != nil {
				return e
			}
			if !state.Alive {
				continue
			}
			if e = visit(child); e != nil {
				return e
			}
			if e = p.Cancel(ctx, child, state.Birth); e != nil {
				return e
			}
		}
		return nil
	}
	if e = visit(pid); e != nil {
		return e
	}
	return p.Cancel(ctx, pid, birth)
}
