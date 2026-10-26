package broker

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
	"github.com/LexingtonStanley/lugarn-comms/internal/store"
	"github.com/LexingtonStanley/lugarn-comms/internal/wire"
)

func spawning(t *testing.T, b *Broker) {
	t.Helper()
	must(t, b.db.Write(context.Background(), func(tx *store.Tx) error {
		return tx.PutSpawnSettings(store.SpawnSettings{MemberID: "writer", Enabled: true, RunningAtOnce: 1, PerRun: 2, Mode: model.ModeHeadless, Folder: model.SpawnFolderShared, MaxProfile: model.ProfileWorkspace, Brief: ptr("worker brief")})
	}))
	b.opts.Wait = func(context.Context, time.Duration) error { return nil }
	b.opts.RequestTimeout = time.Millisecond
}
func TestSpawnTransactionAndCaps(t *testing.T) {
	b, c := setup(t)
	spawning(t, b)
	ctx := context.Background()
	_, e := b.ToolSpawn(ctx, c, wire.SpawnInput{Name: "worker", Harness: model.HarnessCodex, Task: "task"})
	code(t, e, model.ErrorTimeout)
	id := model.AsError(e).Details["member"].(string)
	must(t, b.db.Read(ctx, func(tx *store.Tx) error {
		m, e := tx.GetStoredMember(id)
		if e != nil {
			return e
		}
		if m.Member.State != model.MemberStateStarting || m.Member.SpawnedBy.ID != "writer" || *m.Brief != "worker brief" {
			t.Fatal(m)
		}
		g, e := tx.GetGridEdge("writer", id)
		if e != nil {
			return e
		}
		if len(g.Can) != 2 {
			t.Fatal(g)
		}
		rs, e := tx.ListOpenRequests("t")
		if e != nil {
			return e
		}
		if len(rs) != 1 || rs[0].Kind != model.RequestKindSpawn {
			t.Fatal(rs)
		}
		team, e := tx.GetTeam("t")
		if e != nil {
			return e
		}
		if team.Revision != 2 {
			t.Fatal(team)
		}
		ds, e := tx.NextDeliveries(id, 0)
		if e != nil {
			return e
		}
		if len(ds) != 1 {
			t.Fatal(ds)
		}
		msg, e := tx.GetMessage(ds[0].MessageID)
		if e != nil {
			return e
		}
		if msg.Kind != model.MessageKindTask || msg.Hop == nil || *msg.Hop != 1 {
			t.Fatal(msg)
		}
		return nil
	}))
	_, e = b.ToolSpawn(ctx, c, wire.SpawnInput{Name: "second", Harness: model.HarnessCodex, Task: "task"})
	code(t, e, model.ErrorSpawnCapReached)
	old := model.Time{Time: time.Now().Add(-time.Minute)}
	must(t, b.db.Write(ctx, func(tx *store.Tx) error {
		return tx.Heartbeat("t", model.Runner{State: model.RunnerStateUp, HeartbeatAt: &old}, b.now())
	}))
	_, e = b.ToolSpawn(ctx, c, wire.SpawnInput{Name: "third", Harness: model.HarnessCodex, Task: "task"})
	code(t, e, model.ErrorRunnerUnavailable)
}
func TestConcurrentSpawn(t *testing.T) {
	b, c := setup(t)
	spawning(t, b)
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, name := range []string{"one", "two"} {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			_, e := b.ToolSpawn(context.Background(), c, wire.SpawnInput{Name: name, Harness: model.HarnessCodex, Task: "task"})
			errs <- e
		}(name)
	}
	wg.Wait()
	close(errs)
	counts := map[model.ErrorCode]int{}
	for e := range errs {
		if e == nil {
			t.Fatal("no runner should finish the request")
		}
		counts[model.AsError(e).Code]++
	}
	if counts[model.ErrorTimeout] != 1 || counts[model.ErrorSpawnCapReached] != 1 {
		t.Fatal(counts)
	}
}
func TestStopPolicy(t *testing.T) {
	b, c := setup(t)
	ctx := context.Background()
	_, e := b.ToolStop(ctx, c, wire.StopInput{Member: "reviewer"})
	code(t, e, model.ErrorDenied)
	spawning(t, b)
	_, e = b.ToolSpawn(ctx, c, wire.SpawnInput{Name: "worker", Harness: model.HarnessCodex, Task: "task"})
	code(t, e, model.ErrorTimeout)
	id := model.AsError(e).Details["member"].(string)
	must(t, b.db.Write(ctx, func(tx *store.Tx) error {
		return tx.SetMemberState(id, model.MemberStateStopped, ptr(model.StopReasonLaunchFailed), nil, ptr(ptr(b.now())), b.now())
	}))
	out, e := b.ToolStop(ctx, c, wire.StopInput{Member: "worker"})
	must(t, e)
	if out.Member.State != model.MemberStateStopped {
		t.Fatal(out)
	}
}
