//go:build darwin

package broker

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

func darwinTreeProcess(_ context.Context, pid int64) (treeProcess, error) {
	info, e := unix.SysctlKinfoProc("kern.proc.pid", int(pid))
	if e == unix.ESRCH || e == unix.ENOENT {
		return treeProcess{}, nil
	}
	if e != nil {
		return treeProcess{}, e
	}
	if info.Proc.P_pid != int32(pid) || info.Proc.P_stat == 5 {
		return treeProcess{}, nil
	}
	return treeProcess{PID: pid, Parent: int64(info.Eproc.Ppid), Birth: fmt.Sprintf("%d:%d", info.Proc.P_starttime.Sec, info.Proc.P_starttime.Usec)}, nil
}
func cancelOwnedTree(ctx context.Context, pid int64, birth string) error {
	root, e := darwinTreeProcess(ctx, pid)
	if e != nil {
		return e
	}
	if root.Birth == "" || root.Birth != birth {
		return nil
	}
	raw, e := exec.CommandContext(ctx, "/bin/ps", "-axo", "pid=,ppid=").Output()
	if e != nil {
		return e
	}
	nodes := map[int64]treeProcess{}
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
		if e != nil {
			continue
		}
		node, e := darwinTreeProcess(ctx, child)
		if e != nil {
			return e
		}
		if node.Parent == parent && node.Birth != "" {
			nodes[child] = node
		}
	}
	return walkOwnedTree(ctx, root, nodes, darwinTreeProcess, func(ctx context.Context, node treeProcess) error {
		current, e := darwinTreeProcess(ctx, node.PID)
		if e != nil {
			return e
		}
		if current != node {
			return nil
		}
		// Darwin has no pidfd. Recheck parent and birth immediately before signalling.
		e = unix.Kill(int(node.PID), unix.SIGKILL)
		if e == unix.ESRCH {
			return nil
		}
		return e
	})
}
