package broker

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/LexingtonStanley/lugarn-comms/internal/harness"
	"github.com/LexingtonStanley/lugarn-comms/internal/hc"
	"github.com/LexingtonStanley/lugarn-comms/internal/model"
	"github.com/LexingtonStanley/lugarn-comms/internal/policy"
	"github.com/LexingtonStanley/lugarn-comms/internal/store"
	"github.com/LexingtonStanley/lugarn-comms/internal/wire"
)

var hcSpacePattern = regexp.MustCompile(`^sp[0-9a-f]{32}$`)
var hcTabPattern = regexp.MustCompile(`^tb[0-9a-f]{32}$`)
var hcTerminalPattern = regexp.MustCompile(`^t[0-9a-z]+$`)

func hcHarness(t hc.Terminal) model.Harness {
	name := ""
	if t.Status != nil {
		name = t.Status.App
	}
	if name == "" && t.Foreground != nil {
		name = t.Foreground.Name
	}
	name = strings.TrimSuffix(strings.ToLower(filepath.Base(name)), ".exe")
	if name == "claude-code" {
		name = "claude"
	}
	return model.Harness(name)
}

// Session discovery reads only the permitted per-process Claude hint. It never
// consults credentials, global config, or arbitrary paths reported by a core.
func hcSession(home string, t hc.Terminal, h model.Harness) *string {
	f := t.Foreground
	if f == nil {
		return nil
	}
	if h == model.HarnessClaude && f.PID > 0 {
		path := filepath.Join(home, ".claude", "sessions", fmt.Sprintf("%d.json", f.PID))
		if info, e := os.Lstat(path); e == nil && info.Mode().IsRegular() && info.Size() <= 1<<20 {
			raw, e := os.ReadFile(path)
			if e == nil {
				var data struct {
					PID       int64  `json:"pid"`
					Cwd       string `json:"cwd"`
					SessionID string `json:"sessionId"`
				}
				if json.Unmarshal(raw, &data) == nil && data.PID == f.PID && data.Cwd == f.Cwd && harness.ValidSession(h, data.SessionID) {
					return &data.SessionID
				}
			}
		}
	}
	for n, arg := range f.Argv {
		if n+1 == len(f.Argv) {
			break
		}
		next := f.Argv[n+1]
		match := h == model.HarnessClaude && (arg == "--resume" || arg == "-r") || h == model.HarnessCodex && arg == "resume" || h == model.HarnessOpenCode && (arg == "--session" || arg == "-s")
		if match && harness.ValidSession(h, next) {
			return &next
		}
	}
	return nil
}
func (i *Integration) validateHCExternal(ctx context.Context, space, address, name string, h model.Harness, folder string, t hc.Terminal) (model.ExternalMember, error) {
	var out model.ExternalMember
	if !hcSpacePattern.MatchString(space) || !hcTerminalPattern.MatchString(address) || !hcTabPattern.MatchString(t.Labels["herdr.tab"]) {
		return out, invalid("invalid hc identity")
	}
	actual := t.Address
	if t.Name != nil {
		actual = *t.Name
	}
	if t.Address != address || actual != name || t.State != "live" || t.Labels["herdr.space"] != space || t.Foreground == nil || t.PID == nil {
		return out, model.NewError(model.ErrorLeaseConflict, "hc terminal identity changed")
	}
	if hcHarness(t) != h || t.Foreground.Cwd != folder {
		return out, model.NewError(model.ErrorLeaseConflict, "hc harness or folder changed")
	}
	if !utf8.ValidString(folder) || strings.ContainsRune(folder, 0) || utf8.RuneCountInString(folder) > 4096 || !policy.IsAbsPath(i.b.host(ctx).OS(), folder) {
		return out, invalid("invalid hc folder")
	}
	home := i.opts.HomeDir
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	return model.ExternalMember{Workspace: space, Pane: address, Terminal: address, Agent: name, Session: hcSession(home, t, h), Activity: t.Activity(), Available: true}, nil
}
func (b *Broker) adoptHC(ctx context.Context, r wire.AdoptTeamRequest) (wire.ApplyTeamResult, error) {
	var zero wire.ApplyTeamResult
	if b.integration == nil {
		return zero, model.NewError(model.ErrorHerdrUnavailable, "hc integration unavailable")
	}
	if len(r.Members) != len(r.Spec.Members) || len(r.Members) == 0 {
		return zero, invalid("adoption must bind every member")
	}
	if e := policy.ValidateSpec(r.Spec, policy.SpecFacts{}, b.host(ctx)); e != nil {
		return zero, e
	}
	spec := policy.ApplyDefaults(r.Spec)
	c, e := b.integration.hcExisting(ctx, *spec.Herdr.Server)
	if e != nil {
		return zero, e
	}
	defer c.Close()
	terminals, e := c.List(ctx, nil)
	if e != nil {
		return zero, e
	}
	all := map[string]hc.Terminal{}
	for _, t := range terminals {
		all[t.Address] = t
	}
	members := map[string]model.MemberSpec{}
	for _, m := range spec.Members {
		members[m.ID] = m
	}
	bindings := map[string]model.ExternalMember{}
	seen := map[string]bool{}
	records := map[string]store.HCTerminal{}
	for _, binding := range r.Members {
		m, ok := members[binding.Member]
		if !ok || seen[binding.Pane] || bindings[binding.Member].Pane != "" {
			return zero, invalid("duplicate or unknown adoption binding")
		}
		if *m.Worktree || m.Mode != model.ModePane {
			return zero, invalid("adoption requires pane mode without worktrees")
		}
		terminal := all[binding.Pane]
		v, e := b.integration.validateHCExternal(ctx, r.Workspace, binding.Pane, binding.Agent, m.Harness, m.Folder, terminal)
		if e != nil {
			return zero, e
		}
		bindings[m.ID] = v
		seen[binding.Pane] = true
		records[m.ID] = store.HCTerminal{Team: spec.ID, Core: c.Hello.Core.ID, Address: terminal.Address, Member: ptr(m.ID), PID: *terminal.PID, ForegroundPID: terminal.Foreground.PID, Space: r.Workspace, Tab: terminal.Labels["herdr.tab"], External: true}
	}
	return b.applyTeam(ctx, wire.ApplyTeamRequest{Spec: spec, IfRevision: 0}, func(tx *store.Tx) error {
		for id, v := range bindings {
			used, e := tx.HCPaneTracked(c.Hello.Core.ID, v.Pane)
			if e != nil {
				return e
			}
			if used {
				return model.NewError(model.ErrorLeaseConflict, "hc terminal is already tracked")
			}
			var name *string
			if all[v.Pane].Name != nil {
				name = all[v.Pane].Name
				names, e := tx.ReservedHCNames(*spec.Herdr.Server)
				if e != nil {
					return e
				}
				if names[*name] {
					return model.NewError(model.ErrorNameTaken, "hc terminal name is reserved").WithDetail("name", *name)
				}
			}
			if e = tx.PutExternalMember(id, v); e != nil {
				return e
			}
			if e = tx.PutHCTerminal(records[id]); e != nil {
				return e
			}
			if e = tx.UpdateMember(id, store.MemberPatch{HerdrAgent: &name, HerdrWorkspace: ptr(&v.Workspace)}, b.now()); e != nil {
				return e
			}
		}
		return tx.UpdateTeam(spec.ID, store.TeamPatch{HerdrWorkspace: ptr(&r.Workspace), Adopted: ptr(true), HCCoreID: ptr(&c.Hello.Core.ID)}, b.now())
	})
}
func (i *Integration) externalHC(ctx context.Context, t *store.Team, m model.Member) (model.ExternalMember, error) {
	old := *m.External
	r, e := i.hcRecord(ctx, m)
	if e != nil {
		return old, e
	}
	c, e := i.hcClient(ctx, t.HerdrServer)
	if e != nil {
		return old, e
	}
	defer c.Close()
	if c.Hello.Core.ID != r.Core {
		return old, model.NewError(model.ErrorLeaseConflict, "hc core changed")
	}
	terminal, e := c.Get(ctx, r.Address)
	if hc.IsGone(e) || e == nil && terminal.State == "finished" {
		var restarting bool
		err := i.b.db.Read(ctx, func(tx *store.Tx) error { var e error; restarting, e = tx.ExternalRestarting(m.ID); return e })
		if err != nil {
			return old, err
		}
		if restarting {
			old.Available = false
			return old, nil
		}
	}
	if e != nil {
		return old, e
	}
	if terminal.PID == nil || *terminal.PID != r.PID || terminal.Foreground == nil || terminal.Foreground.PID != r.ForegroundPID {
		return old, model.NewError(model.ErrorLeaseConflict, "hc external program changed")
	}
	return i.validateHCExternal(ctx, old.Workspace, old.Pane, old.Agent, *m.Harness, *m.Folder, terminal)
}
func (b *Broker) refreshHCExternal(ctx context.Context, t *store.Team, ms []model.Member) error {
	needed := false
	for _, m := range ms {
		needed = needed || m.External != nil
	}
	if !needed {
		return nil
	}
	c, e := b.integration.hcExisting(ctx, t.HerdrServer)
	var all []hc.Terminal
	var core string
	if e == nil {
		defer c.Close()
		core = c.Hello.Core.ID
		all, e = c.List(ctx, nil)
	}
	byAddress := map[string]hc.Terminal{}
	for _, v := range all {
		byAddress[v.Address] = v
	}
	for _, m := range ms {
		if m.External == nil {
			continue
		}
		old := *m.External
		v := old
		valid := false
		if e == nil {
			r, err := b.integration.hcRecord(ctx, m)
			terminal := byAddress[old.Pane]
			if err == nil && r.Core == core && terminal.PID != nil && *terminal.PID == r.PID && terminal.Foreground != nil && terminal.Foreground.PID == r.ForegroundPID {
				v, err = b.integration.validateHCExternal(ctx, old.Workspace, old.Pane, old.Agent, *m.Harness, *m.Folder, terminal)
				valid = err == nil
			}
		}
		if !valid {
			v = old
			v.Available = false
			v.Activity = model.ActivityUnknown
		}
		if reflect.DeepEqual(old, v) {
			continue
		}
		if err := b.db.Write(ctx, func(tx *store.Tx) error {
			current, err := tx.GetMember(m.ID)
			if err != nil {
				return err
			}
			if !reflect.DeepEqual(current.External, m.External) {
				return nil
			}
			return tx.PutExternalMember(m.ID, v)
		}); err != nil {
			return err
		}
	}
	return nil
}

