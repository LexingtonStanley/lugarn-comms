//go:build !windows

package broker

import (
	"slices"
	"testing"
)

func TestCleanShellHistoryIsEmpty(t *testing.T) {
	args := cleanShellArgs()
	if !slices.Contains(args, "HISTFILE=") {
		t.Fatal("bash must not invent a default history file", args)
	}
	for i, a := range args {
		if a == "HISTFILE" && i > 0 && args[i-1] == "-u" {
			t.Fatal(args)
		}
	}
}
