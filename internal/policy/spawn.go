package policy

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
)

// ProfileRank orders profiles: read-only < workspace < bypass. An unknown
// profile ranks -1.
func ProfileRank(p model.Profile) int {
	switch p {
	case model.ProfileReadOnly:
		return 0
	case model.ProfileWorkspace:
		return 1
	case model.ProfileBypass:
		return 2
	}
	return -1
}

// LowerProfile returns the lower of two profiles.
func LowerProfile(a, b model.Profile) model.Profile {
	if ProfileRank(b) < ProfileRank(a) {
		return b
	}
	return a
}

// HighestWorkerProfile is the effective highest profile a spawner may give a
// worker: the lower of spawn.max_profile and its own profile.
func HighestWorkerProfile(s SpawnConfig, spawner model.Profile) model.Profile {
	return LowerProfile(s.MaxProfile, spawner)
}

// DefaultWorkerProfile is a worker's profile when spawn names none: the lower
// of workspace and the effective highest.
func DefaultWorkerProfile(s SpawnConfig, spawner model.Profile) model.Profile {
	return LowerProfile(model.ProfileWorkspace, HighestWorkerProfile(s, spawner))
}

// SpawnUsesWorktree is the folder decision for a spawned worker: a new
// worktree of the spawner's workdir with spawn.folder worktree, except for a
// read-only worker or a workdir outside git, which share the workdir. With
// spawn.folder shared it always shares.
func SpawnUsesWorktree(folder model.SpawnFolder, profile model.Profile, inGit bool) bool {
	return folder == model.SpawnFolderWorktree && profile != model.ProfileReadOnly && inGit
}

// SpawnInput is a spawn call with the facts its checks read.
type SpawnInput struct {
	// The call: name, harness and task are required; Profile and Mode are
	// optional.
	Name    string
	Harness model.Harness
	Task    string
	Profile *model.Profile
	Mode    *model.Mode

	TeamState model.TeamState
	// RunnerUp: the runner's heartbeat is fresh.
	RunnerUp bool
	// Caller is the spawner's member record.
	Caller model.Member
	// Settings are the caller's spawn settings; the zero value if it has none.
	Settings SpawnConfig
	// Live is the team's live roster, human included.
	Live []model.Member
	// Running counts the caller's workers that are starting, running or
	// stopping; SpawnedThisRun those spawned since the team's last start, in
	// any state (the spawn_counts view).
	Running        int
	SpawnedThisRun int
}

// SpawnPlan is a spawn that passed its checks. The caller, in the same
// transaction: inserts the worker (origin spawn, role worker, state starting,
// spawned_by the caller, spawned_run this run, harness, profile, mode, folder,
// brief); adds Edges to the grid; accepts Task as a message from the caller,
// kind task, with Edges in the grid (it counts as a hop); bumps the team
// revision; and creates a spawn request carrying Worktree for the runner.
// Events: member.added, message.created and its budget events,
// request.changed.
type SpawnPlan struct {
	Name    string
	Harness model.Harness
	Profile model.Profile
	Mode    model.Mode
	Role    model.Role
	// Brief is the spawner's spawn.brief.
	Brief string
	// Folder is the spawner's workdir: the worker's configured folder and the
	// base of its worktree.
	Folder string
	// Worktree: the runner makes a worktree of Folder (PlanWorktree). False:
	// the worker shares Folder.
	Worktree bool
	Edges    []model.GridEdge
	Task     string
}

// SpawnDecision is the outcome of a spawn: Plan, or Err and the events to
// write for the refusal.
type SpawnDecision struct {
	Plan *SpawnPlan
	Err  *model.Error
	// Events holds policy.denied when the refusal is a policy one (the 20–26
	// error codes: nesting_not_allowed, spawn_disabled, name_taken,
	// harness_not_allowed, profile_too_high, spawn_cap_reached). Not-running,
	// runner, input and host failures write none.
	Events []Event
}

