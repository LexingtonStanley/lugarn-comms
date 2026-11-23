package broker

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/LexingtonStanley/lugarn-comms/internal/hc"
	"github.com/LexingtonStanley/lugarn-comms/internal/herdr"
	"github.com/LexingtonStanley/lugarn-comms/internal/model"
	"github.com/LexingtonStanley/lugarn-comms/internal/store"
)

func resolvedChoice(c model.HerdrBackendChoice) *model.HerdrBackend {
	if c == model.HerdrBackendChoiceAuto {
		return nil
	}
	if c == "" {
		c = model.HerdrBackendChoiceHerdr
	}
	v := model.HerdrBackend(c)
	return &v
}
func hcTeam(t *store.Team) bool { return t.Backend != nil && *t.Backend == model.HerdrBackendHC }
func hcMember(m model.Member) bool {
	return m.Herdr != nil && m.Herdr.Backend != nil && *m.Herdr.Backend == model.HerdrBackendHC
}
func (i *Integration) hcClient(ctx context.Context, server string) (*hc.Client, error) {
	return hc.Connect(ctx, i.opts.HCBinary, server)
}

// hcExisting connects only to a core that is already running. Read-only
// operations (adopt-team, the members refresh) must not start one: connect
// starts a missing core, which would be a change on the host.
func (i *Integration) hcExisting(ctx context.Context, server string) (*hc.Client, error) {
	if i.opts.HCSocket != nil {
		if running := i.opts.HCSocket(server); running != nil && !*running {
			return nil, model.NewError(model.ErrorHerdrUnavailable, "the hc core is not running").WithDetail("reason", "not_running")
		}
	}
	return i.hcClient(ctx, server)
}
func (i *Integration) team(ctx context.Context, id string) (*store.Team, error) {
	var t *store.Team
	e := i.b.db.Read(ctx, func(tx *store.Tx) error { var e error; t, e = tx.GetTeam(id); return e })
	return t, e
}
func (i *Integration) resolveBackend(ctx context.Context, t *store.Team) (*model.HerdrBackend, *string, error) {
	choice := t.BackendChoice
	if choice == "" || choice == model.HerdrBackendChoiceHerdr {
		return ptr(model.HerdrBackendHerdr), nil, nil
	}
	if e := i.refreshPath(ctx); e != nil {
		return nil, nil, e
	}
	c, e := i.hcClient(ctx, t.HerdrServer)
	if e == nil {
		defer c.Close()
		return ptr(model.HerdrBackendHC), &c.Hello.Core.ID, nil
	}
	if choice == model.HerdrBackendChoiceAuto {
		{ // Herdr remains the fallback, including a broken core.
			status, err := i.opts.Herdr(t.HerdrServer).Status(ctx)
			if err == nil && status.Installed {
				return ptr(model.HerdrBackendHerdr), nil, nil
			}
		}
	}
	failure := model.NewError(model.ErrorHerdrUnavailable, "herdr-core is unavailable").WithDetail("reason", "not_installed")
	if errors.Is(e, hc.ErrGeneration) {
		failure.WithDetail("hc", "unsupported_generation")
	} else if _, err := hc.Resolve(i.opts.HCBinary); err == nil && choice != model.HerdrBackendChoiceAuto {
		failure.WithDetail("reason", "not_running")
	}
	return nil, nil, failure
}
func (i *Integration) hcLabels(team, role string) map[string]string {
	sum := sha256.Sum256([]byte(i.b.opts.StateDir))
	labels := map[string]string{"lugarn.home": hex.EncodeToString(sum[:8]), "lugarn.team": team}
	if role != "" {
		labels["lugarn.role"] = role
	}
	return labels
}
func hcID(prefix string) (string, error) {
	var b [16]byte
	_, e := rand.Read(b[:])
	return prefix + hex.EncodeToString(b[:]), e
}

var hcAddressName = regexp.MustCompile(`^t[0-9][0-9a-z]*$`)
var windowsReserved = regexp.MustCompile(`(?i)^(con|prn|aux|nul|com[1-9]|lpt[1-9])(?:\..*)?$`)

