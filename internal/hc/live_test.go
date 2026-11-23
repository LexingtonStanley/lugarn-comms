package hc_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/LexingtonStanley/lugarn-comms/internal/hc"
	"github.com/LexingtonStanley/lugarn-comms/internal/hc/hctest"
)

func TestHCLive(t *testing.T) {
	c, session, _ := hctest.Private(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	terminal, e := c.Run(ctx, hc.RunOptions{Argv: []string{"/bin/sh", "-c", "printf '\033]7501;state=idle:app=pi\007'; cat"}, Cwd: t.TempDir(), Env: map[string]string{"LUGARN_TEST": "present"}, Name: ptrHC("hc-live"), Labels: map[string]string{"lugarn.test": session}})
	if e != nil {
		t.Fatal(e)
	}
	if terminal.Parent != nil || terminal.Keep || terminal.PID == nil {
		t.Fatal(terminal)
	}
	if _, e = c.Send(ctx, terminal.Address, "hello from private hc live test"); e != nil {
		t.Fatal(e)
	}
	text, e := c.Read(ctx, terminal.Address, 20)
	if e != nil || !strings.Contains(text, "hello from private hc live test") {
		t.Fatal(text, e)
	}
	killed, e := c.Kill(ctx, terminal.Address)
	if e != nil || killed.State != "finished" {
		t.Fatal(killed, e)
	}
	if e = c.Remove(ctx, terminal.Address); e != nil {
		t.Fatal(e)
	}
	if _, e = c.Get(ctx, terminal.Address); !hc.IsGone(e) {
		t.Fatal(e)
	}
}
func ptrHC[T any](v T) *T { return &v }
