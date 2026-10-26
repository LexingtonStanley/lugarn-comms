//go:build darwin

package broker

import (
	"context"
	"os/exec"
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
	}
	raw, e := exec.CommandContext(ctx, "/bin/ps", "-axo", "pid=,ppid=").Output()
	if e != nil {
		return e
	}
	parents := map[int64]int64{}
	for _, line := range strings.Split(string(raw), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		child, e := strconv.ParseInt(fields[0], 10, 64)
		if e != nil {
			continue
		}
		parent, e := strconv.ParseInt(fields[1], 10, 64)
		if e == nil {
			parents[child] = parent
		}
	}
	seen := map[int64]bool{}
	var visit func(int64) error
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