func hcFolder(label, name string) string {
	label = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 || strings.ContainsRune(`/\<>:"|?*`, r) {
			return '-'
		}
		return r
	}, label)
	label = strings.Trim(label, " .")
	if len(label) > 64 {
		label = label[:64]
		for !utf8.ValidString(label) {
			label = label[:len(label)-1]
		}
		label = strings.Trim(label, " .")
	}
	if label == "" || windowsReserved.MatchString(label) {
		return name
	}
	return label
}
func (i *Integration) hcRunnerFolder(t store.Team) (string, error) {
	label := t.Name
	if t.DisplayName != nil {
		label = *t.DisplayName
	}
	path := filepath.Join(i.b.opts.StateDir, "spaces", t.ID, hcFolder(label, t.Name))
	if e := os.MkdirAll(path, 0700); e != nil {
		return "", e
	}
	return path, nil
}
func (i *Integration) reserveHCName(tx *store.Tx, t *store.Team, m model.Member, terminals []hc.Terminal) (string, error) {
	current, e := tx.GetStoredMember(m.ID)
	if e != nil {
		return "", e
	}
	if current.HerdrAgent != nil {
		return *current.HerdrAgent, nil
	}
	names, e := tx.ReservedHCNames(t.HerdrServer)
	if e != nil {
		return "", e
	}
	for _, v := range terminals {
		if v.State == "live" && v.Name != nil {
			names[*v.Name] = true
		}
	}
	for n := 1; n <= len(names)+3; n++ {
		name := herdr.CandidateName(m.Name, n)
		if !names[name] && !hcAddressName.MatchString(name) {
			return name, nil
		}
	}
	return "", model.NewError(model.ErrorNameTaken, "no free hc terminal name").WithDetail("name", m.Name)
}
func (i *Integration) hcRecord(ctx context.Context, m model.Member) (store.HCTerminal, error) {
	var records []store.HCTerminal
	e := i.b.db.Read(ctx, func(tx *store.Tx) error { var e error; records, e = tx.HCTerminals(m.Team); return e })
	if e != nil {
		return store.HCTerminal{}, e
	}
	for _, r := range records {
		if r.Member == nil || *r.Member != m.ID {
			continue
		}
		if m.External != nil && r.External && r.Address == m.External.Pane {
			return r, nil
		}
		if m.Lease != nil && m.Lease.Launch != nil && r.Launch != nil && *r.Launch == *m.Lease.Launch {
			return r, nil
		}
	}
	return store.HCTerminal{}, model.NewError(model.ErrorNotFound, "hc terminal record missing")
}

// Every managed mutation calls this guard with the record read from the store.
// A pid change means a kept terminal has become somebody's replacement shell.
func (i *Integration) hcOwned(ctx context.Context, c *hc.Client, r store.HCTerminal, live bool) (hc.Terminal, error) {
	var zero hc.Terminal
	if r.External || r.Core == "" || r.Core != c.Hello.Core.ID || strings.Contains(r.Address, "/") {
		return zero, model.NewError(model.ErrorLeaseConflict, "hc core identity changed")
	}
	t, e := c.Get(ctx, r.Address)
	if e != nil {
		return t, e
	}
	labels := i.hcLabels(r.Team, "")
	if t.Address != r.Address || t.Labels["lugarn.home"] != labels["lugarn.home"] || t.Labels["lugarn.team"] != r.Team {
		return zero, model.NewError(model.ErrorLeaseConflict, "hc terminal ownership changed")
	}
	if t.State == "live" && (t.PID == nil || *t.PID != r.PID) {
		return zero, model.NewError(model.ErrorLeaseConflict, "hc terminal program changed")
	}
	if live && t.State != "live" {
		return t, model.NewError(model.ErrorNotFound, "hc terminal has finished")
	}
	return t, nil
}
func (i *Integration) hcClose(ctx context.Context, c *hc.Client, r store.HCTerminal) error {
	t, e := i.hcOwned(ctx, c, r, false)
	if hc.IsGone(e) {
		return nil
	}
	if e != nil {
		return e
	}
	if t.State == "live" {
		t, e = c.Kill(ctx, r.Address)
		if e != nil {
			return e
		}
		if t.State != "finished" {
			return fmt.Errorf("hc kill did not finish terminal")
		}
	}
	if _, e = i.hcOwned(ctx, c, r, false); hc.IsGone(e) {
		return nil
	}
	if e != nil {
		return e
	}
	return c.Remove(ctx, r.Address)
}
func (i *Integration) hcEnvironment(env map[string]string) map[string]string {
	out := map[string]string{}
	// The direct launch needs the real home and login environment, not the
	// clean-shell capsule used exclusively by the 0.9 driver.
	for _, k := range []string{"HOME", "USERPROFILE", "ZDOTDIR", "XDG_CONFIG_HOME"} {
		if v, ok := os.LookupEnv(k); ok {
			out[k] = v
		}
	}
	for k, v := range env {
		if !strings.HasPrefix(strings.ToUpper(k), "HC_") {
			out[k] = v
		}
	}
	out["LUGARN_COMMS_RESTORE_ENV"] = "0"
	out["PATH"] = completeSystemPath()
	out["LUGARN_COMMS_POINTER_HOME"] = i.opts.HomeDir
	return out
}
