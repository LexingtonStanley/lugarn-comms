package broker

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/LexingtonStanley/lugarn-comms/internal/hc"
	"github.com/LexingtonStanley/lugarn-comms/internal/hc/hctest"
	"github.com/LexingtonStanley/lugarn-comms/internal/store"
)

func TestHCLiveOwnership(t *testing.T) {
	c, _, binary := hctest.Private(t)
	b, _ := setup(t)
	bin, e := os.Executable()
	must(t, e)
	i, e := NewIntegration(b, IntegrationOptions{Binary: bin, HCBinary: binary, HomeDir: t.TempDir()})
	must(t, e)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	terminal, e := c.Run(ctx, hc.RunOptions{Argv: []string{"/bin/cat"}, Cwd: t.TempDir(), Env: map[string]string{}, Labels: i.hcLabels("t", "member")})
	must(t, e)
	r := store.HCTerminal{Team: "t", Core: c.Hello.Core.ID, Address: terminal.Address, PID: *terminal.PID}
	wrong := r
	wrong.PID++
	if e = i.hcClose(ctx, c, wrong); e == nil {
		t.Fatal("pid mismatch accepted")
	}
	current, e := c.Get(ctx, r.Address)
	must(t, e)
	if current.State != "live" {
		t.Fatal("foreign terminal killed")
	}
	must(t, i.hcClose(ctx, c, r))
	if _, e = c.Get(ctx, r.Address); !hc.IsGone(e) {
		t.Fatal(e)
	}
}
