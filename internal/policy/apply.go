package policy

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
)

// ApplyInput is apply-team's request with the store facts it needs.
type ApplyInput struct {
	Spec       model.TeamSpec
	IfRevision int64
	Facts      SpecFacts
}

// ApplyPlan is what an accepted apply-team writes.
//
// The service layer, in one transaction: creates the team (and its human
// member, a fresh ULID) when Create, else updates it; sets the revision;
// writes the team fields and limits from Spec; inserts Added; updates Changed;
// sets removed_at and removed_reason removed_from_spec on Removed; replaces
// the grid with Grid, resolving names against the live roster after the
// apply. Then it writes member.removed for each Removed, member.added for
// human (on Create) and each Added, and team.applied, each a snapshot taken
// after the writes.
type ApplyPlan struct {
	Create bool
	// Revision is the team's revision after the apply.
	Revision int64
	// Spec is the request's spec with defaults filled: what get-team returns
	// until the next apply or spawn.
	Spec   model.TeamSpec
	Limits Limits
	// Added are spec members new to the team, in spec order, with defaults.
	Added []model.MemberSpec
	// Changed are live spec members whose settings differ, in spec order.
	Changed []MemberChange
	// Removed are live spec members missing from the spec, in stored order.
	Removed []model.MemberRef
	// Grid is exactly spec.grid. Spawn edges left out of it are revoked.
	Grid []model.GridEdge
	// TeamFields are the team-level fields that change, as JSON pointers:
	// /name, /pattern, /herdr/server, /limits.
	TeamFields []string
	Warnings   []Warning
}

// MemberChange is a live spec member whose settings an apply changes. Every
// change is written at once. Grid, limits and spawn settings act at once; a
// brief from the member's next new session; a task at the next fresh start;
// profile and mode from its next launch.
type MemberChange struct {
	ID   string
	Name string
	// Fields are the changed member fields: name, role, harness, folder,
	// worktree, profile, mode, brief, task, spawn.
	Fields []string
	// NextLaunch is true when profile or mode changed on a member that isn't
	// stopped: the new value applies from its next launch (warning next_launch).
	NextLaunch bool
}

// PlanApply decides an apply-team. The checks run in this order, and the
// first failure is the error:
//
//  1. if_revision: 0 creates and fails with revision_conflict if the team
//     exists; otherwise it must equal the stored revision (a missing team
//     counts as revision 0). details: team, expected, actual.
//  2. ValidateSpec: unsupported_version, invalid_request, name_taken.
//  3. A team that isn't stopped refuses changes that need it stopped:
//     team_running with details.fields, JSON pointers into the spec (an
//     added member is /members/<i>; a removed one adds /members).
//
// Warnings: folder_missing, folder_not_git, harness_unavailable, next_launch.
func PlanApply(in ApplyInput, host Host) (ApplyPlan, *model.Error) {
	cur := in.Facts.Current
	if err := checkRevision(in.Spec, in.IfRevision, cur); err != nil {
		return ApplyPlan{}, err
	}
	if err := ValidateSpec(in.Spec, in.Facts, host.OS()); err != nil {
		return ApplyPlan{}, err
	}
	spec := ApplyDefaults(in.Spec)
	plan := ApplyPlan{
		Create:   cur == nil,
		Revision: in.IfRevision + 1,
		Spec:     spec,
		Limits:   ResolveLimits(spec.Limits),
		Grid:     spec.Grid,
		Added:    []model.MemberSpec{},
		Changed:  []MemberChange{},
		Removed:  []model.MemberRef{},
		Warnings: specWarnings(spec, host),
	}
	if cur == nil {
		plan.Added = append(plan.Added, spec.Members...)
		plan.Warnings = orderWarnings(spec, plan.Warnings)
		return plan, nil
	}

	old := ApplyDefaults(cur.Spec)
	stopFields := []string{}
	if old.Name != spec.Name {
		plan.TeamFields = append(plan.TeamFields, "/name")
		stopFields = append(stopFields, "/name")
	}
	if old.Pattern != spec.Pattern {
		plan.TeamFields = append(plan.TeamFields, "/pattern")
	}
	if *old.Herdr.Server != *spec.Herdr.Server {
		plan.TeamFields = append(plan.TeamFields, "/herdr/server")
		stopFields = append(stopFields, "/herdr/server")
	}
	if ResolveLimits(old.Limits) != plan.Limits {
		plan.TeamFields = append(plan.TeamFields, "/limits")
	}

	oldByID := map[string]model.MemberSpec{}
	for _, m := range old.Members {
		oldByID[m.ID] = m
	}
	newIDs := map[string]bool{}
	for i, m := range spec.Members {
		newIDs[m.ID] = true
		was, ok := oldByID[m.ID]
		if !ok {
			plan.Added = append(plan.Added, m)
			stopFields = append(stopFields, fmt.Sprintf("/members/%d", i))
			continue
		}
		fields := memberDiff(was, m)
		if len(fields) == 0 {
			continue
		}
		change := MemberChange{ID: m.ID, Name: m.Name, Fields: fields}
		for _, f := range fields {
			switch f {
			case "name", "harness", "folder", "worktree":
				stopFields = append(stopFields, fmt.Sprintf("/members/%d/%s", i, f))
			}
		}
		if rec := cur.member(m.ID); rec != nil && rec.State != model.MemberStateStopped {
			var moved []string
			if was.Profile != m.Profile {
				moved = append(moved, fmt.Sprintf("profile from %s to %s", was.Profile, m.Profile))
			}
			if was.Mode != m.Mode {
				moved = append(moved, fmt.Sprintf("mode from %s to %s", was.Mode, m.Mode))
			}
			if len(moved) > 0 {
				change.NextLaunch = true
				msg := fmt.Sprintf("%s is %s: its %s", m.Name, rec.State, moved[0])
				if len(moved) == 2 {
					msg += " and its " + moved[1]
				}
				id := m.ID
				plan.Warnings = append(plan.Warnings, Warning{Code: model.WarningCodeNextLaunch, Member: &id,
					Message: msg + " apply from its next launch"})
			}
		}
		plan.Changed = append(plan.Changed, change)
	}
	removed := false
	for _, m := range old.Members {
		if !newIDs[m.ID] {
			plan.Removed = append(plan.Removed, model.MemberRef{ID: m.ID, Name: m.Name})
			removed = true
		}
	}
	if removed {
		stopFields = append(stopFields, "/members")
	}

	if cur.State != model.TeamStateStopped && len(stopFields) > 0 {
		return ApplyPlan{}, model.NewError(model.ErrorTeamRunning,
			fmt.Sprintf("team %s is %s: stop it to change %s", old.Name, cur.State, strings.Join(stopFields, ", "))).
			WithDetail("fields", stopFields)
	}
	plan.Revision = cur.Revision + 1
	plan.Warnings = orderWarnings(spec, plan.Warnings)
	return plan, nil
}

