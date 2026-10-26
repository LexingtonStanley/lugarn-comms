package broker

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/LexingtonStanley/lugarn-comms/internal/harness"
	"github.com/LexingtonStanley/lugarn-comms/internal/herdr"
	"github.com/LexingtonStanley/lugarn-comms/internal/hostprobe"
	"github.com/LexingtonStanley/lugarn-comms/internal/model"
)

// HerdrAPI is the integration's replaceable Herdr client. Production uses
// herdr.Client; tests use fakes. Every factory call names a specific server.
type HerdrAPI interface {
	Status(context.Context) (herdr.Status, error)
	CreateWorkspace(context.Context, string, string, map[string]string) (herdr.Workspace, error)
	Workspace(context.Context, string) (herdr.Workspace, error)
	OpenTab(context.Context, string, string, string, map[string]string) (herdr.Tab, error)
	CloseTab(context.Context, string) error
	Pane(context.Context, string) (herdr.Pane, error)
	ClosePane(context.Context, string) error
	RunCommand(context.Context, string, ...string) error
	StartAgent(context.Context, herdr.StartOptions) (herdr.Agent, error)
	Agent(context.Context, string) (herdr.Agent, error)
	Prompt(context.Context, string, string) (herdr.Agent, error)
	Wait(context.Context, string, []model.Activity, time.Duration) (herdr.Agent, error)
	ReadAgent(context.Context, string, int) (string, error)
}

// IntegrationOptions supplies absolute broker/Herdr paths and the explicit home
// for launch pointers. Models and ExtraEnv are trusted host configuration only.
// No credential files or copied configuration homes are ever accessed here.
type IntegrationOptions struct {
	Binary, HerdrBinary, HomeDir string
	Models                       map[model.Harness]string
	ExtraEnv                     map[string]string
	Herdr                        func(string) HerdrAPI
	StartServer                  func(context.Context, string, string, ...herdr.ServerOption) (herdr.StartResult, error)
}

// Integration binds the real drivers to the broker and runner. Constructing it
// performs no host commands; operations resolve their own team/server as needed.
type Integration struct {
	b       *Broker
	opts    IntegrationOptions
	adapter *harnessAdapter
	panes   *paneAdapter
}

// NewIntegration constructs adapters without launching any process or server.
func NewIntegration(b *Broker, opts IntegrationOptions) (*Integration, error) {
	if opts.Binary == "" {
		var e error
		opts.Binary, e = os.Executable()
		if e != nil {
			return nil, e
		}
	}
	if !filepath.IsAbs(opts.Binary) {
		return nil, invalid("broker binary must be absolute")
	}
	if opts.HomeDir != "" && !filepath.IsAbs(opts.HomeDir) {
		return nil, invalid("pointer home must be absolute")
	}
	if opts.Herdr == nil {
		opts.Herdr = func(server string) HerdrAPI { return herdr.New(opts.HerdrBinary, server) }
	}
	if opts.StartServer == nil {
		opts.StartServer = herdr.StartServer
	}
	integration := &Integration{b: b, opts: opts}
	integration.adapter = &harnessAdapter{binary: opts.Binary, state: b.opts.StateDir, home: opts.HomeDir, probe: b.opts.Probe, models: opts.Models, extra: opts.ExtraEnv}
	integration.panes = &paneAdapter{clients: opts.Herdr, observed: map[string]model.Activity{}, submitted: map[string]bool{}, checked: map[string]int64{}}
	return integration, nil
}

// Runner returns a runner using real headless/files/pane adapters. The caller
// supplies optional scheduling settings and owns Run's cancellation lifecycle.
func (i *Integration) Runner(team string, opts RunnerOptions) *Runner {
	opts.Headless = i.adapter
	opts.Files = i.adapter
	opts.Panes = i.panes
	opts.HomeDir = i.opts.HomeDir
	if opts.Processes == nil {
		opts.Processes = integrationProcesses{}
	}
	r := NewRunner(i.b, team, opts)
	r.onStop = cleanupAdoptedDescendants
	lifecycle := &integrationLifecycle{i: i, r: r}
	r.opts.PaneLifecycle = lifecycle
	r.opts.Requests = lifecycle
	r.checkPane = lifecycle.checkPane
	r.prepareMember = func(ctx context.Context, m model.Member) (*model.Member, error) { return i.prepareWorktree(ctx, r, m) }
	r.onAcquire = func(ctx context.Context) error {
		placement, e := i.placement(team)
		if e != nil {
			return e
		}
		if placement.Pane == "" {
			return nil
		}
		state, e := (nativeProcesses{}).Inspect(ctx, r.opts.PID)
		if e != nil {
			return e
		}
		placement.PID = r.opts.PID
		placement.Birth = state.Birth
		return i.savePlacement(team, placement)
	}
	return r
}

