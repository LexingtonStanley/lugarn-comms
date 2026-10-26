//go:build !linux && !darwin && !windows

package harness

func processParents() (map[int]int, error) { return nil, nil }
