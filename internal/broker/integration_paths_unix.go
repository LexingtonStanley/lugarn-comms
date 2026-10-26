//go:build !windows

package broker

import "regexp"

// plainLaunchPath matches the words Herdr's RunCommand types unquoted on POSIX.
var plainLaunchPath = regexp.MustCompile(`^[A-Za-z0-9._/:-]+$`)

func plainNativePath(path string) (string, error) { return path, nil }
