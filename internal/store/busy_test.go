package store

import (
	"context"
	"database/sql"
	"net/url"
	"path/filepath"
	"testing"
	"time"
)

// A writer locked out for longer than busy_timeout still gets through once the
// lock frees, instead of failing internal after five seconds.
func TestWriteOutlastsBusyTimeout(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "comms.db")
	s, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	query := url.Values{}
	query.Add("_pragma", "busy_timeout=0")
	query.Set("_txlock", "immediate")
	uri, err := sqliteURI(path, query)
	if err != nil {
		t.Fatal(err)
	}
	holder, err := sql.Open("sqlite", uri)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = holder.Close() })
	lock, err := holder.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	const hold = 6 * time.Second
	go func() {
		time.Sleep(hold)
		_ = lock.Rollback()
	}()
	start := time.Now()
	writeTest(t, s, func(tx *Tx) error { return tx.CreateTeam(testTeam()) })
	if waited := time.Since(start); waited < hold-time.Second {
		t.Fatalf("write finished after %v while the lock was held for %v", waited, hold)
	}
}

func TestWriteGivesUpWithContext(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "comms.db")
	s, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	query := url.Values{}
	query.Set("_txlock", "immediate")
	uri, err := sqliteURI(path, query)
	if err != nil {
		t.Fatal(err)
	}
	holder, err := sql.Open("sqlite", uri)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = holder.Close() })
	lock, err := holder.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = lock.Rollback() })
	ctx, cancel := context.WithTimeout(context.Background(), 7*time.Second)
	defer cancel()
	start := time.Now()
	if err := s.Write(ctx, func(tx *Tx) error { return tx.CreateTeam(testTeam()) }); err == nil {
		t.Fatal("write succeeded while another connection held the lock")
	}
	if waited := time.Since(start); waited > 12*time.Second {
		t.Fatalf("write ignored its context for %v", waited)
	}
}
