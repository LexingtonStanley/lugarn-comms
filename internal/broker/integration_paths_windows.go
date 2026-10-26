//go:build windows

package broker

import (
	"regexp"

	"golang.org/x/sys/windows"
)

// plainLaunchPath matches the words Herdr's RunCommand types unquoted on
// Windows, including the ~ of an 8.3 short name, which GetShortPathName below
// produces for a path with spaces (C:\Users\JOHNSM~1).
var plainLaunchPath = regexp.MustCompile(`^(?:[A-Za-z0-9]~|[A-Za-z0-9._/:\\-])+$`)

func plainNativePath(path string) (string, error) {
	if plainLaunchPath.MatchString(path) {
		return path, nil
	}
	input, e := windows.UTF16PtrFromString(path)
	if e != nil {
		return "", e
	}
	buffer := make([]uint16, 32768)
	n, e := windows.GetShortPathName(input, &buffer[0], uint32(len(buffer)))
	if e != nil {
		return "", e
	}
	return windows.UTF16ToString(buffer[:n]), nil
}
