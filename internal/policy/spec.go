package policy

import (
	"fmt"
	"regexp"
	"slices"
	"unicode/utf8"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
)

// MaxLiveAgents caps a team's live agent members, spec plus spawned.
const MaxLiveAgents = 24

// SupportedSpecVersions are the team-spec versions this broker accepts
// (hello.schemas.team_spec).
var SupportedSpecVersions = []int{1}

// Limits are a team's limits with defaults filled in.
type Limits struct {
	HopBudget          int
	ReviewRounds       int
	PerRecipient       int
	HeadlessTurns      int
	TurnTimeoutSeconds int
}

// ResolveLimits fills the defaults into l, which may be nil.
func ResolveLimits(l *model.Limits) Limits {
	out := Limits{
		HopBudget:          model.DefaultHopBudget,
		ReviewRounds:       model.DefaultReviewRounds,
		PerRecipient:       model.DefaultPerRecipient,
		HeadlessTurns:      model.DefaultHeadlessTurns,
		TurnTimeoutSeconds: model.DefaultTurnTimeoutSeconds,
	}
	if l == nil {
		return out
	}
	setInt(&out.HopBudget, l.HopBudget)
	setInt(&out.ReviewRounds, l.ReviewRounds)
	setInt(&out.PerRecipient, l.PerRecipient)
	setInt(&out.HeadlessTurns, l.HeadlessTurns)
	setInt(&out.TurnTimeoutSeconds, l.TurnTimeoutSeconds)
	return out
}

// SpawnConfig is a member's spawn settings with defaults filled in. The zero
// value (Enabled false) is a member that can't spawn.
type SpawnConfig struct {
	Enabled       bool
	RunningAtOnce int
	PerRun        int
	// Harnesses is nil for every harness the host supports.
	Harnesses  []model.Harness
	Mode       model.Mode
	Folder     model.SpawnFolder
	MaxProfile model.Profile
	// Brief is the worker brief template; "" when none was given.
	Brief string
}

// ResolveSpawn fills the defaults into s. Absent settings (nil) mean spawning
// is off.
func ResolveSpawn(s *model.SpawnSettings) SpawnConfig {
	out := SpawnConfig{
		RunningAtOnce: model.DefaultSpawnRunningAtOnce,
		PerRun:        model.DefaultSpawnPerRun,
		Mode:          model.DefaultSpawnMode,
		Folder:        model.DefaultSpawnFolder,
		MaxProfile:    model.DefaultSpawnMaxProfile,
	}
	if s == nil {
		return out
	}
	out.Enabled = s.Enabled
	setInt(&out.RunningAtOnce, s.RunningAtOnce)
	setInt(&out.PerRun, s.PerRun)
	if s.Harnesses != nil {
		out.Harnesses = slices.Clone(*s.Harnesses)
	}
	if s.Mode != nil {
		out.Mode = *s.Mode
	}
	if s.Folder != nil {
		out.Folder = *s.Folder
	}
	if s.MaxProfile != nil {
		out.MaxProfile = *s.MaxProfile
	}
	if s.Brief != nil {
		out.Brief = *s.Brief
	}
	return out
}

func setInt(dst *int, src *int) {
	if src != nil {
		*dst = *src
	}
}

