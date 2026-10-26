package policy

import (
	"testing"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
)

func TestIsAbsPath(t *testing.T) {
	cases := []struct {
		os   model.HostOS
		path string
		want bool
	}{
		{model.HostOSLinux, "/home/lex/code", true},
		{model.HostOSLinux, "/", true},
		{model.HostOSDarwin, "/Users/lex/code", true},
		{model.HostOSLinux, "home/lex", false},
		{model.HostOSLinux, "./code", false},
		{model.HostOSLinux, "~/code", false},
		{model.HostOSLinux, "", false},
		{model.HostOSLinux, "/home/\x00lex", false},
		{model.HostOSLinux, `C:\Users\lex`, false},
		{model.HostOSDarwin, `\\server\share`, false},

		{model.HostOSWindows, `C:\Users\lex\code`, true},
		{model.HostOSWindows, `c:\`, true},
		{model.HostOSWindows, `C:/Users/lex/code`, true},
		{model.HostOSWindows, `\\server\share\code`, true},
		{model.HostOSWindows, `//server/share`, true},
		{model.HostOSWindows, `\\?\C:\Users\lex`, true},
		{model.HostOSWindows, `\\.\pipe\x`, true},
		{model.HostOSWindows, `C:Users\lex`, false},
		{model.HostOSWindows, `C:`, false},
		{model.HostOSWindows, `\Users\lex`, false},
		{model.HostOSWindows, `/home/lex`, false},
		{model.HostOSWindows, `\\server`, false},
		{model.HostOSWindows, `\\server\`, false},
		{model.HostOSWindows, `\\\share`, false},
		{model.HostOSWindows, `1:\x`, false},
		{model.HostOSWindows, `Users\lex`, false},
	}
	for _, c := range cases {
		if got := IsAbsPath(c.os, c.path); got != c.want {
			t.Errorf("IsAbsPath(%s, %q) = %v; want %v", c.os, c.path, got, c.want)
		}
	}
}

func TestSplitAndJoinPath(t *testing.T) {
	cases := []struct {
		os                 model.HostOS
		path, parent, base string
		ok                 bool
	}{
		{model.HostOSLinux, "/home/lex/code/flare", "/home/lex/code", "flare", true},
		{model.HostOSLinux, "/home/lex/code/flare/", "/home/lex/code", "flare", true},
		{model.HostOSLinux, "/flare", "/", "flare", true},
		{model.HostOSLinux, "/", "", "", false},
		{model.HostOSWindows, `C:\Users\lex\flare`, `C:\Users\lex`, "flare", true},
		{model.HostOSWindows, `C:/Users/lex/flare`, `C:\Users\lex`, "flare", true},
		{model.HostOSWindows, `C:\flare`, `C:\`, "flare", true},
		{model.HostOSWindows, `C:\`, "", "", false},
		{model.HostOSWindows, `\\srv\share\flare`, `\\srv\share`, "flare", true},
		{model.HostOSWindows, `\\srv\share`, "", "", false},
	}
	for _, c := range cases {
		parent, base, ok := splitPath(c.os, c.path)
		if parent != c.parent || base != c.base || ok != c.ok {
			t.Errorf("splitPath(%s, %q) = %q, %q, %v; want %q, %q, %v", c.os, c.path, parent, base, ok, c.parent, c.base, c.ok)
		}
	}
	joins := []struct {
		os   model.HostOS
		dir  string
		want string
	}{
		{model.HostOSLinux, "/home/lex", "/home/lex/a/b"},
		{model.HostOSLinux, "/", "/a/b"},
		{model.HostOSWindows, `C:\Users`, `C:\Users\a\b`},
		{model.HostOSWindows, `C:/Users`, `C:\Users\a\b`},
		{model.HostOSWindows, `C:\`, `C:\a\b`},
	}
	for _, j := range joins {
		if got := joinPath(j.os, j.dir, "a", "b"); got != j.want {
			t.Errorf("joinPath(%s, %q) = %q; want %q", j.os, j.dir, got, j.want)
		}
	}
}
