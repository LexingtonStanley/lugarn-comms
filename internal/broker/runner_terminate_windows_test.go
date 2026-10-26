//go:build windows

package broker

import (
	"errors"
	"testing"

	"golang.org/x/sys/windows"
)

func TestWindowsTerminateProcessAccessDeniedRace(t *testing.T) {
	for _, tc := range []struct {
		name         string
		terminateErr error
		state        uint32
		waitErr      error
		wantNil      bool
		wantWait     bool
	}{
		{"already terminating", windows.ERROR_ACCESS_DENIED, windows.WAIT_OBJECT_0, nil, true, true},
		{"still live denied", windows.ERROR_ACCESS_DENIED, uint32(windows.WAIT_TIMEOUT), nil, false, true},
		{"wait failed", windows.ERROR_ACCESS_DENIED, 0, windows.ERROR_INVALID_HANDLE, false, true},
		{"different error", windows.ERROR_INVALID_PARAMETER, 0, nil, false, false},
		{"terminated", nil, 0, nil, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			handle := windows.Handle(123)
			waited := false
			err := terminateWindowsProcess(handle, func(got windows.Handle, exit uint32) error {
				if got != handle || exit != 1 {
					t.Fatal(got, exit)
				}
				return tc.terminateErr
			}, func(got windows.Handle, timeout uint32) (uint32, error) {
				waited = true
				if got != handle || timeout != 500 {
					t.Fatal(got, timeout)
				}
				return tc.state, tc.waitErr
			})
			if (err == nil) != tc.wantNil || waited != tc.wantWait {
				t.Fatal(err, waited)
			}
			if !tc.wantNil && !errors.Is(err, tc.terminateErr) {
				t.Fatal("lost original failure", err)
			}
			if tc.waitErr != nil && !errors.Is(err, tc.waitErr) {
				t.Fatal("lost wait failure", err)
			}
		})
	}
}
