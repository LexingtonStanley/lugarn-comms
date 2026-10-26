package broker

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
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
