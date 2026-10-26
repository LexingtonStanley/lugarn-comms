// Package herdr drives Herdr, the terminal multiplexer that hosts pane
// members, through its CLI (`herdr --session <name> <command>`, JSON out).
//
// Every call names its server with --session and runs with every HERDR_*
// variable removed from the child's environment, so a Client can only reach
// the server it was given, even when the broker itself runs inside a Herdr
// pane. Nothing goes through a shell: arguments, prompt text included, are
// single argv elements, and Herdr is a native executable on every OS.
//
// Failures are *Error values that unwrap to the protocol's *model.Error
// (herdr_unavailable, not_found, timeout, launch_failed, invalid_request or
// internal), with Herdr's own code and message in its details. IsGone,
// IsBlocked, IsTimeout and IsUnavailable classify them for the runner.
//
// Behaviour this package relies on was checked live against Herdr 0.9.5 on
// Linux; see the C2b report for the commands and output.
package herdr