// This is the sole exception to managed labels: explicit conversion rechecks the
// complete external binding immediately before kill and never removes it.
func (i *Integration) killHCExternal(ctx context.Context, t *store.Team, m model.Member, expected model.ExternalMember) error {
	v, e := i.externalHC(ctx, t, m)
	if e != nil {
		return e
	}
	if !reflect.DeepEqual(v.Session, expected.Session) || v.Pane != expected.Pane || v.Terminal != expected.Terminal {
		return model.NewError(model.ErrorLeaseConflict, "hc external conversation changed")
	}
	if !v.Available {
		return nil
	}
	r, e := i.hcRecord(ctx, m)
	if e != nil {
		return e
	}
	c, e := i.hcClient(ctx, t.HerdrServer)
	if e != nil {
		return e
	}
	defer c.Close()
	// Recheck on the connection used for the mutation, including the foreground pid.
	terminal, e := c.Get(ctx, r.Address)
	if e != nil {
		return e
	}
	if c.Hello.Core.ID != r.Core || terminal.PID == nil || *terminal.PID != r.PID || terminal.Foreground == nil || terminal.Foreground.PID != r.ForegroundPID {
		return model.NewError(model.ErrorLeaseConflict, "hc external identity changed")
	}
	now, e := i.validateHCExternal(ctx, v.Workspace, v.Pane, v.Agent, *m.Harness, *m.Folder, terminal)
	if e != nil {
		return e
	}
	if !reflect.DeepEqual(now.Session, v.Session) {
		return model.NewError(model.ErrorLeaseConflict, "hc external session changed")
	}
	killed, e := c.Kill(ctx, r.Address)
	if e != nil {
		return e
	}
	if killed.State != "finished" {
		return model.NewError(model.ErrorLaunchFailed, "hc external terminal did not finish")
	}
	return nil
}
