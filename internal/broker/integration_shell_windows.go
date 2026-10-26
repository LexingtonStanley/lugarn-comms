//go:build windows

package broker

import (
	"encoding/base64"
	"encoding/binary"
	"os"
	"os/exec"
	"path/filepath"
	"unicode/utf16"
)

func execPaneShell(env []string) error {
	script := `[IO.File]::WriteAllText($env:LUGARN_COMMS_PANE_READY, 'ready')`
	units := utf16.Encode([]rune(script))
	raw := make([]byte, len(units)*2)
	for n, u := range units {
		binary.LittleEndian.PutUint16(raw[n*2:], u)
	}
	command := exec.Command(filepath.Join(os.Getenv("SystemRoot"), "System32", "WindowsPowerShell", "v1.0", "powershell.exe"), "-NoLogo", "-NoProfile", "-NoExit", "-EncodedCommand", base64.StdEncoding.EncodeToString(raw))
	command.Env = env
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	return command.Run()
}