// Spawn runs the twelve spawn checks in order; the first failure wins.
//
//  1. The team is running: team_not_running.
//  2. The runner is up: runner_unavailable.
//  3. The caller wasn't spawned: nesting_not_allowed.
//  4. The caller's spawn.enabled: spawn_disabled.
//  5. The call is well formed (name a valid agent name; harness, profile and
//     mode valid; task 1 to 65,536 code points): invalid_request with
//     details.errors.
//  6. name isn't a live member's: name_taken.
//  7. harness is in spawn.harnesses, or that's null: harness_not_allowed.
//  8. The host has harness at a supported version: harness_unavailable with
//     details.reason.
//  9. profile is within the effective highest: profile_too_high.
//  10. The caller's running workers are under running_at_once,
//  11. its workers spawned this run under per_run, and
//  12. live agent members under 24: spawn_cap_reached with details cap,
//     limit and current.
func Spawn(in SpawnInput, host Host) SpawnDecision {
	refuse := func(err *model.Error, policy bool) SpawnDecision {
		d := SpawnDecision{Err: err, Events: []Event{}}
		if policy {
			d.Events = append(d.Events, policyDenied(model.PolicyActionSpawn, ref(in.Caller), nil, nil, err))
		}
		return d
	}
	caller := in.Caller.Name

	if in.TeamState != model.TeamStateRunning {
		return refuse(model.NewError(model.ErrorTeamNotRunning, "the team isn't running"), false)
	}
	if !in.RunnerUp {
		return refuse(model.NewError(model.ErrorRunnerUnavailable, "the team's runner isn't up"), false)
	}
	if in.Caller.Origin == model.MemberOriginSpawn || in.Caller.SpawnedBy != nil {
		return refuse(model.NewError(model.ErrorNestingNotAllowed,
			fmt.Sprintf("%s was spawned, and spawned workers can't spawn", caller)), true)
	}
	if !in.Settings.Enabled {
		return refuse(model.NewError(model.ErrorSpawnDisabled, fmt.Sprintf("%s can't spawn workers", caller)), true)
	}

	var errs fieldErrors
	if model.ValidateAgentName(in.Name) != nil {
		errs.add("/name", "must be a lower-case letter, then up to 31 of a-z, 0-9 and -, and not human or all")
	}
	if !in.Harness.Valid() {
		errs.add("/harness", "must be one of %v", model.Harness("").Values())
	}
	checkText(&errs, "/task", in.Task)
	if in.Profile != nil && !in.Profile.Valid() {
		errs.add("/profile", "must be one of %v", model.Profile("").Values())
	}
	if in.Mode != nil && !in.Mode.Valid() {
		errs.add("/mode", "must be one of %v", model.Mode("").Values())
	}
	if len(errs) > 0 {
		return refuse(model.NewError(model.ErrorInvalidRequest, errs[0].Path[1:]+": "+errs[0].Message).
			WithDetail("errors", []FieldError(errs)), false)
	}

	for _, m := range in.Live {
		if m.Name == in.Name && m.RemovedAt == nil {
			return refuse(model.NewError(model.ErrorNameTaken,
				fmt.Sprintf("%s is already a member of this team", in.Name)).WithDetail("name", in.Name), true)
		}
	}
	if hs := in.Settings.Harnesses; hs != nil && !slices.Contains(hs, in.Harness) {
		return refuse(model.NewError(model.ErrorHarnessNotAllowed,
			fmt.Sprintf("%s may not spawn %s workers (allowed: %s)", caller, in.Harness, joinHarnesses(hs))).
			WithDetails(map[string]any{"harness": in.Harness, "allowed": slices.Clone(hs)}), true)
	}
	if issue := host.HarnessIssue(in.Harness); issue != "" {
		return refuse(model.NewError(model.ErrorHarnessUnavailable,
			fmt.Sprintf("%s isn't available on this host (%s)", in.Harness, issue)).
			WithDetails(map[string]any{"harness": in.Harness, "reason": issue}), false)
	}

	callerProfile := model.ProfileReadOnly // an agent always has one; fail closed
	if in.Caller.Profile != nil {
		callerProfile = *in.Caller.Profile
	}
	highest := HighestWorkerProfile(in.Settings, callerProfile)
	profile := DefaultWorkerProfile(in.Settings, callerProfile)
	if in.Profile != nil {
		profile = *in.Profile
	}
	if ProfileRank(profile) > ProfileRank(highest) {
		return refuse(model.NewError(model.ErrorProfileTooHigh,
			fmt.Sprintf("%s is above %s's highest spawn profile, %s", profile, caller, highest)).
			WithDetails(map[string]any{"requested": profile, "highest": highest}), true)
	}

	if in.Running >= in.Settings.RunningAtOnce {
		return refuse(capReached(model.SpawnCapRunningAtOnce, in.Settings.RunningAtOnce, in.Running,
			fmt.Sprintf("%s already has %d workers running (running at once: %d)", caller, in.Running, in.Settings.RunningAtOnce)), true)
	}
	if in.SpawnedThisRun >= in.Settings.PerRun {
		return refuse(capReached(model.SpawnCapPerRun, in.Settings.PerRun, in.SpawnedThisRun,
			fmt.Sprintf("%s has spawned %d workers this run (per run: %d)", caller, in.SpawnedThisRun, in.Settings.PerRun)), true)
	}
	agents := 0
	for _, m := range in.Live {
		if m.Kind == model.MemberKindAgent && m.RemovedAt == nil {
			agents++
		}
	}
	if agents >= MaxLiveAgents {
		return refuse(capReached(model.SpawnCapTeamSize, MaxLiveAgents, agents,
			fmt.Sprintf("the team already has %d live agent members (team size: %d)", agents, MaxLiveAgents)), true)
	}

	mode := in.Settings.Mode
	if in.Mode != nil {
		mode = *in.Mode
	}
	folder := ""
	switch {
	case in.Caller.Workdir != nil:
		folder = *in.Caller.Workdir
	case in.Caller.Folder != nil:
		folder = *in.Caller.Folder
	}
	_, inGit := host.RepoRoot(folder)
	return SpawnDecision{Events: []Event{}, Plan: &SpawnPlan{
		Name:     in.Name,
		Harness:  in.Harness,
		Profile:  profile,
		Mode:     mode,
		Role:     model.RoleWorker,
		Brief:    in.Settings.Brief,
		Folder:   folder,
		Worktree: SpawnUsesWorktree(in.Settings.Folder, profile, inGit),
		Edges:    SpawnEdges(caller, in.Name),
		Task:     in.Task,
	}}
}

