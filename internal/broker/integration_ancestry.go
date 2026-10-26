package broker

import "context"

type treeProcess struct {
	PID, Parent int64
	Birth       string
}

// walkOwnedTree uses identities captured with the parent snapshot, then checks
// both again before recursion and signalling. The OS signal function rechecks
// once more through its stable process handle where one exists (Linux pidfd).
func walkOwnedTree(ctx context.Context, root treeProcess, nodes map[int64]treeProcess, inspect func(context.Context, int64) (treeProcess, error), signal func(context.Context, treeProcess) error) error {
	seen := map[int64]bool{}
	var visit func(treeProcess) error
	visit = func(node treeProcess) error {
		if seen[node.PID] {
			return nil
		}
		seen[node.PID] = true
		current, e := inspect(ctx, node.PID)
		if e != nil {
			return e
		}
		if current != node || node.Birth == "" {
			return nil
		}
		for _, child := range nodes {
			if child.Parent == node.PID {
				if e = visit(child); e != nil {
					return e
				}
			}
		}
		current, e = inspect(ctx, node.PID)
		if e != nil {
			return e
		}
		if current != node {
			return nil
		}
		return signal(ctx, node)
	}
	return visit(root)
}