// ApplyDefaults returns a deep copy of spec with every optional field filled:
// herdr.server, every limit, each member's worktree flag, and every field of
// spawn settings that are present. Absent spawn settings stay absent (spawning
// off) and null harnesses stay null (every supported harness). A nil grid
// becomes empty. This is the spec get-team returns. spec is not modified.
func ApplyDefaults(spec model.TeamSpec) model.TeamSpec {
	out := spec
	server := model.DefaultHerdrServer
	if spec.Herdr != nil && spec.Herdr.Server != nil {
		server = *spec.Herdr.Server
	}
	out.Herdr = &model.HerdrSettings{Server: &server}

	l := ResolveLimits(spec.Limits)
	out.Limits = &model.Limits{
		HopBudget:          ptr(l.HopBudget),
		ReviewRounds:       ptr(l.ReviewRounds),
		PerRecipient:       ptr(l.PerRecipient),
		HeadlessTurns:      ptr(l.HeadlessTurns),
		TurnTimeoutSeconds: ptr(l.TurnTimeoutSeconds),
	}

	out.Members = make([]model.MemberSpec, len(spec.Members))
	for i, m := range spec.Members {
		c := m
		c.Worktree = ptr(m.Worktree != nil && *m.Worktree)
		if m.Task != nil {
			c.Task = ptr(*m.Task)
		}
		c.Host = nil
		if m.Spawn != nil {
			s := ResolveSpawn(m.Spawn)
			c.Spawn = &model.SpawnSettings{
				Enabled:       s.Enabled,
				RunningAtOnce: ptr(s.RunningAtOnce),
				PerRun:        ptr(s.PerRun),
				Mode:          ptr(s.Mode),
				Folder:        ptr(s.Folder),
				MaxProfile:    ptr(s.MaxProfile),
			}
			if s.Harnesses != nil {
				c.Spawn.Harnesses = ptr(s.Harnesses)
			}
			if m.Spawn.Brief != nil {
				c.Spawn.Brief = ptr(*m.Spawn.Brief)
			}
		}
		out.Members[i] = c
	}

	out.Grid = make([]model.GridEdge, len(spec.Grid))
	for i, e := range spec.Grid {
		out.Grid[i] = model.GridEdge{From: e.From, To: e.To, Can: slices.Clone(e.Can)}
	}
	return out
}

// FieldError is one entry of an invalid_request's details.errors. Path is a
// JSON pointer into the request's spec (or the tool's input).
type FieldError struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}

// Team is the stored team that the spec rules compare against.
type Team struct {
	ID       string
	Revision int64
	State    model.TeamState
	// Spec is the stored spec as get-team rebuilds it: the live spec members
	// in spec order, the whole current grid and limits with defaults filled.
	Spec model.TeamSpec
	// Members is every member row of the team: spec members live and removed,
	// spawned members live and removed, and human.
	Members []model.Member
}

func (t *Team) member(id string) *model.Member {
	if t == nil {
		return nil
	}
	for i := range t.Members {
		if t.Members[i].ID == id {
			return &t.Members[i]
		}
	}
	return nil
}

// liveSpawned returns the team's spawned members that haven't been removed.
func (t *Team) liveSpawned() []model.Member {
	if t == nil {
		return nil
	}
	var out []model.Member
	for _, m := range t.Members {
		if m.Origin == model.MemberOriginSpawn && m.RemovedAt == nil {
			out = append(out, m)
		}
	}
	return out
}

// SpecFacts are the facts about the store that validating a spec needs.
type SpecFacts struct {
	// Current is the stored team with the spec's id, or nil if there is none.
	Current *Team
	// OtherTeamNames are the names of every other team on the host.
	OtherTeamNames []string
	// ForeignMemberIDs are member ids, among those the spec uses, that belong
	// to another team. Passing every other team's member ids also works.
	ForeignMemberIDs []string
}

var herdrServerPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// ValidateSpec checks spec as received, before defaults. It returns:
//
//   - unsupported_version (details.supported) for a version other than 1;
//   - invalid_request with details.errors [{path, message}] for every value
//     rule of team-spec.v1 that model.DecodeStrict doesn't check (required
//     fields, patterns, ranges, enums, wake without message), and for
//     checks 1–4 and 6 of Validating a spec;
//   - name_taken when another team has the name (check 5), only once the
//     spec is otherwise valid.
//
// os decides what an absolute folder is (check 4).
func ValidateSpec(spec model.TeamSpec, facts SpecFacts, os model.HostOS) *model.Error {
	if spec.Version != 0 && !slices.Contains(SupportedSpecVersions, spec.Version) {
		return model.NewError(model.ErrorUnsupportedVersion,
			fmt.Sprintf("team spec version %d isn't supported", spec.Version)).
			WithDetail("supported", slices.Clone(SupportedSpecVersions))
	}
	var errs fieldErrors
	checkSpecValues(&errs, spec)
	checkSpecRules(&errs, spec, facts, os)
	if len(errs) > 0 {
		msg := "invalid team spec: " + errs[0].Path + ": " + errs[0].Message
		if len(errs) > 1 {
			msg += fmt.Sprintf(" (and %d more)", len(errs)-1)
		}
		return model.NewError(model.ErrorInvalidRequest, msg).WithDetail("errors", []FieldError(errs))
	}
	if slices.Contains(facts.OtherTeamNames, spec.Name) {
		return model.NewError(model.ErrorNameTaken,
			fmt.Sprintf("another team on this host is already called %s", spec.Name)).
			WithDetail("name", spec.Name)
	}
	return nil
}

