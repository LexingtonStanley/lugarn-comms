package broker

import (
	"context"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/LexingtonStanley/lugarn-comms/internal/hostprobe"
	"github.com/LexingtonStanley/lugarn-comms/internal/model"
	"github.com/LexingtonStanley/lugarn-comms/internal/service"
	"github.com/LexingtonStanley/lugarn-comms/internal/store"
	"github.com/LexingtonStanley/lugarn-comms/internal/wire"
)

type fakeHost struct{}

func (fakeHost) OS() model.HostOS                              { return model.HostOS(runtime.GOOS) }
func (fakeHost) FolderExists(string) bool                      { return true }
func (fakeHost) RepoRoot(string) (string, bool)                { return "", false }
func (fakeHost) HarnessIssue(model.Harness) model.HarnessIssue { return "" }
func (fakeHost) CanRunHeadless(h model.Harness) bool           { return h != model.HarnessPi }
func (fakeHost) PathExists(string) bool                        { return false }
func (fakeHost) BranchExists(string, string) bool              { return false }

type fakeProbe struct{}

func (fakeProbe) Host() (hostprobe.HostInfo, error) {
	return hostprobe.HostInfo{OS: model.HostOS(runtime.GOOS), Arch: model.HostArch(runtime.GOARCH), Hostname: "test"}, nil
}
func (fakeProbe) Harnesses(context.Context) []model.HarnessInfo {
	return []model.HarnessInfo{{Harness: model.HarnessCodex, Installed: true, Supported: true, Headless: true}}
}
func (fakeProbe) Herdr(_ context.Context, s string) (model.HerdrInfo, error) {
	return model.HerdrInfo{Server: s, Installed: true, Running: true}, nil
}
func (fakeProbe) Logind(context.Context) *hostprobe.LogindInfo {
	return &hostprobe.LogindInfo{Linger: true}
}
func (fakeProbe) CanRunHeadless(h model.Harness) bool  { return h != model.HarnessPi }
func (fakeProbe) Binary(model.Harness) (string, error) { return "fake", nil }
func setup(t *testing.T) (*Broker, service.Caller) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("LUGARN_COMMS_HOME", dir)
	db, e := store.Open(context.Background(), filepath.Join(dir, "comms.db"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { db.Close() })
	b := New(db, Options{StateDir: dir, Host: fakeHost{}, Probe: fakeProbe{}})
	e = db.Write(context.Background(), func(tx *store.Tx) error {
		now := b.now()
		if e := tx.CreateTeam(store.Team{ID: "t", Name: "test", Pattern: model.PatternWriterReviewer, State: model.TeamStateRunning, Run: 1, Runner: model.Runner{State: model.RunnerStateUp, HeartbeatAt: &now}}); e != nil {
			return e
		}
		for i, name := range []string{"writer", "reviewer", "human"} {
			p := store.MemberParams{ID: name, TeamID: "t", Name: name, Kind: model.MemberKindAgent, Origin: model.MemberOriginSpec, State: model.MemberStateRunning, Role: ptr(model.RoleWriter), Harness: ptr(model.HarnessCodex), Profile: ptr(model.ProfileWorkspace), Mode: ptr(model.ModeHeadless), Folder: &dir, Brief: ptr("brief"), Position: ptr(int64(i))}
			if name == "reviewer" {
				p.Role = ptr(model.RoleReviewer)
			}
			if name == "human" {
				p.Kind = model.MemberKindHuman
				p.Origin = model.MemberOriginImplicit
				p.Role = nil
				p.Harness = nil
				p.Profile = nil
				p.Mode = nil
				p.Folder = nil
				p.Brief = nil
			}
			if e := tx.InsertMember(p); e != nil {
				return e
			}
			if name != "human" {
				if e := tx.InsertLease(name, model.Lease{Driver: model.DriverNone}, now); e != nil {
					return e
				}
			}
		}
		return tx.ReplaceGrid("t", []store.GridEdge{{TeamID: "t", FromID: "writer", ToID: "reviewer", Can: []model.GridCapability{model.GridCapabilityMessage, model.GridCapabilityWake}}, {TeamID: "t", FromID: "reviewer", ToID: "writer", Can: []model.GridCapability{model.GridCapabilityMessage}}})
	})
	if e != nil {
		t.Fatal(e)
	}
	return b, service.Caller{MemberID: "writer", TeamID: "t"}
}
func code(t *testing.T, e error, w model.ErrorCode) {
	t.Helper()
	if e == nil || model.AsError(e).Code != w {
		t.Fatalf("error = %v, want %s", e, w)
	}
}
func must(t *testing.T, e error) {
	t.Helper()
	if e != nil {
		t.Fatal(e)
	}
}
func launch(t *testing.T, b *Broker, member string) (string, string) {
	t.Helper()
	id, e := model.NewULID()
	must(t, e)
	token, e := model.NewToken()
	must(t, e)
	must(t, b.db.Write(context.Background(), func(tx *store.Tx) error {
		if e := tx.InsertLaunch(store.Launch{ID: id, MemberID: member, Generation: 1, Driver: model.ModeHeadless, StartedAt: b.now()}); e != nil {
			return e
		}
		return tx.InsertToken(store.Token{LaunchID: id, MemberID: member, Hash: model.HashToken(token), CreatedAt: b.now()})
	}))
	return id, token
}
func TestAuthenticate(t *testing.T) {
	b, _ := setup(t)
	ctx := context.Background()
	id, token := launch(t, b, "writer")
	tok := &service.Token{Bytes: []byte(token), Hash: model.HashToken(token)}
	c, e := b.Authenticate(ctx, "writer", tok, model.FaceCLI)
	must(t, e)
	if c.LaunchID != id {
		t.Fatal(c)
	}
	for _, v := range []struct {
		id      string
		tok     *service.Token
		failure model.AuthFailure
	}{{"writer", nil, model.AuthFailureNoToken}, {"ghost", tok, model.AuthFailureBadToken}, {"reviewer", tok, model.AuthFailureBadToken}, {"", tok, model.AuthFailureNoToken}} {
		_, e := b.Authenticate(ctx, v.id, v.tok, model.FaceMCP)
		code(t, e, model.ErrorUnauthenticated)
	}
	must(t, b.db.Write(ctx, func(tx *store.Tx) error { return tx.RevokeToken(id, b.now()) }))
	_, e = b.Authenticate(ctx, "writer", tok, model.FaceCLI)
	code(t, e, model.ErrorUnauthenticated)
	if model.AsError(e).Details["reason"] != "launch_ended" {
		t.Fatal(e)
	}
	must(t, b.db.Write(ctx, func(tx *store.Tx) error {
		return tx.UpdateTeam("t", store.TeamPatch{State: ptr(model.TeamStateStopped)}, b.now())
	}))
	_, e = b.Authenticate(ctx, "writer", tok, model.FaceCLI)
	code(t, e, model.ErrorTeamNotRunning)
	must(t, b.db.Read(ctx, func(tx *store.Tx) error {
		es, e := tx.ReplayEvents(0, nil, 0)
		if e != nil {
			return e
		}
		if len(es) != 5 {
			t.Fatalf("events: %v", es)
		}
		if es[1].TeamID != nil {
			t.Fatal("unknown member has a team")
		}
		return nil
	}))
}
func TestReadOps(t *testing.T) {
	b, c := setup(t)
	ctx := context.Background()
	h, e := b.Hello(ctx, wire.HelloRequest{Protocols: []int64{2, 1}})
	must(t, e)
	if h.Host.Logind == nil || len(h.Capabilities) != 4 {
		t.Fatal(h)
	}
	_, e = b.Hello(ctx, wire.HelloRequest{Protocols: []int64{2}})
	code(t, e, model.ErrorUnsupportedVersion)
	s, e := b.Status(ctx, wire.StatusRequest{})
	must(t, e)
	if len(s.Teams) != 1 || s.Teams[0].Counts.Members != 2 || s.Teams[0].LastAppContactAt == nil {
		t.Fatal(s)
	}
	m, e := b.Members(ctx, wire.MembersRequest{Team: "t"})
	must(t, e)
	if len(m.Members) != 3 {
		t.Fatal(m)
	}
	team, e := b.ToolTeam(ctx, c, wire.TeamInput{})
	must(t, e)
	if len(team.Members) != 2 || team.Spawn != nil {
		t.Fatal(team)
	}
	in, e := b.ToolInbox(ctx, c, wire.InboxInput{})
	must(t, e)
	if len(in.Messages) != 0 {
		t.Fatal(in)
	}
	_, e = b.Timeline(ctx, wire.TimelineRequest{Team: "t", After: ptr("invalid")})
	code(t, e, model.ErrorCursorInvalid)
	_, e = b.Members(ctx, wire.MembersRequest{Team: "missing"})
	code(t, e, model.ErrorNotFound)
	old := model.Time{Time: time.Now().Add(-time.Minute)}
	must(t, b.db.Write(ctx, func(tx *store.Tx) error {
		return tx.Heartbeat("t", model.Runner{State: model.RunnerStateUp, HeartbeatAt: &old}, b.now())
	}))
	s, e = b.Status(ctx, wire.StatusRequest{})
	must(t, e)
	if s.Teams[0].Runner.State != model.RunnerStateDown {
		t.Fatal(s)
	}
	_, e = b.Status(ctx, wire.StatusRequest{})
	must(t, e)
	must(t, b.db.Read(ctx, func(tx *store.Tx) error {
		es, e := tx.ReplayEvents(0, nil, 0)
		if len(es) != 1 {
			t.Fatalf("stale runner emitted %d events", len(es))
		}
		return e
	}))
}
