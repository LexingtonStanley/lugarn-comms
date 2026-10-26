//go:build windows

package broker

import "golang.org/x/sys/windows"

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
