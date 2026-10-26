package broker

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LexingtonStanley/lugarn-comms/internal/store"
)

type detachAckHandle struct {
	*turnStub
	armed *atomic.Bool
}

func (h *detachAckHandle) Acknowledgements() <-chan TurnAcknowledgement {
	h.armed.Store(true)
	return h.acks
}

type detachAckDriver struct{ handle TurnHandle }

func (d detachAckDriver) Start(context.Context, TurnLaunch, string) (TurnHandle, error) {
	return d.handle, nil
}
func TestRunnerExitDetachesWhenAcknowledgementWriteIsCancelled(t *testing.T) {
	b, r, m := headlessFixture(t, &headlessStub{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var armed atomic.Bool
	h := &detachAckHandle{turnStub: newTurnStub(), armed: &armed}
	h.acknowledge("00000000-0000-4000-8000-000000000001")
	defer h.complete(TurnOutcome{Acknowledged: true, ExitCode: ptr(int64(0))})
	r.opts.Headless = detachAckDriver{handle: h}
	sendToWriter(t, b, "pending task")
	// Start/PID/birth persistence finish first. Cancel only inside the subsequent
	// acknowledgement transaction, so this deterministically exercises its error.
	b.opts.Clock = func() time.Time {
		if armed.Swap(false) {
			cancel()
		}
		return time.Now()
	}
	must(t, r.runTurn(ctx, m))
	if ctx.Err() == nil {
		t.Fatal("did not cancel the acknowledgement write")
	}
	h.mu.Lock()
	cancelled := h.cancelled
	h.mu.Unlock()
	if cancelled != 0 {
		t.Fatal("ordinary runner exit killed a durably recorded turn")
	}
	must(t, b.db.Read(context.Background(), func(tx *store.Tx) error {
		turn, e := tx.GetLiveTurn(m.ID)
		if e != nil {
			return e
		}
		if turn.PID == nil || r.processBirth(*turn) == "" {
			t.Fatal("detached without a recorded process identity", turn)
		}
		return nil
	}))
}
