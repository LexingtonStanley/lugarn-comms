package broker

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
	"github.com/LexingtonStanley/lugarn-comms/internal/store"
)

func TestOpenPreflightLeavesHeadlessMemberUnchanged(t *testing.T) {
	for _, kind := range []string{"harness-issue", "binary-resolution", "live-turn"} {
		t.Run(kind, func(t *testing.T) {
			b, r, m := headlessFixture(t, &headlessStub{})
			i, e := NewIntegration(b, IntegrationOptions{HomeDir: t.TempDir()})
			must(t, e)
			l := &integrationLifecycle{i: i, r: r}
			switch kind {
			case "harness-issue":
				b.opts.Host = unavailableHost{}
			case "binary-resolution":
				r.opts.Binary = func(model.Harness) (string, error) { return "", errors.New("missing") }
			case "live-turn":
				sendToWriter(t, b, "queued")
				turn, e := r.prepareTurn(context.Background(), m)
				must(t, e)
				if turn == nil {
					t.Fatal("no turn")
				}
			}
			var before, after *model.Member
			read := func(out **model.Member) {
				t.Helper()
				must(t, b.db.Read(context.Background(), func(tx *store.Tx) error { var e error; *out, e = tx.GetMember(m.ID); return e }))
			}
			read(&before)
			if e = l.Open(context.Background(), *before, model.LeaseCauseOpenTerminal); e == nil {
				t.Fatal("expected refusal")
			}
			read(&after)
			if !reflect.DeepEqual(before, after) {
				t.Fatal("preflight altered healthy member", before, after)
			}
		})
	}
}