func checkRevision(spec model.TeamSpec, ifRevision int64, cur *Team) *model.Error {
	if ifRevision < 0 {
		return model.NewError(model.ErrorInvalidRequest, "if_revision must be 0 or more").
			WithDetail("errors", []FieldError{{Path: "/if_revision", Message: "must be 0 or more"}})
	}
	var actual int64
	if cur != nil {
		actual = cur.Revision
	}
	if ifRevision == actual {
		return nil
	}
	var msg string
	switch {
	case cur == nil:
		msg = fmt.Sprintf("team %s doesn't exist: apply with if_revision 0 to create it", spec.ID)
	case ifRevision == 0:
		msg = fmt.Sprintf("team %s already exists at revision %d: re-read it and apply again", cur.Spec.Name, actual)
	default:
		msg = fmt.Sprintf("team %s is at revision %d, not %d: re-read it and apply again", cur.Spec.Name, actual, ifRevision)
	}
	return model.NewError(model.ErrorRevisionConflict, msg).WithDetails(map[string]any{
		"team": spec.ID, "expected": ifRevision, "actual": actual,
	})
}

// memberDiff lists the fields that differ between two defaulted member specs.
func memberDiff(a, b model.MemberSpec) []string {
	var out []string
	if a.Name != b.Name {
		out = append(out, "name")
	}
	if a.Role != b.Role {
		out = append(out, "role")
	}
	if a.Harness != b.Harness {
		out = append(out, "harness")
	}
	if a.Folder != b.Folder {
		out = append(out, "folder")
	}
	if *a.Worktree != *b.Worktree {
		out = append(out, "worktree")
	}
	if a.Profile != b.Profile {
		out = append(out, "profile")
	}
	if a.Mode != b.Mode {
		out = append(out, "mode")
	}
	if a.Brief != b.Brief {
		out = append(out, "brief")
	}
	if !reflect.DeepEqual(a.Task, b.Task) {
		out = append(out, "task")
	}
	if !reflect.DeepEqual(spawnOf(a.Spawn), spawnOf(b.Spawn)) {
		out = append(out, "spawn")
	}
	return out
}

// spawnView compares spawn settings after defaults. Absent and present-but-off
// differ, because the store keeps a spawn_settings row only for the latter.
type spawnView struct {
	present bool
	cfg     SpawnConfig
}

func spawnOf(s *model.SpawnSettings) spawnView {
	return spawnView{present: s != nil, cfg: ResolveSpawn(s)}
}

// orderWarnings sorts warnings by member in spec order, keeping each
// member's warnings in the order they were raised.
func orderWarnings(spec model.TeamSpec, ws []Warning) []Warning {
	out := make([]Warning, 0, len(ws))
	for _, m := range spec.Members {
		for _, w := range ws {
			if w.Member != nil && *w.Member == m.ID {
				out = append(out, w)
			}
		}
	}
	for _, w := range ws {
		if w.Member == nil {
			out = append(out, w)
		}
	}
	return out
}
