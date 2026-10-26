//go:build windows

package herdr

import "golang.org/x/sys/windows"

func processAlive(pid int) bool {
	h, err := windows.OpenProcess(windows.SYNCHRONIZE|windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	event, err := windows.WaitForSingleObject(h, 0)
	return err == nil && event == uint32(windows.WAIT_TIMEOUT)
}

func killProcess(pid int) {
	h, err := windows.OpenProcess(windows.PROCESS_TERMINATE|windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		return
	}
	defer windows.CloseHandle(h)
	_ = windows.TerminateProcess(h, 1)
	_, _ = windows.WaitForSingleObject(h, 10000)
}
