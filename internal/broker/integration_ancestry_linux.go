//go:build linux

package broker

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

func linuxTreeProcess(_ context.Context, pid int64) (treeProcess, error) {
	raw, e := os.ReadFile(filepath.Join("/proc", strconv.FormatInt(pid, 10), "stat"))
	if os.IsNotExist(e) {
		return treeProcess{}, nil
	}
	if e != nil {
		return treeProcess{}, e
	}
	end := strings.LastIndexByte(string(raw), ')')
	if end < 0 {
		return treeProcess{}, fmt.Errorf("invalid process stat")
	}
	fields := strings.Fields(string(raw[end+1:]))
	if len(fields) < 20 {
		return treeProcess{}, fmt.Errorf("short process stat")
	}
	if fields[0] == "Z" || fields[0] == "X" {
		return treeProcess{}, nil
	}
	parent, e := strconv.ParseInt(fields[1], 10, 64)
	if e != nil {
		return treeProcess{}, e
	}
	boot, e := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if e != nil {
		return treeProcess{}, e
	}
	return treeProcess{PID: pid, Parent: parent, Birth: strings.TrimSpace(string(boot)) + ":" + fields[19]}, nil
}
func signalLinuxProcess(ctx context.Context, node treeProcess) error {
	// Open before verification: even immediate PID reuse after verification cannot
	// redirect this signal. Kernels without pidfds fail closed, never kill by name.
	fd, e := unix.PidfdOpen(int(node.PID), 0)
	if e == unix.ESRCH {
		return nil
	}
	if e != nil {
		return e
	}
	defer unix.Close(fd)
	current, e := linuxTreeProcess(ctx, node.PID)
	if e != nil {
		return e
	}
	if current != node {
		return nil
	}
	e = unix.PidfdSendSignal(fd, unix.SIGKILL, nil, 0)
	if e == unix.ESRCH {
		return nil
	}
	return e
}
func cancelOwnedTree(ctx context.Context, pid int64, birth string) error {
	root, e := linuxTreeProcess(ctx, pid)
	if e != nil {
		return e
	}
	if root.Birth == "" || root.Birth != birth {
		return nil
	}
	entries, e := os.ReadDir("/proc")
	if e != nil {
		return e
	}
	nodes := map[int64]treeProcess{}
	for _, entry := range entries {
		id, e := strconv.ParseInt(entry.Name(), 10, 64)
		if e != nil {
			continue
		}
		node, e := linuxTreeProcess(ctx, id)
		if e == nil && node.Birth != "" {
			nodes[id] = node
		}
	}
	return walkOwnedTree(ctx, root, nodes, linuxTreeProcess, signalLinuxProcess)
}
