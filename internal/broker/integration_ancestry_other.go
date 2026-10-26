//go:build !linux

package broker

import "context"

// Windows' native helper walks the process tree; macOS uses the recorded group.
func cancelOwnedTree(ctx context.Context, pid int64, birth string) error {
	return (nativeProcesses{}).Cancel(ctx, pid, birth)
}
