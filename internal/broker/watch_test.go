package broker

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
	"github.com/LexingtonStanley/lugarn-comms/internal/store"
	"github.com/LexingtonStanley/lugarn-comms/internal/wire"
)

func TestWatchReplayAndLive(t *testing.T) {
	b, c := setup(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	zero, e := b.cursorAt(0)
	must(t, e)
	_, e = b.ToolSend(ctx, c, wire.SendInput{To: "reviewer", Text: "replay"})
	must(t, e)
	seen := []string{}
	live := false
	var prev int64
	e = b.Watch(ctx, wire.WatchRequest{Team: ptr("t"), After: &zero}, func(l wire.WatchLine) error {
		switch v := l.(type) {
		case wire.Event:
			s, e := model.ParseCursor(v.Cursor, b.db.HostID())
			if e != nil {
				return e
			}
			if s <= prev {
				t.Fatalf("events out of order: %d after %d", s, prev)
			}
			prev = s
			seen = append(seen, string(v.Type))
			if live && v.Type == model.EventMessageCreated {
				cancel()
			}
		case wire.WatchLiveLine:
			live = true
			_, e := b.ToolSend(ctx, c, wire.SendInput{To: "reviewer", Text: "live"})
			return e
		}
		return nil
	})
	must(t, e)
	if !live || len(seen) < 3 {
		t.Fatal(seen)
	}
}
func TestWatchInvalidCursorAndBrokenOutput(t *testing.T) {
	b, _ := setup(t)
	for _, cursor := range []string{"wrong", b.db.HostID() + ".9999"} {
		e := b.Watch(context.Background(), wire.WatchRequest{After: &cursor}, func(wire.WatchLine) error { t.Fatal("output before validation"); return nil })
		code(t, e, model.ErrorCursorInvalid)
	}
	must(t, b.Watch(context.Background(), wire.WatchRequest{}, func(wire.WatchLine) error { return errors.New("broken pipe") }))
}

func TestWatchPollReadsWhileWriterIsBusy(t *testing.T) {
	b, caller := setup(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	// A fixed domain clock keeps the contact interval below five seconds.
	now := time.Now()
	b.opts.Clock = func() time.Time { return now }
	locked := make(chan struct{})
	release := make(chan struct{})
	writerDone := make(chan error, 1)
	writerStarted := false
	sawMessage := false
	err := b.Watch(ctx, wire.WatchRequest{Team: ptr("t")}, func(line wire.WatchLine) error {
		switch v := line.(type) {
		case wire.WatchLiveLine:
			if _, err := b.ToolSend(ctx, caller, wire.SendInput{To: "reviewer", Text: "live"}); err != nil {
				return err
			}
			writerStarted = true
			go func() {
				writerDone <- b.db.Write(ctx, func(*store.Tx) error {
					close(locked)
					select {
					case <-release:
						return nil
					case <-ctx.Done():
						return ctx.Err()
					}
				})
			}()
			select {
			case <-locked:
			case <-ctx.Done():
				return ctx.Err()
			}
		case wire.Event:
			if v.Type == model.EventMessageCreated {
				sawMessage = true
				cancel()
			}
		}
		return nil
	})
	close(release)
	if writerStarted {
		<-writerDone
	}
	must(t, err)
	if !sawMessage {
		t.Fatal("watch could not read committed events while another writer held the lock")
	}
}

func TestWatchContactEveryFiveSeconds(t *testing.T) {
	b, _ := setup(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	now := time.Now().UTC().Truncate(time.Millisecond)
	var clockNanos atomic.Int64
	clockNanos.Store(now.UnixNano())
	b.opts.Clock = func() time.Time { return time.Unix(0, clockNanos.Load()) }
	var marker int64
	sawRefresh := false
	err := b.Watch(ctx, wire.WatchRequest{Team: ptr("t")}, func(line wire.WatchLine) error {
		switch v := line.(type) {
		case wire.WatchLiveLine:
			// Four seconds is too early for contact, even with a stale heartbeat.
			clockNanos.Store(now.Add(4 * time.Second).UnixNano())
			return b.db.Write(ctx, func(tx *store.Tx) error {
				old := model.Time{Time: now.Add(-time.Minute)}
				if err := tx.Heartbeat("t", model.Runner{State: model.RunnerStateUp, HeartbeatAt: &old}, b.now()); err != nil {
					return err
				}
				var err error
				marker, err = tx.AppendEvent(store.Event{TeamID: ptr("t"), Type: model.EventAuthRejected, At: b.now(), Data: json.RawMessage(`{"claimed_member":null,"failure":"no_token","face":"cli"}`)})
				return err
			})
		case wire.Event:
			if v.Type == model.EventAuthRejected {
				if err := b.db.Read(ctx, func(tx *store.Tx) error {
					team, err := tx.GetTeam("t")
					if err != nil {
						return err
					}
					if team.Runner.State != model.RunnerStateUp {
						t.Error("contact ran before five seconds")
					}
					return nil
				}); err != nil {
					return err
				}
				clockNanos.Store(now.Add(5 * time.Second).UnixNano())
			} else if v.Type == model.EventRunnerStateChanged {
				seq, err := model.ParseCursor(v.Cursor, b.db.HostID())
				if err != nil {
					return err
				}
				if seq <= marker || !v.At.Equal(now.Add(5*time.Second)) {
					t.Errorf("unexpected contact event: %#v", v)
				}
				sawRefresh = true
				cancel()
			}
		}
		return nil
	})
	must(t, err)
	if !sawRefresh {
		t.Fatal("watch did not refresh contact at five seconds")
	}
}
