//go:build !windows

package broker

func plainNativePath(path string) (string, error) { return path, nil }
