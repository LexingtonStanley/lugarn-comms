//go:build windows

package herdr

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"

	"golang.org/x/sys/windows"
)

// startServerProcess creates the server through WMI. A process started any
// other way inside an SSH session belongs to its job object and dies with the
// connection; WMI's belongs to the WMI service. It isn't our child, so nothing
// reports its exit: StartServer polls until its timeout.
func startServerProcess(ctx context.Context, bin, session, dir string, overrides []string) (int, <-chan struct{}, StartMethod, error) {
	pid, err := wmiCreate(ctx, []string{bin, "--session", session, "server"}, dir)
	if err != nil {
		return 0, nil, "", err
	}
	return pid, nil, MethodWMI, nil
}

// wmiScript calls Win32_Process.Create. It goes to powershell.exe as
// -EncodedCommand and reads the command line and directory from the
// environment, so no string built here is ever quoted for PowerShell.
const wmiScript = `$ErrorActionPreference = 'Stop'
$arguments = @{ CommandLine = $env:LUGARN_HERDR_WMI_COMMAND_LINE }
if ($env:LUGARN_HERDR_WMI_DIRECTORY) { $arguments.CurrentDirectory = $env:LUGARN_HERDR_WMI_DIRECTORY }
$r = Invoke-CimMethod -ClassName Win32_Process -MethodName Create -Arguments $arguments
[Console]::Out.Write(('{0} {1}' -f [int]$r.ReturnValue, [int64]$r.ProcessId))
`

// wmiCreate creates argv as a new process through WMI and returns its pid.
// windows.ComposeCommandLine quotes argv by the rules CommandLineToArgvW and
// Go's runtime parse, as os/exec does.
func wmiCreate(ctx context.Context, argv []string, dir string) (int, error) {
	system, err := windows.GetSystemDirectory()
	if err != nil {
		return 0, clientError("server", kindOther, "system directory: "+err.Error())
	}
	ps := filepath.Join(system, "WindowsPowerShell", "v1.0", "powershell.exe")
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, ps, "-NoLogo", "-NoProfile", "-NonInteractive", "-EncodedCommand", encodePowerShell(wmiScript))
	cmd.Env = append(cleanEnv(os.Environ()),
		"LUGARN_HERDR_WMI_COMMAND_LINE="+windows.ComposeCommandLine(argv),
		"LUGARN_HERDR_WMI_DIRECTORY="+dir)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return 0, clientError("server", kindOther, "Win32_Process.Create failed")
	}
	fields := strings.Fields(string(out))
	if len(fields) != 2 {
		return 0, clientError("server", kindOther, "Win32_Process.Create: unexpected output")
	}
	rv, err1 := strconv.Atoi(fields[0])
	pid, err2 := strconv.Atoi(fields[1])
	if err1 != nil || err2 != nil {
		return 0, clientError("server", kindOther, "Win32_Process.Create: unexpected output")
	}
	if rv != 0 {
		return 0, clientError("server", kindOther, fmt.Sprintf("Win32_Process.Create returned %d (%s)", rv, wmiReturn(rv)))
	}
	return pid, nil
}

// encodePowerShell is -EncodedCommand's format: base64 of UTF-16LE.
func encodePowerShell(script string) string {
	units := utf16.Encode([]rune(script))
	b := make([]byte, 2*len(units))
	for i, u := range units {
		b[2*i] = byte(u)
		b[2*i+1] = byte(u >> 8)
	}
	return base64.StdEncoding.EncodeToString(b)
}

func wmiReturn(rv int) string {
	switch rv {
	case 2:
		return "access denied"
	case 3:
		return "insufficient privilege"
	case 8:
		return "unknown failure"
	case 9:
		return "path not found"
	case 21:
		return "invalid parameter"
	default:
		return "unexpected"
	}
}