type harnessAdapter struct {
	binary, state, home string
	probe               hostprobe.Probe
	models              map[model.Harness]string
	extra               map[string]string
}
type fixedHarnessBinary string

func (b fixedHarnessBinary) Binary(model.Harness) (string, error) { return string(b), nil }
func (a *harnessAdapter) launch(ctx context.Context, d TurnLaunch) (harness.Launch, error) {
	l := harness.Launch{Pane: d.Mode == model.ModePane, PointerHome: a.home, Harness: d.Harness, Profile: d.Profile, WorkDir: d.Workdir, StateDir: d.StateDir, LaunchID: d.LaunchID, MemberID: d.MemberID, Binary: a.binary, Model: a.models[d.Harness], ExtraEnv: map[string]string{"LUGARN_COMMS_HOME": a.state}}
	for k, v := range a.extra {
		l.ExtraEnv[k] = v
	}
	if d.Session != nil {
		l.SessionID = *d.Session
		l.Resume = !d.NewSession
	}
	if d.Harness == model.HarnessOpenCode {
		for _, info := range a.probe.Harnesses(ctx) {
			if info.Harness == d.Harness && info.Version != nil {
				l.Version = *info.Version
				break
			}
		}
	}
	return l, l.Validate()
}
func (a *harnessAdapter) Write(ctx context.Context, d TurnLaunch) error {
	l, e := a.launch(ctx, d)
	if e != nil {
		return e
	}
	return harness.WriteFiles(l)
}
func (a *harnessAdapter) Delete(_ context.Context, id string) error {
	if model.ValidateULID(id) != nil {
		return invalid("invalid launch id")
	}
	for _, suffix := range []string{".json", ".settings.json", ".opencode.json"} {
		if e := os.Remove(filepath.Join(a.state, "launch", id+suffix)); e != nil && !os.IsNotExist(e) {
			return e
		}
	}
	return nil
}
func (a *harnessAdapter) Start(ctx context.Context, d TurnLaunch, prompt string) (TurnHandle, error) {
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	l, e := a.launch(ctx, d)
	if e != nil {
		return nil, e
	}
	// The harness driver treats ctx as process lifetime. The runner's startup
	// context must not kill a successfully started turn when it is released.
	protect := startProtection()
	h, e := (harness.Driver{Resolver: fixedHarnessBinary(d.Binary)}).Start(context.WithoutCancel(ctx), l, prompt)
	if e != nil {
		protect(0)
		return nil, e
	}
	protect(h.PID)
	adapter := &harnessHandle{handle: h, acks: make(chan TurnAcknowledgement, 1)}
	go func() {
		defer close(adapter.acks)
		for event := range h.Events {
			var session *string
			if event.SessionID != "" {
				session = ptr(event.SessionID)
			}
			adapter.acks <- TurnAcknowledgement{Session: session}
		}
	}()
	return adapter, nil
}

type harnessHandle struct {
	handle *harness.Handle
	acks   chan TurnAcknowledgement
}

func (h *harnessHandle) JobName() string                              { return h.handle.JobName }
func (h *harnessHandle) PID() int64                                   { return int64(h.handle.PID) }
func (h *harnessHandle) Acknowledgements() <-chan TurnAcknowledgement { return h.acks }
func (h *harnessHandle) Cancel() error                                { h.handle.Cancel(); return nil }
func (h *harnessHandle) Wait() TurnOutcome {
	out := h.handle.Wait()
	releaseProtection(h.handle.PID)
	var session *string
	if out.SessionID != "" {
		session = &out.SessionID
	}
	return TurnOutcome{ExitCode: ptr(int64(out.ExitCode)), Acknowledged: out.Acknowledged, Session: session, Text: out.FinalText, Usage: out.Usage, Error: out.Error, Cancelled: out.Cancelled}
}