type fieldErrors []FieldError

func (e *fieldErrors) add(path, format string, args ...any) {
	*e = append(*e, FieldError{Path: path, Message: fmt.Sprintf(format, args...)})
}

func checkText(errs *fieldErrors, path, s string) {
	if err := model.ValidateText(s); err != nil {
		errs.add(path, "must be 1 to %d Unicode code points of valid UTF-8", model.MaxTextCodePoints)
	}
}

func checkRange(errs *fieldErrors, path string, v *int, lo, hi int) {
	if v != nil && (*v < lo || *v > hi) {
		errs.add(path, "must be from %d to %d", lo, hi)
	}
}

// checkSpecValues applies team-spec.v1's value rules.
func checkSpecValues(errs *fieldErrors, spec model.TeamSpec) {
	if spec.Version == 0 {
		errs.add("/version", "is required and must be 1")
	}
	if model.ValidateOpaqueID(spec.ID) != nil {
		errs.add("/id", "must be 1 to 64 of A-Z, a-z, 0-9, _ and -")
	}
	if model.ValidateTeamName(spec.Name) != nil {
		errs.add("/name", "must be a lower-case letter or digit, then up to 47 of a-z, 0-9 and -")
	}
	if !spec.Pattern.Valid() {
		errs.add("/pattern", "must be one of %v", model.Pattern("").Values())
	}
	if spec.Herdr != nil && spec.Herdr.Server != nil && !herdrServerPattern.MatchString(*spec.Herdr.Server) {
		errs.add("/herdr/server", "must be 1 to 64 of A-Z, a-z, 0-9, _ and -")
	}
	if l := spec.Limits; l != nil {
		checkRange(errs, "/limits/hop_budget", l.HopBudget, 1, 1000)
		checkRange(errs, "/limits/review_rounds", l.ReviewRounds, 1, 100)
		checkRange(errs, "/limits/per_recipient", l.PerRecipient, 1, 1)
		checkRange(errs, "/limits/headless_turns", l.HeadlessTurns, 1, 24)
		checkRange(errs, "/limits/turn_timeout_seconds", l.TurnTimeoutSeconds, 60, 86400)
	}
	if len(spec.Members) < 1 || len(spec.Members) > MaxLiveAgents {
		errs.add("/members", "must list 1 to %d members", MaxLiveAgents)
	}
	for i, m := range spec.Members {
		p := fmt.Sprintf("/members/%d", i)
		if model.ValidateOpaqueID(m.ID) != nil {
			errs.add(p+"/id", "must be 1 to 64 of A-Z, a-z, 0-9, _ and -")
		}
		if model.ValidateAgentName(m.Name) != nil {
			errs.add(p+"/name", "must be a lower-case letter, then up to 31 of a-z, 0-9 and -, and not human or all")
		}
		if !m.Role.Valid() {
			errs.add(p+"/role", "must be one of %v", model.Role("").Values())
		}
		if !m.Harness.Valid() {
			errs.add(p+"/harness", "must be one of %v", model.Harness("").Values())
		}
		if n := utf8.RuneCountInString(m.Folder); n < 1 || n > 4096 {
			errs.add(p+"/folder", "must be 1 to 4096 characters")
		}
		if !m.Profile.Valid() {
			errs.add(p+"/profile", "must be one of %v", model.Profile("").Values())
		}
		if !m.Mode.Valid() {
			errs.add(p+"/mode", "must be one of %v", model.Mode("").Values())
		}
		checkText(errs, p+"/brief", m.Brief)
		if m.Task != nil {
			checkText(errs, p+"/task", *m.Task)
		}
		if s := m.Spawn; s != nil {
			checkRange(errs, p+"/spawn/running_at_once", s.RunningAtOnce, 1, 24)
			checkRange(errs, p+"/spawn/per_run", s.PerRun, 1, 24)
			if s.Harnesses != nil {
				hs := *s.Harnesses
				if len(hs) == 0 {
					errs.add(p+"/spawn/harnesses", "must list at least one harness, or be null")
				}
				for j, h := range hs {
					if !h.Valid() {
						errs.add(fmt.Sprintf("%s/spawn/harnesses/%d", p, j), "must be one of %v", model.Harness("").Values())
					} else if slices.Index(hs, h) < j {
						errs.add(fmt.Sprintf("%s/spawn/harnesses/%d", p, j), "lists %s twice", h)
					}
				}
			}
			if s.Mode != nil && !s.Mode.Valid() {
				errs.add(p+"/spawn/mode", "must be one of %v", model.Mode("").Values())
			}
			if s.Folder != nil && !s.Folder.Valid() {
				errs.add(p+"/spawn/folder", "must be one of %v", model.SpawnFolder("").Values())
			}
			if s.MaxProfile != nil && !s.MaxProfile.Valid() {
				errs.add(p+"/spawn/max_profile", "must be one of %v", model.Profile("").Values())
			}
			switch {
			case s.Brief != nil:
				checkText(errs, p+"/spawn/brief", *s.Brief)
			case s.Enabled:
				errs.add(p+"/spawn/brief", "is required when spawning is enabled")
			}
		}
	}
	if spec.Grid == nil {
		errs.add("/grid", "is required; an empty list denies everything")
	}
	for i, e := range spec.Grid {
		p := fmt.Sprintf("/grid/%d", i)
		if e.From != HumanName && model.ValidateAgentName(e.From) != nil {
			errs.add(p+"/from", "must be an agent name")
		}
		if e.To != HumanName && model.ValidateAgentName(e.To) != nil {
			errs.add(p+"/to", "must be an agent name")
		}
		if len(e.Can) == 0 {
			errs.add(p+"/can", "must list message, and optionally wake")
		}
		for j, c := range e.Can {
			if !c.Valid() {
				errs.add(fmt.Sprintf("%s/can/%d", p, j), "must be message or wake")
			} else if slices.Index(e.Can, c) < j {
				errs.add(fmt.Sprintf("%s/can/%d", p, j), "lists %s twice", c)
			}
		}
		if slices.Contains(e.Can, model.GridCapabilityWake) && !slices.Contains(e.Can, model.GridCapabilityMessage) {
			errs.add(p+"/can", "wake requires message on the same edge")
		}
	}
}

