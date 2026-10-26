package broker

import (
	"context"
	"encoding/json"
	"time"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
)

// TurnLaunch describes one headless launch. Session is an existing session to
// resume, or a broker-assigned UUID for Claude when NewSession is true. Drivers
// must use per-launch configuration and send the prompt on stdin, never argv.
type TurnLaunch struct {
	Harness                                       model.Harness
	Profile                                       model.Profile
	Workdir, StateDir, LaunchID, MemberID, Binary string
	Session                                       *string
	NewSession                                    bool
}

// TurnAcknowledgement reports the harness's acknowledgement and optional session
// id. Send it once, then close the channel. Wait must also retain it in Outcome
// so an exit racing the acknowledgement cannot lose it.
type TurnAcknowledgement struct{ Session *string }

// TurnOutcome is the driver's final result. Error is a one-line explanation.
// Acknowledged is independent of ExitCode; zero without acknowledgement failed.
type TurnOutcome struct {
	ExitCode     *int64
	Acknowledged bool
	Session      *string
	Text         string
	Usage        *model.Usage
	UsageRaw     json.RawMessage
	Error        string
	Cancelled    bool
}

// Headless starts a process promptly and returns its handle. ctx governs startup
// only: loss of the runner must leave the process alive. Cancel, not cancellation
// of the runner's context, terminates the process group/job and all its children.
type Headless interface {
	// Start returns promptly with a PID-bearing handle, or an error before any
	// process was started. The prompt goes on stdin; ctx is startup-only.
	Start(context.Context, TurnLaunch, string) (TurnHandle, error)
}

// TurnHandle belongs to one process. PID is available immediately; Wait is called
// once and drains output until exit. Cancel is idempotent and must return promptly
// after initiating process-tree termination; Wait then completes promptly.
type TurnHandle interface {
	// PID returns the started process ID; it must be positive and stable.
	PID() int64
	// Acknowledgements yields at most one acknowledgement, then closes.
	Acknowledgements() <-chan TurnAcknowledgement
	// Wait is called once, drains output, and retains acknowledgement/session
	// information even if exit races the acknowledgement channel.
	Wait() TurnOutcome
	// Cancel initiates termination of the process group/job, is idempotent,
	// and makes Wait complete promptly. It must never signal by process name.
	Cancel() error
}

// LaunchFiles writes/removes only harness-specific per-launch files. The runner
// owns the token and overflow files. Delete must tolerate absent files. Methods
// run outside store transactions and must not change global harness config.
type LaunchFiles interface {
	// Write creates private per-launch config after the runner wrote the token.
	// It must honor ctx and never edit global harness configuration.
	Write(context.Context, TurnLaunch) error
	// Delete removes only this launch ID's harness config, tolerating absence.
	Delete(context.Context, string) error
}

// PaneObservation is an agent's current activity or a vanished pane/agent.
type PaneObservation struct {
	Activity model.Activity
	Gone     bool
}

// Panes provides delivery operations for a member's existing pane lease. Calls
// must honor ctx. WaitSettled waits for idle/done/blocked/unknown (not working).
// WaitActivity observes activity caused by the last Submit, including activity
// already observed before WaitActivity starts; false at the deadline is uncertain.
type Panes interface {
	// State observes the agent on this member's recorded pane lease.
	State(context.Context, model.Member) (PaneObservation, error)
	// Submit sends the prompt once and remembers the activity baseline. An error
	// means it was definitely not submitted; adapters must classify ambiguity.
	Submit(context.Context, model.Member, string) error
	// WaitSettled waits for a non-working state; timeout returns working without
	// an error so this recipient can stay busy while others continue.
	WaitSettled(context.Context, model.Member, time.Duration) (PaneObservation, error)
	// WaitActivity confirms activity since Submit, including changes already
	// observed. At the deadline it returns false without retrying submission.
	WaitActivity(context.Context, model.Member, time.Duration) (bool, error)
}

// RequestExecutor performs one running request. The runner owns its status and
// request.changed events. Adapters must fence their lease-bound writes by the
// recorded member generation and stop on ctx cancellation/runner lease loss.
type RequestExecutor interface {
	// Execute receives a request already marked running, carries out its side
	// effects, and returns its failure or nil. It must honor ctx cancellation.
	Execute(context.Context, model.Request) error
}