type paneAdapter struct {
	clients   func(string) HerdrAPI
	mu        sync.Mutex
	observed  map[string]model.Activity
	submitted map[string]bool
	checked   map[string]int64
}

func paneIdentity(m model.Member) (string, string) {
	server := model.DefaultHerdrServer
	agent := herdr.AgentName(m.Team, m.Name)
	if m.Herdr != nil {
		server = m.Herdr.Server
		if m.Herdr.Agent != nil {
			agent = *m.Herdr.Agent
		}
	}
	return server, agent
}
func paneKey(m model.Member) string { server, agent := paneIdentity(m); return server + "/" + agent }
func (p *paneAdapter) State(ctx context.Context, m model.Member) (PaneObservation, error) {
	server, name := paneIdentity(m)
	a, e := p.clients(server).Agent(ctx, name)
	if herdr.IsTimeout(e) {
		return PaneObservation{Activity: model.ActivityUnknown}, nil
	}
	if herdr.IsGone(e) || herdr.IsUnavailable(e) {
		return PaneObservation{Gone: true}, nil
	}
	if e != nil {
		return PaneObservation{}, e
	}
	p.mu.Lock()
	p.observed[paneKey(m)] = a.Activity
	p.mu.Unlock()
	return PaneObservation{Activity: a.Activity}, nil
}
func (p *paneAdapter) Submit(ctx context.Context, m model.Member, prompt string) error {
	server, name := paneIdentity(m)
	client := p.clients(server)
	before, e := client.Agent(ctx, name)
	if e != nil {
		return e
	}
	if before.Activity != model.ActivityIdle && before.Activity != model.ActivityDone {
		return model.NewError(model.ErrorLaunchFailed, "pane is no longer ready for submission")
	}
	after, e := client.Prompt(ctx, name, prompt)
	if e != nil {
		return e
	}
	p.mu.Lock()
	p.observed[paneKey(m)] = before.Activity
	p.submitted[paneKey(m)] = after.Activity != before.Activity
	p.mu.Unlock()
	return nil
}
func (p *paneAdapter) WaitSettled(ctx context.Context, m model.Member, timeout time.Duration) (PaneObservation, error) {
	server, name := paneIdentity(m)
	a, e := p.clients(server).Wait(ctx, name, []model.Activity{model.ActivityIdle, model.ActivityDone, model.ActivityBlocked, model.ActivityUnknown}, timeout)
	if herdr.IsTimeout(e) {
		return PaneObservation{Activity: model.ActivityWorking}, nil
	}
	if herdr.IsGone(e) || herdr.IsUnavailable(e) {
		return PaneObservation{Gone: true}, nil
	}
	if e != nil {
		return PaneObservation{}, e
	}
	p.mu.Lock()
	p.observed[paneKey(m)] = a.Activity
	p.mu.Unlock()
	return PaneObservation{Activity: a.Activity}, nil
}
func (p *paneAdapter) WaitActivity(ctx context.Context, m model.Member, timeout time.Duration) (bool, error) {
	p.mu.Lock()
	baseline := p.observed[paneKey(m)]
	confirmed := p.submitted[paneKey(m)]
	p.mu.Unlock()
	if confirmed {
		return true, nil
	}
	states := []model.Activity{}
	for _, s := range []model.Activity{model.ActivityIdle, model.ActivityWorking, model.ActivityDone, model.ActivityBlocked} {
		if s != baseline {
			states = append(states, s)
		}
	}
	server, name := paneIdentity(m)
	_, e := p.clients(server).Wait(ctx, name, states, timeout)
	if herdr.IsTimeout(e) || herdr.IsGone(e) || herdr.IsUnavailable(e) {
		return false, nil
	}
	return e == nil, e
}

type integrationProcesses struct{ nativeProcesses }

func (integrationProcesses) Cancel(ctx context.Context, pid int64, birth string) error {
	return cancelOwnedTree(ctx, pid, birth)
}