// checkSpecRules applies checks 1–4 and 6 of Validating a spec.
func checkSpecRules(errs *fieldErrors, spec model.TeamSpec, facts SpecFacts, os model.HostOS) {
	cur := facts.Current
	spawned := cur.liveSpawned()
	spawnedNames := map[string]bool{}
	for _, m := range spawned {
		spawnedNames[m.Name] = true
	}

	// 1. Member ids and names are unique within the spec, and no spec member
	// takes a live spawned member's name (names are unique among live members).
	ids := map[string]int{}
	names := map[string]int{}
	for i, m := range spec.Members {
		p := fmt.Sprintf("/members/%d", i)
		if j, ok := ids[m.ID]; ok {
			errs.add(p+"/id", "repeats the id of /members/%d", j)
		} else {
			ids[m.ID] = i
		}
		if j, ok := names[m.Name]; ok {
			errs.add(p+"/name", "repeats the name of /members/%d", j)
		} else {
			names[m.Name] = i
		}
		if spawnedNames[m.Name] {
			errs.add(p+"/name", "is the name of a live spawned member of this team")
		}
	}

	// 2. No spec member id belongs to another team, or to a member of this
	// team that isn't a live spec member (a spawned member, human, or a spec
	// member removed earlier: ids are never reused).
	for i, m := range spec.Members {
		p := fmt.Sprintf("/members/%d/id", i)
		if slices.Contains(facts.ForeignMemberIDs, m.ID) {
			errs.add(p, "belongs to a member of another team")
			continue
		}
		if have := cur.member(m.ID); have != nil {
			switch {
			case have.Origin == model.MemberOriginSpawn:
				errs.add(p, "belongs to %s, a spawned member of this team", have.Name)
			case have.Kind == model.MemberKindHuman:
				errs.add(p, "belongs to this team's human")
			case have.RemovedAt != nil:
				errs.add(p, "belonged to %s, removed from this team; member ids are never reused", have.Name)
			}
		}
	}

	// 3. Grid names exist; no human; no self edge; one edge per pair.
	edges := map[gridKey]int{}
	for i, e := range spec.Grid {
		p := fmt.Sprintf("/grid/%d", i)
		for _, end := range []struct{ field, name string }{{"from", e.From}, {"to", e.To}} {
			switch {
			case end.name == HumanName:
				errs.add(p+"/"+end.field, "names human, whose row and column are implicit")
			case model.ValidateAgentName(end.name) != nil:
				// Already reported by the value rules.
			case !hasName(spec, end.name) && !spawnedNames[end.name]:
				errs.add(p+"/"+end.field, "names %s, which is neither a spec member nor a live spawned member", end.name)
			}
		}
		if e.From == e.To {
			errs.add(p, "is a self edge")
		}
		k := gridKey{e.From, e.To}
		if j, ok := edges[k]; ok {
			errs.add(p, "repeats the edge %s → %s of /grid/%d", e.From, e.To, j)
		} else {
			edges[k] = i
		}
	}

	// 4. folder is absolute in the host's syntax.
	for i, m := range spec.Members {
		if m.Folder != "" && !IsAbsPath(os, m.Folder) {
			errs.add(fmt.Sprintf("/members/%d/folder", i), "must be an absolute %s path", osLabel(os))
		}
	}

	// 6. Live agent members, spec plus spawned, number at most 24.
	if n := len(spec.Members) + len(spawned); n > MaxLiveAgents && len(spec.Members) <= MaxLiveAgents {
		errs.add("/members", "would make %d live agent members (%d in the spec, %d spawned); the most is %d",
			n, len(spec.Members), len(spawned), MaxLiveAgents)
	}
}

