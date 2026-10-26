package store

import (
	"github.com/LexingtonStanley/lugarn-comms/internal/model"
	"testing"
)

func TestRenumberMessage(t *testing.T) {
	s := openTestStore(t)
	makeFixture(t, s)
	writeTest(t, s, func(tx *Tx) error {
		_, e := tx.InsertMessage(MessageParams{Message: model.Message{ID: id(10), Team: "team", Kind: model.MessageKindMessage, Text: "task", Hop: ptr(int64(17)), CreatedAt: testTime()}, SenderID: id(2), Run: 1})
		return e
	})
	writeTest(t, s, func(tx *Tx) error { return tx.RenumberMessage(id(10), ptr(int64(1)), nil) })
	readTest(t, s, func(tx *Tx) error {
		m, e := tx.GetMessage(id(10))
		if e != nil {
			return e
		}
		if m.Hop == nil || *m.Hop != 1 || m.Round != nil {
			t.Fatal(m)
		}
		return nil
	})
	writeTest(t, s, func(tx *Tx) error {
		requireCode(t, tx.RenumberMessage(id(99), nil, nil), model.ErrorNotFound)
		return nil
	})
}

func TestHumanRateLimitIncludesCommittedSameMillisecond(t *testing.T) {
	s := openTestStore(t)
	makeFixture(t, s)
	writeTest(t, s, func(tx *Tx) error {
		at := testTime()
		for i := 0; i < 6; i++ {
			count, e := tx.CountMessagesToHuman(id(2), at)
			if e != nil {
				return e
			}
			if count != int64(i) {
				t.Fatalf("same millisecond count %d want %d", count, i)
			}
			mid := id(10 + i)
			if _, e = tx.InsertMessage(MessageParams{Message: model.Message{ID: mid, Team: "team", Kind: model.MessageKindMessage, Text: "help", CreatedAt: at}, SenderID: id(2), Run: 1}); e != nil {
				return e
			}
			if e = tx.InsertDelivery(DeliveryParams{MessageID: mid, RecipientID: id(1), Delivery: model.Delivery{State: model.DeliveryStateDelivered, Via: ptr(model.DeliveryViaHuman), Notify: ptr(i < 5), UpdatedAt: at, DeliveredAt: &at}, CreatedAt: &at}); e != nil {
				return e
			}
		}
		return nil
	})
}

func TestWorktreePathLookupPrefersLiveReusedPath(t *testing.T) {
	s := openTestStore(t)
	makeFixture(t, s)
	writeTest(t, s, func(tx *Tx) error {
		w := WorktreeRecord{ID: id(30), TeamID: "team", MemberID: id(2), Repo: "repo", Path: "tree", Branch: "lugarn/test/worker", Base: "main", CreatedAt: testTime()}
		if e := tx.InsertWorktree(w); e != nil {
			return e
		}
		if e := tx.RemoveWorktree(w.ID, testTime()); e != nil {
			return e
		}
		w.ID = id(31)
		if e := tx.InsertWorktree(w); e != nil {
			return e
		}
		got, e := tx.GetWorktreeByPath(w.Path)
		if e != nil {
			return e
		}
		if got.ID != w.ID || got.RemovedAt != nil {
			t.Fatal(got)
		}
		return nil
	})
}
