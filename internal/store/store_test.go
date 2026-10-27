package store

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
)

func TestOpenMigrateReopenAndDowngrade(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "comms.db")
	s, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if s.Version() != 1 {
		t.Fatalf("version = %d", s.Version())
	}
	if err := model.ValidateHostID(s.HostID()); err != nil {
		t.Fatalf("host id: %v", err)
	}
	hostID := s.HostID()
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Fatalf("database mode = %o", got)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	s, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if s.HostID() != hostID || s.Version() != 1 {
		t.Fatalf("reopen changed metadata")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO schema_migrations (version, name) VALUES (2, 'future')"); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s, err = Open(ctx, path)
	if s != nil {
		_ = s.Close()
		t.Fatal("downgrade returned a store")
	}
	var protocolErr *model.Error
	if !errors.As(err, &protocolErr) || protocolErr.Code != model.ErrorUnsupportedVersion {
		t.Fatalf("downgrade error = %v", err)
	}
	if protocolErr.Details["store"] != 2 || protocolErr.Details["supported"] != 1 {
		t.Fatalf("downgrade details = %#v", protocolErr.Details)
	}
	after, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("downgrade refusal changed database bytes")
	}
}

func TestConcurrentFirstOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "comms.db")
	const opens = 4
	stores := make([]*Store, opens)
	errs := make([]error, opens)
	var wait sync.WaitGroup
	for i := range stores {
		wait.Add(1)
		go func() {
			defer wait.Done()
			stores[i], errs[i] = Open(context.Background(), path)
		}()
	}
	wait.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("open %d: %#v", i, err)
		}
	}
	hostID := stores[0].HostID()
	for i, store := range stores {
		if store.HostID() != hostID || store.Version() != 1 {
			t.Fatalf("open %d metadata mismatch", i)
		}
		if err := store.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestReadIsReadOnly(t *testing.T) {
	s := openTestStore(t)
	makeFixture(t, s)
	if err := s.Read(context.Background(), func(tx *Tx) error {
		return tx.CreateTeam(Team{ID: "other", Name: "other", Pattern: model.PatternCustom})
	}); err == nil {
		t.Fatal("read transaction committed a write")
	}
	readTest(t, s, func(tx *Tx) error {
		_, err := tx.GetTeam("other")
		requireCode(t, err, model.ErrorNotFound)
		return nil
	})
}

func TestTransactionErrorsAndRollback(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	sentinel := model.NewError(model.ErrorDenied, "keep me")
	if err := s.Write(ctx, func(tx *Tx) error { return sentinel }); err != sentinel {
		t.Fatalf("protocol error identity lost: %v", err)
	}
	plain := errors.New("plain")
	err := s.Write(ctx, func(tx *Tx) error { return plain })
	var protocolErr *model.Error
	if !errors.As(err, &protocolErr) || protocolErr.Code != model.ErrorInternal || len(protocolErr.Details) != 0 {
		t.Fatalf("wrapped error = %#v", err)
	}
	team := testTeam()
	if err := s.Write(ctx, func(tx *Tx) error {
		if err := tx.CreateTeam(team); err != nil {
			return err
		}
		return errors.New("rollback")
	}); err == nil {
		t.Fatal("expected rollback error")
	}
	if err := s.Read(ctx, func(tx *Tx) error {
		_, err := tx.GetTeam(team.ID)
		var notFound *model.Error
		if !errors.As(err, &notFound) || notFound.Code != model.ErrorNotFound {
			t.Fatalf("team survived rollback: %v", err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func openTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(context.Background(), filepath.Join(t.TempDir(), "comms.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("close: %v (%#v, %T)", err, err, err)
		}
	})
	return s
}

func testTeam() Team {
	return Team{ID: "team", Name: "team-one", Pattern: model.PatternCustom}
}

func testTime() model.Time { return mustTime("2026-10-26T14:03:07.123Z") }

func mustTime(value string) model.Time {
	parsed, err := parseTime(value)
	if err != nil {
		panic(err)
	}
	return parsed
}

func writeTest(t *testing.T, s *Store, fn func(*Tx) error) {
	t.Helper()
	if err := s.Write(context.Background(), fn); err != nil {
		t.Fatal(err)
	}
}

func readTest(t *testing.T, s *Store, fn func(*Tx) error) {
	t.Helper()
	if err := s.Read(context.Background(), fn); err != nil {
		t.Fatal(err)
	}
}

func requireCode(t *testing.T, err error, code model.ErrorCode) {
	t.Helper()
	var protocolErr *model.Error
	if !errors.As(err, &protocolErr) || protocolErr.Code != code {
		t.Fatalf("error = %#v, want %s", err, code)
	}
}

func TestFileURI(t *testing.T) {
	q := url.Values{}
	q.Set("mode", "ro")
	for slashPath, want := range map[string]string{
		"/home/lex/.lugarn/comms/comms.db":           "file:///home/lex/.lugarn/comms/comms.db?mode=ro",
		"C:/Users/lex/AppData/Local/Lugarn/comms.db": "file:///C:/Users/lex/AppData/Local/Lugarn/comms.db?mode=ro",
		"/tmp/a b#c?d.db":                            "file:///tmp/a%20b%23c%3Fd.db?mode=ro",
	} {
		if got := fileURI(slashPath, q); got != want {
			t.Errorf("fileURI(%q) = %q, want %q", slashPath, got, want)
		}
	}
}
