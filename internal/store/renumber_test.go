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
