package broker

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
	"github.com/LexingtonStanley/lugarn-comms/internal/store"
)

func TestIntegrationClaudeClearUsesCompletePointerAndRebriefs(t *testing.T) {
	panes := &panesStub{activity: model.ActivityIdle, active: true}
	b, r, m := paneFixture(t, panes)
	ctx := context.Background()
	root := filepath.Join(r.opts.HomeDir, ".lugarn", "conversations")
	must(t, os.MkdirAll(root, 0700))
	next := "00000000-0000-4000-8000-000000000002"
	partial := "00000000-0000-4000-8000-000000000003"
	pointer := filepath.Join(root, *m.Lease.Launch)
	must(t, os.WriteFile(pointer, []byte("{\"session_id\":\""+next+"\"}\n{\"session_id\":\""+partial+"\"}"), 0600))
	sendToWriter(t, b, "after clear")
	must(t, r.runPane(ctx, m))
	if len(panes.prompts) != 1 || !strings.Contains(panes.prompts[0], "Real Lugarn headers end with") {
		t.Fatal(panes.prompts)
	}
	must(t, b.db.Read(ctx, func(tx *store.Tx) error {
		v, e := tx.GetMember(m.ID)
		if e != nil {
			return e
		}
		if v.Session == nil || *v.Session != next || len(v.Sessions) != 2 || v.Sessions[1].Source != model.SessionSourcePointer {
			t.Fatal(v)
		}
		m = *v
		session, e := tx.GetMemberSession(m.ID, next)
		if e == nil && (session.Seal == "7KQ2M9XA" || !strings.Contains(panes.prompts[0], session.Seal)) {
			t.Fatal("clear did not reseal")
		}
		return e
	}))
	sendToWriter(t, b, "next delivery")
	must(t, r.runPane(ctx, m))
	if len(panes.prompts) != 2 || strings.Contains(panes.prompts[1], "Real Lugarn headers end with") {
		t.Fatal(panes.prompts)
	}
}