func capReached(c model.SpawnCap, limit, current int, msg string) *model.Error {
	return model.NewError(model.ErrorSpawnCapReached, msg).
		WithDetails(map[string]any{"cap": c, "limit": limit, "current": current})
}

func joinHarnesses(hs []model.Harness) string {
	s := make([]string, len(hs))
	for i, h := range hs {
		s[i] = string(h)
	}
	return strings.Join(s, ", ")
}

// StopDecision is the outcome of an agent's stop(member).
type StopDecision struct {
	// AlreadyStopped: return the worker's record; nothing changes.
	AlreadyStopped bool
	// Request: create a stop-member request for the runner.
	Request bool
	Err     *model.Error
	// Events holds policy.denied when the caller isn't the target's spawner.
	Events []Event
}

// Stop decides an agent's stop(member). Only the worker's spawner may stop
// it (denied, details.reason not_spawner, and policy.denied otherwise). A
// worker already stopped is returned as is. Stopping a live one needs the
// runner (runner_unavailable).
func Stop(caller, target model.Member, runnerUp bool) StopDecision {
	if target.SpawnedBy == nil || target.SpawnedBy.ID != caller.ID {
		err := model.NewError(model.ErrorDenied,
			fmt.Sprintf("%s didn't spawn %s, so it can't stop it", caller.Name, target.Name)).
			WithDetail("reason", "not_spawner")
		t := ref(target)
		return StopDecision{Err: err, Events: []Event{policyDenied(model.PolicyActionStop, ref(caller), &t, nil, err)}}
	}
	if target.State == model.MemberStateStopped {
		return StopDecision{AlreadyStopped: true, Events: []Event{}}
	}
	if !runnerUp {
		return StopDecision{Err: model.NewError(model.ErrorRunnerUnavailable, "the team's runner isn't up"), Events: []Event{}}
	}
	return StopDecision{Request: true, Events: []Event{}}
}

// Hold TTL bounds, in seconds.
const (
	HoldMinSeconds = 10
	HoldMaxSeconds = 120
)

// HoldDecision is the outcome of the hold op.
type HoldDecision struct {
	// Release: end the hold now (held_until null), and move the member's
	// deliveries held for person_control back to queued.
	Release bool
	// HeldUntil is now plus the TTL when starting or renewing a hold.
	HeldUntil *time.Time
}

// Hold decides the hold op. It needs a running team (team_not_running) and
// an agent member (invalid_request for human). Starting or renewing a hold
// needs the member running and a TTL of 10 to 120 seconds; a release works
// whatever the member's state.
func Hold(now time.Time, teamState model.TeamState, member model.Member, ttlSeconds *int, release bool) (HoldDecision, *model.Error) {
	if teamState != model.TeamStateRunning {
		return HoldDecision{}, model.NewError(model.ErrorTeamNotRunning, "the team isn't running")
	}
	if member.Kind == model.MemberKindHuman {
		return HoldDecision{}, invalid("/member", "human can't be held")
	}
	if release == (ttlSeconds != nil) {
		return HoldDecision{}, invalid("/ttl_seconds", "give either ttl_seconds or release: true")
	}
	if release {
		return HoldDecision{Release: true}, nil
	}
	if member.State != model.MemberStateRunning {
		return HoldDecision{}, invalid("/member", "%s is %s, not running", member.Name, member.State)
	}
	if *ttlSeconds < HoldMinSeconds || *ttlSeconds > HoldMaxSeconds {
		return HoldDecision{}, invalid("/ttl_seconds", "must be from %d to %d", HoldMinSeconds, HoldMaxSeconds)
	}
	until := now.Add(time.Duration(*ttlSeconds) * time.Second)
	return HoldDecision{HeldUntil: &until}, nil
}
