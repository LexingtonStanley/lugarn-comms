package broker

import (
	"context"
	"testing"

	"github.com/LexingtonStanley/lugarn-comms/internal/herdr"
	"github.com/LexingtonStanley/lugarn-comms/internal/hostprobe"
	"github.com/LexingtonStanley/lugarn-comms/internal/wire"
)

type logindProbe struct {
	fakeProbe
	calls int
}

func (p *logindProbe) Logind(context.Context) *hostprobe.LogindInfo {
	p.calls++
	return &hostprobe.LogindInfo{KillUserProcesses: true, Linger: true}
}
func TestIntegrationStartHerdrAndCapabilities(t *testing.T) {
	b, _ := setup(t)
	probe := &logindProbe{}
	b.opts.Probe = probe
	called := false
	i, e := NewIntegration(b, IntegrationOptions{HomeDir: t.TempDir(), StartServer: func(_ context.Context, _, server string, options ...herdr.ServerOption) (herdr.StartResult, error) {
		called = true
		if server != "lugarn-test-fake12345" || len(options) != 1 {
			t.Fatal(server, len(options))
		}
		return herdr.StartResult{Started: true, Method: herdr.MethodSystemd, Status: herdr.Status{Installed: true, Running: true, Session: server, Version: "0.9.5"}}, nil
	}})
	must(t, e)
	i.Attach()
	result, e := b.StartHerdr(context.Background(), wire.StartHerdrRequest{Server: ptr("lugarn-test-fake12345")})
	must(t, e)
	if !called || probe.calls != 1 || !result.Started || result.Method == nil || *result.Method != wire.HerdrStartMethod("systemd") {
		t.Fatal(result)
	}
	hello, e := b.Hello(context.Background(), wire.HelloRequest{Protocols: []int64{1}})
	must(t, e)
	if len(hello.Capabilities) != 8 {
		t.Fatal(hello.Capabilities)
	}
}
