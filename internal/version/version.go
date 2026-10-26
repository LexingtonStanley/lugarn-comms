// Package version holds build metadata and the supported protocol version.
package version

// Version and Commit can be set at build time with -ldflags -X.
var (
	Version = "dev"
	Commit  = "unknown"
)

const Protocol = 1