func hasName(spec model.TeamSpec, name string) bool {
	for _, m := range spec.Members {
		if m.Name == name {
			return true
		}
	}
	return false
}

func osLabel(os model.HostOS) string {
	if os == model.HostOSWindows {
		return "Windows"
	}
	return "POSIX"
}

// Warning is an apply-team warning: a problem that doesn't block the apply.
type Warning struct {
	Code model.WarningCode `json:"code"`
	// Member is the member's id, or nil for a team-wide warning.
	Member  *string `json:"member"`
	Message string  `json:"message"`
}

// specWarnings returns the warnings for a defaulted spec, member by member in
// spec order: folder_missing, folder_not_git, harness_unavailable. next_launch
// warnings come from the apply diff.
func specWarnings(spec model.TeamSpec, host Host) []Warning {
	var out []Warning
	for _, m := range spec.Members {
		id := m.ID
		if !host.FolderExists(m.Folder) {
			out = append(out, Warning{Code: model.WarningCodeFolderMissing, Member: &id,
				Message: fmt.Sprintf("%s's folder %s doesn't exist yet; its launch will fail if it still doesn't", m.Name, m.Folder)})
		} else if _, inGit := host.RepoRoot(m.Folder); !inGit {
			switch {
			case m.Worktree != nil && *m.Worktree:
				out = append(out, Warning{Code: model.WarningCodeFolderNotGit, Member: &id,
					Message: fmt.Sprintf("%s's folder %s is outside git, so it runs in the folder itself, not a worktree", m.Name, m.Folder)})
			case m.Spawn != nil && m.Spawn.Enabled && ResolveSpawn(m.Spawn).Folder == model.SpawnFolderWorktree:
				out = append(out, Warning{Code: model.WarningCodeFolderNotGit, Member: &id,
					Message: fmt.Sprintf("%s's folder %s is outside git, so its workers share it instead of getting worktrees", m.Name, m.Folder)})
			}
		}
		if issue := host.HarnessIssue(m.Harness); issue != "" {
			out = append(out, Warning{Code: model.WarningCodeHarnessUnavailable, Member: &id,
				Message: fmt.Sprintf("%s's harness %s isn't available on this host (%s)", m.Name, m.Harness, issue)})
		}
	}
	return out
}
