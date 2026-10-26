package broker

import (
	"context"
	"testing"
)

func TestTreeKillRechecksSnapshotIdentity(t *testing.T) {
	for _, change := range []string{"birth", "parent", "before-signal"} {
		t.Run(change, func(t *testing.T) {
			root := treeProcess{PID: 1, Parent: 9, Birth: "root"}
			child := treeProcess{PID: 2, Parent: 1, Birth: "old"}
			reads := 0
			killedChild := false
			e := walkOwnedTree(context.Background(), root, map[int64]treeProcess{2: child}, func(_ context.Context, id int64) (treeProcess, error) {
				if id == 1 {
					return root, nil
				}
				reads++
				current := child
				if change == "birth" || change == "before-signal" && reads > 1 {
					current.Birth = "reused"
				}
				if change == "parent" {
					current.Parent = 99
				}
				return current, nil
			}, func(_ context.Context, node treeProcess) error {
				if node.PID == 2 {
					killedChild = true
				}
				return nil
			})
			if e != nil || killedChild {
				t.Fatal("signalled changed/reparented process", e)
			}
		})
	}
}
