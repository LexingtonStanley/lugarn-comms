package broker

import (
	"context"
	"time"

	"github.com/LexingtonStanley/lugarn-comms/internal/store"
	"github.com/LexingtonStanley/lugarn-comms/internal/wire"
)

// Watch replays immutable event snapshots, marks a snapshot boundary with
// watch.live, then polls at 250 ms. Cancellation and a broken output end cleanly.
func (b *Broker) Watch(ctx context.Context, r wire.WatchRequest, emit func(wire.WatchLine) error) error {
	if e := b.contact(ctx, r.Team); e != nil {
		if ctx.Err() != nil {
			return nil
		}
		return e
	}
	var seq int64
	var replay []store.Event
	e := b.db.Read(ctx, func(tx *store.Tx) error {
		after, e := b.parseCursor(tx, r.After)
		if e != nil {
			return e
		}
		seq, e = tx.CurrentEventSeq()
		if e != nil {
			return e
		}
		if r.After != nil {
			replay, e = tx.ReplayEvents(after, r.Team, 0)
		}
		return e
	})
	if e != nil {
		if ctx.Err() != nil {
			return nil
		}
		return e
	}
	send := func(events []store.Event) (bool, error) {
		for _, ev := range events {
			v, e := b.wireEvent(ev)
			if e != nil {
				return false, e
			}
			if ctx.Err() != nil {
				return false, nil
			}
			if e = emit(v); e != nil {
				return false, nil
			}
		}
		return true, nil
	}
	if ok, e := send(replay); e != nil || !ok {
		return e
	}
	cursor, _ := b.cursorAt(seq)
	if e = emit(wire.WatchLiveLine{Type: "watch.live", Cursor: cursor, At: b.now(), Protocol: 1}); e != nil {
		return nil
	}
	lastOutput := time.Now()
	poll := time.NewTicker(250 * time.Millisecond)
	defer poll.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-poll.C:
			if e = b.contact(ctx, r.Team); e != nil {
				if ctx.Err() != nil {
					return nil
				}
				return e
			}
			var events []store.Event
			var newest int64
			e = b.db.Read(ctx, func(tx *store.Tx) error {
				var e error
				newest, e = tx.CurrentEventSeq()
				if e != nil {
					return e
				}
				events, e = tx.ReplayEvents(seq, r.Team, 0)
				return e
			})
			if e != nil {
				if ctx.Err() != nil {
					return nil
				}
				return e
			}
			seq = newest
			if ok, e := send(events); e != nil || !ok {
				return e
			}
			if len(events) > 0 {
				lastOutput = time.Now()
			}
			if time.Since(lastOutput) >= 15*time.Second {
				cursor, _ = b.cursorAt(seq)
				if e = emit(wire.HeartbeatLine{Type: "heartbeat", Cursor: cursor, At: b.now()}); e != nil {
					return nil
				}
				lastOutput = time.Now()
			}
		}
	}
}
