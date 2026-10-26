//go:build !linux

package broker

import "context"

// EnableSubreaper is a no-op on platforms without Linux's subreaper facility.
// The harness driver still controls its process group/job and known ancestry.
func EnableSubreaper() error     { return nil }
func startProtection() func(int) { return func(int) {} }
func releaseProtection(int)      {}
func reapRunnerOrphans()         {}

func cleanupAdoptedDescendants(context.Context) error { return nil }
