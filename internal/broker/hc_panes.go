package broker

import (
	"context"
	"errors"
	"github.com/LexingtonStanley/lugarn-comms/internal/hc"
	"github.com/LexingtonStanley/lugarn-comms/internal/model"
	"sync"
	"time"
)

type hcPoll struct {
	at        time.Time
	core      string
	terminals map[string]hc.Terminal
}
type hcPaneAdapter struct {
	i         *Integration
	mu        sync.Mutex
	pollMu    sync.Mutex
	polls     map[string]hcPoll
	views     map[string]*hc.View
	submitted map[string]bool
}

func (p *hcPaneAdapter) start(ctx context.Context, team string) error {
	t, e := p.i.team(ctx, team)
	if e != nil || !hcTeam(t) {
		return e
	}
	view := hc.NewView()
	p.mu.Lock()
	p.views[team] = view
	p.mu.Unlock()
	go view.Run(ctx, p.i.opts.HCBinary, t.HerdrServer, p.i.hcLabels(team, ""))
	return nil
}
func (p *hcPaneAdapter) State(ctx context.Context, m model.Member) (PaneObservation, error) {
	r, e := p.i.hcRecord(ctx, m)
	if e != nil {
		if missing(e) {
			return PaneObservation{Gone: true}, nil
		}
		return PaneObservation{}, e
	}
	t, e := p.i.team(ctx, m.Team)
	if e != nil {
		return PaneObservation{}, e
	}
	p.mu.Lock()
	view := p.views[m.Team]
	p.mu.Unlock()
	var terminals map[string]hc.Terminal
	connected := false
	if view != nil {
		terminals, connected = view.Current(r.Core)
	}
	if _, ok := terminals[r.Address]; !connected || !ok {
		var core string
		terminals, core, e = p.poll(ctx, m.Team, t.HerdrServer)
		if e != nil {
			return PaneObservation{}, e
		}
		if core != r.Core {
			return PaneObservation{Gone: true}, nil
		}
	}

	v, ok := terminals[r.Address]
	labels := p.i.hcLabels(m.Team, "")
	if !ok || v.State != "live" || v.PID == nil || *v.PID != r.PID || v.Labels["lugarn.home"] != labels["lugarn.home"] || v.Labels["lugarn.team"] != r.Team {
		return PaneObservation{Gone: true}, nil
	}
	if m.Harness != nil && *m.Harness == model.HarnessClaude && m.Session != nil && v.Activity() != model.ActivityUnknown && v.Activity() != model.ActivityBlocked {
		if e = p.i.b.rememberOrigin(m, *m.Session); e != nil {
			return PaneObservation{}, e
		}
	}
	return PaneObservation{Activity: v.Activity()}, nil
}
func (p *hcPaneAdapter) Submit(ctx context.Context, m model.Member, prompt string) error {
	r, e := p.i.hcRecord(ctx, m)
	if e != nil {
		return e
	}
	t, e := p.i.team(ctx, m.Team)
	if e != nil {
		return e
	}
	c, e := p.i.hcClient(ctx, t.HerdrServer)
	if e != nil {
		return e
	}
	defer c.Close()
	before, e := p.i.hcOwned(ctx, c, r, true)
	if e != nil {
		return e
	}
	if before.Activity() == model.ActivityBlocked {
		return hcBlocked()
	}
	if before.Activity() != model.ActivityIdle && before.Activity() != model.ActivityDone {
		return model.NewError(model.ErrorLaunchFailed, "hc terminal is no longer ready")
	}
	result, e := c.Send(ctx, r.Address, prompt)
	if hc.IsKind(e, "blocked") {
		return hcBlocked()
	}
	if e != nil {
		return e
	}
	p.mu.Lock()
	p.submitted[m.ID] = result.Started != nil && *result.Started || result.Turn > before.Turn || result.Status != nil && *result.Status != "idle" && *result.Status != "done"
	p.mu.Unlock()
	return nil
}
func hcBlocked() error {
	return model.NewError(model.ErrorLaunchFailed, "hc recipient is blocked").WithDetail("reason", "recipient_blocked")
}
func (p *hcPaneAdapter) WaitSettled(ctx context.Context, m model.Member, timeout time.Duration) (PaneObservation, error) {
	t, e := p.i.team(ctx, m.Team)
	if e != nil {
		return PaneObservation{}, e
	}
	r, e := p.i.hcRecord(ctx, m)
	if e != nil {
		return PaneObservation{}, e
	}
	c, e := p.i.hcClient(ctx, t.HerdrServer)
	if e != nil {
		return PaneObservation{}, e
	}
	defer c.Close()
	if _, e = p.i.hcOwned(ctx, c, r, true); e != nil {
		if hc.IsGone(e) || missing(e) {
			return PaneObservation{Gone: true}, nil
		}
		return PaneObservation{}, e
	}
	result, e := c.Wait(ctx, r.Address, timeout)
	if e != nil {
		return PaneObservation{}, e
	}
	if result.Reason == "timeout" {
		return PaneObservation{Activity: model.ActivityWorking}, nil
	}
	// Wait's result can precede the notification stream. Read its fresh record.
	v, e := c.Get(ctx, r.Address)
	if hc.IsGone(e) {
		return PaneObservation{Gone: true}, nil
	}
	return PaneObservation{Activity: v.Activity(), Gone: v.State != "live"}, e
}
func (p *hcPaneAdapter) WaitActivity(ctx context.Context, m model.Member, timeout time.Duration) (bool, error) {
	p.mu.Lock()
	submitted := p.submitted[m.ID]
	p.mu.Unlock()
	if submitted {
		return true, nil
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		observation, e := p.State(ctx, m)
		if e != nil {
			return false, e
		}
		if observation.Gone {
			return false, nil
		}
		if observation.Activity == model.ActivityWorking || observation.Activity == model.ActivityBlocked {
			return true, nil
		}
		if e = p.i.b.opts.Wait(ctx, 50*time.Millisecond); e != nil {
			return false, e
		}
	}
	return false, nil
}
func (i *Integration) readHC(ctx context.Context, m model.Member, lines int) (string, error) {
	t, e := i.team(ctx, m.Team)
	if e != nil {
		return "", e
	}
	r, e := i.hcRecord(ctx, m)
	if e != nil {
		return "", e
	}
	c, e := i.hcClient(ctx, t.HerdrServer)
	if e != nil {
		return "", e
	}
	defer c.Close()
	if _, e = i.hcOwned(ctx, c, r, true); e != nil {
		return "", e
	}
	return c.Read(ctx, r.Address, lines)
}
func (i *Integration) waitHCIdle(ctx context.Context, m model.Member, timeout time.Duration) error {
	wait, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	for {
		observation, e := i.hcPanes.WaitSettled(wait, m, timeout)
		if errors.Is(e, context.DeadlineExceeded) {
			return model.NewError(model.ErrorTimeout, "hc terminal did not settle")
		}
		if e != nil {
			return e
		}
		if observation.Gone {
			return model.NewError(model.ErrorNotFound, "hc terminal has gone")
		}
		if observation.Activity == model.ActivityIdle || observation.Activity == model.ActivityDone {
			return nil
		}
		if observation.Activity == model.ActivityBlocked {
			return hcBlocked()
		}
		if e = i.b.opts.Wait(wait, 50*time.Millisecond); e != nil {
			return model.NewError(model.ErrorTimeout, "hc terminal did not settle")
		}
	}
}

// One list serves all members observed in a runner pass during a disconnect.
func (p *hcPaneAdapter) poll(ctx context.Context, team, server string) (map[string]hc.Terminal, string, error) {
	p.pollMu.Lock()
	defer p.pollMu.Unlock()
	if cached, ok := p.polls[team]; ok && time.Since(cached.at) < 200*time.Millisecond {
		return cached.terminals, cached.core, nil
	}
	c, e := p.i.hcClient(ctx, server)
	if e != nil {
		return nil, "", e
	}
	defer c.Close()
	all, e := c.List(ctx, p.i.hcLabels(team, ""))
	if e != nil {
		return nil, "", e
	}
	byAddress := map[string]hc.Terminal{}
	for _, v := range all {
		byAddress[v.Address] = v
	}
	if p.polls == nil {
		p.polls = map[string]hcPoll{}
	}
	p.polls[team] = hcPoll{at: time.Now(), core: c.Hello.Core.ID, terminals: byAddress}
	return byAddress, c.Hello.Core.ID, nil
}
