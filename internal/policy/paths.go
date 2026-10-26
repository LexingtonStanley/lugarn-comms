package policy

import (
	"strings"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
)

// Paths here follow the host's syntax, which is an input (Host.OS), not the
// build's: path/filepath would answer for the machine that compiled the
// broker. POSIX paths use '/'. Windows paths accept '\' and '/' and come out
// with '\'.

// IsAbsPath reports whether p is absolute in the syntax of os. On Linux and
// macOS that means it starts with '/'. On Windows it is a drive path (C:\ or
// C:/) or a UNC path (\\server\share, including \\?\ and \\.\ device paths);
// a rooted path without a drive (\x) and a drive-relative one (C:x) are not
// absolute. A path containing NUL is never absolute.
func IsAbsPath(os model.HostOS, p string) bool {
	if p == "" || strings.IndexByte(p, 0) >= 0 {
		return false
	}
	if os == model.HostOSWindows {
		return isWindowsAbs(p)
	}
	return p[0] == '/'
}

func isWindowsSep(c byte) bool { return c == '\\' || c == '/' }

func isASCIILetter(c byte) bool { return 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' }

func isWindowsAbs(p string) bool {
	if len(p) >= 3 && isASCIILetter(p[0]) && p[1] == ':' && isWindowsSep(p[2]) {
		return true
	}
	if len(p) >= 2 && isWindowsSep(p[0]) && isWindowsSep(p[1]) {
		rest := p[2:]
		i := strings.IndexAny(rest, `\/`)
		if i <= 0 {
			return false
		}
		share := rest[i+1:]
		return share != "" && !isWindowsSep(share[0])
	}
	return false
}

func pathSep(os model.HostOS) string {
	if os == model.HostOSWindows {
		return `\`
	}
	return "/"
}

// nativePath rewrites a Windows path with '\' separators; POSIX paths pass
// through unchanged.
func nativePath(os model.HostOS, p string) string {
	if os == model.HostOSWindows {
		return strings.ReplaceAll(p, "/", `\`)
	}
	return p
}

// splitPath returns the parent directory and final element of an absolute
// path, ignoring trailing separators. ok is false when the path has no final
// element (a root such as "/" or C:\).
func splitPath(os model.HostOS, p string) (parent, base string, ok bool) {
	p = nativePath(os, p)
	sep := pathSep(os)
	trimmed := strings.TrimRight(p, sep)
	i := strings.LastIndex(trimmed, sep)
	if i < 0 {
		return "", "", false
	}
	base = trimmed[i+1:]
	parent = trimmed[:i]
	if base == "" {
		return "", "", false
	}
	switch {
	case parent == "" && os != model.HostOSWindows:
		parent = "/"
	case os == model.HostOSWindows && len(parent) == 2 && parent[1] == ':':
		parent += sep
	case parent == "" || parent == sep:
		return "", "", false
	case os == model.HostOSWindows && strings.HasPrefix(parent, `\\`) && !strings.Contains(parent[2:], sep):
		// \\server\share is a UNC root: \\server alone is no directory.
		return "", "", false
	}
	return parent, base, true
}

// joinPath appends elements to dir with the host's separator.
func joinPath(os model.HostOS, dir string, elems ...string) string {
	sep := pathSep(os)
	var b strings.Builder
	b.WriteString(nativePath(os, dir))
	for _, e := range elems {
		if !strings.HasSuffix(b.String(), sep) {
			b.WriteString(sep)
		}
		b.WriteString(e)
	}
	return b.String()
}