// PaneLifecycle is the C2d boundary for opening/converting panes and handling
// disappearance. Open covers pane-mode startup/wake and session-before-a-pane.
// Adapters own those lease changes and must fence them by member generation.
type PaneLifecycle interface {
	// Open receives a starting member and performs pane/session preparation and
	// guarded lease changes. It runs outside any store transaction.
	Open(context.Context, model.Member, model.LeaseCause) error
	// Exited handles a vanished pane, including any conversion/relaunch needed
	// by its home mode. It must guard writes by this snapshot's generation.
	Exited(context.Context, model.Member) error
}

// ProcessState identifies a running process by the OS's process-birth identity.
// Birth is opaque and stable for a PID's lifetime, not a wall-clock approximation.
type ProcessState struct {
	Alive bool
	Birth string
}

// Processes probes orphan turns and cancels only a process whose PID and birth
// still match. Empty/unknown birth must never authorize killing a reused PID.
type Processes interface {
	// Inspect reports liveness and the OS birth identity. Permission/inspection
	// errors must not be represented as a definitely absent process.
	Inspect(context.Context, int64) (ProcessState, error)
	// Cancel terminates only the matching PID/birth and its descendants; an
	// empty or mismatched identity must not authorize any signal.
	Cancel(context.Context, int64, string) error
}

// RunnerOptions supplies C2 driver adapters and isolated host paths. HomeDir is
// explicit: empty disables pointer cleanup, avoiding an implicit real home access.
// PollInterval is capped at 250 ms; zero uses 250 ms. Other zero durations use
// protocol defaults. TurnTimeout overrides the team timeout only for tests.
type RunnerOptions struct {
	Headless                                                  Headless
	Files                                                     LaunchFiles
	Panes                                                     Panes
	Requests                                                  RequestExecutor
	PaneLifecycle                                             PaneLifecycle
	Processes                                                 Processes
	HomeDir                                                   string
	Pane                                                      *string
	PID                                                       int64
	PollInterval, HeartbeatInterval, TurnTimeout, PaneTimeout time.Duration
	Binary                                                    func(model.Harness) (string, error)
}

// OutcomeStatus is the pure outcome-to-status rule. Timeout takes precedence over
// cancellation; a clean exit without acknowledgement is always failed.
func OutcomeStatus(out TurnOutcome, timedOut bool) model.TurnStatus {
	if timedOut {
		return model.TurnStatusTimedOut
	}
	if out.Cancelled {
		return model.TurnStatusCancelled
	}
	if out.Acknowledged && out.ExitCode != nil && *out.ExitCode == 0 && out.Error == "" {
		return model.TurnStatusSucceeded
	}
	return model.TurnStatusFailed
}

// NotBuiltRunnerDrivers supplies all C2a defaults. It never executes host tools.
// The runner records pane-open failures as stopped/launch_failed, and requests
// as running then failed. C2d replaces these defaults with real adapters.
type NotBuiltRunnerDrivers struct{}

func runnerNotBuilt() *model.Error {
	return model.NewError(model.ErrorInternal, "runner driver is C2d's responsibility").WithDetail("reason", "not_built")
}
func (NotBuiltRunnerDrivers) Start(context.Context, TurnLaunch, string) (TurnHandle, error) {
	return nil, runnerNotBuilt()
}
func (NotBuiltRunnerDrivers) Write(context.Context, TurnLaunch) error { return runnerNotBuilt() }
func (NotBuiltRunnerDrivers) Delete(context.Context, string) error    { return nil }
func (NotBuiltRunnerDrivers) State(context.Context, model.Member) (PaneObservation, error) {
	return PaneObservation{}, runnerNotBuilt()
}
func (NotBuiltRunnerDrivers) Submit(context.Context, model.Member, string) error {
	return runnerNotBuilt()
}
func (NotBuiltRunnerDrivers) WaitSettled(context.Context, model.Member, time.Duration) (PaneObservation, error) {
	return PaneObservation{}, runnerNotBuilt()
}
func (NotBuiltRunnerDrivers) WaitActivity(context.Context, model.Member, time.Duration) (bool, error) {
	return false, runnerNotBuilt()
}
func (NotBuiltRunnerDrivers) Execute(context.Context, model.Request) error { return runnerNotBuilt() }
func (NotBuiltRunnerDrivers) Open(context.Context, model.Member, model.LeaseCause) error {
	return runnerNotBuilt()
}
func (NotBuiltRunnerDrivers) Exited(context.Context, model.Member) error { return runnerNotBuilt() }
