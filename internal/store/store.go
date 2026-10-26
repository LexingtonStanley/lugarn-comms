package store

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"math/rand/v2"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
	"modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// Store owns a read pool and a dedicated immediate-write pool.
type Store struct {
	read    *sql.DB
	write   *sql.DB
	version int
	hostID  string
}

// Tx is a transaction. Accessors use the context supplied to Read or Write.
type Tx struct {
	tx  *sql.Tx
	ctx context.Context
}

func createDatabaseFile(path string) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	created := err == nil
	if errors.Is(err, os.ErrExist) {
		f, err = os.OpenFile(path, os.O_RDWR, 0)
	}
	if err != nil {
		return asError(err)
	}
	if created && runtime.GOOS != "windows" {
		// O_CREATE permissions are filtered through umask; enforce exactly 0600.
		if err := f.Chmod(0o600); err != nil {
			_ = f.Close()
			return asError(err)
		}
	}
	return asError(f.Close())
}

// Open opens or migrates a SQLite store.
func Open(ctx context.Context, dbPath string) (*Store, error) {
	if dbPath == "" {
		return nil, model.NewError(model.ErrorInvalidRequest, "database path is empty")
	}
	if err := createDatabaseFile(dbPath); err != nil {
		return nil, err
	}

	migrations, err := readMigrations()
	if err != nil {
		return nil, err
	}
	latest := 0
	if len(migrations) > 0 {
		latest = migrations[len(migrations)-1].version
	}
	s := &Store{}
	err = retryBusy(ctx, func() error {
		var versionErr error
		s.version, versionErr = preflightVersion(ctx, dbPath)
		return versionErr
	})
	if err != nil {
		return nil, err
	}
	if s.version > latest {
		return nil, unsupportedStoreVersion(s.version, latest)
	}

	write, err := openPool(ctx, dbPath, "immediate", 1, false)
	if err != nil {
		return nil, err
	}
	s.write = write
	ok := false
	defer func() {
		if !ok {
			_ = write.Close()
		}
	}()
	for _, migration := range migrations {
		if migration.version <= s.version {
			continue
		}
		s.version, err = applyMigration(ctx, write, migration, latest)
		if err != nil {
			return nil, err
		}
	}
	if err := write.QueryRowContext(ctx, "SELECT value FROM meta WHERE key = 'host_id'").Scan(&s.hostID); err != nil {
		return nil, asError(err)
	}
	read, err := openPool(ctx, dbPath, "deferred", 4, true)
	if err != nil {
		return nil, err
	}
	s.read = read
	ok = true
	return s, nil
}

func unsupportedStoreVersion(version, supported int) error {
	return model.NewError(model.ErrorUnsupportedVersion, "store is newer than this binary").WithDetails(map[string]any{
		"store": version, "supported": supported,
	})
}

type migration struct {
	version int
	name    string
	sql     string
}

func readMigrations() ([]migration, error) {
	entries, err := fs.ReadDir(migrationFiles, "migrations")
	if err != nil {
		return nil, asError(err)
	}
	var result []migration
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		prefix, _, ok := strings.Cut(entry.Name(), "_")
		if !ok {
			return nil, asError(fmt.Errorf("invalid migration filename %q", entry.Name()))
		}
		version, err := strconv.Atoi(prefix)
		if err != nil {
			return nil, asError(fmt.Errorf("invalid migration filename %q: %w", entry.Name(), err))
		}
		contents, err := migrationFiles.ReadFile("migrations/" + entry.Name())
		if err != nil {
			return nil, asError(err)
		}
		result = append(result, migration{version: version, name: entry.Name(), sql: string(contents)})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].version < result[j].version })
	for i, m := range result {
		if m.version != i+1 {
			return nil, asError(fmt.Errorf("migration sequence gap at %d", i+1))
		}
	}
	return result, nil
}

func openPool(ctx context.Context, path, txlock string, connections int, readOnly bool) (*sql.DB, error) {
	query := url.Values{}
	query.Add("_pragma", "journal_mode=WAL")
	query.Add("_pragma", "busy_timeout=5000")
	query.Add("_pragma", "foreign_keys=ON")
	query.Set("_txlock", txlock)
	if readOnly {
		query.Set("mode", "ro")
	}
	uri, err := sqliteURI(path, query)
	if err != nil {
		return nil, asError(err)
	}
	db, err := sql.Open("sqlite", uri)
	if err != nil {
		return nil, asError(err)
	}
	db.SetMaxOpenConns(connections)
	if err := retryBusy(ctx, func() error { return db.PingContext(ctx) }); err != nil {
		_ = db.Close()
		return nil, asError(err)
	}
	return db, nil
}

// sqliteURI makes an absolute path into a SQLite file URI. Windows paths need
// a leading slash (file:///C:/...); without it, "C:" lands in the authority.
func sqliteURI(path string, query url.Values) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return fileURI(filepath.ToSlash(abs), query), nil
}

func fileURI(slashPath string, query url.Values) string {
	if !strings.HasPrefix(slashPath, "/") {
		slashPath = "/" + slashPath
	}
	u := &url.URL{Scheme: "file", Path: slashPath, RawQuery: query.Encode()}
	return u.String()
}

func preflightVersion(ctx context.Context, path string) (int, error) {
	query := url.Values{}
	query.Set("mode", "ro")
	query.Add("_pragma", "busy_timeout=5000")
	query.Add("_pragma", "foreign_keys=ON")
	uri, err := sqliteURI(path, query)
	if err != nil {
		return 0, asError(err)
	}
	db, err := sql.Open("sqlite", uri)
	if err != nil {
		return 0, asError(err)
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		return 0, asError(err)
	}
	return currentVersion(ctx, db)
}

type rowQuerier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func currentVersion(ctx context.Context, db rowQuerier) (int, error) {
	var name string
	err := db.QueryRowContext(ctx, "SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'schema_migrations'").Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, asError(err)
	}
	var version sql.NullInt64
	if err := db.QueryRowContext(ctx, "SELECT MAX(version) FROM schema_migrations").Scan(&version); err != nil {
		return 0, asError(err)
	}
	return int(version.Int64), nil
}

func applyMigration(ctx context.Context, db *sql.DB, m migration, latest int) (int, error) {
	var tx *sql.Tx
	err := retryBusy(ctx, func() error {
		var beginErr error
		tx, beginErr = db.BeginTx(ctx, nil)
		return beginErr
	})
	if err != nil {
		return 0, asError(err)
	}
	defer func() { _ = tx.Rollback() }()
	version, err := currentVersion(ctx, tx)
	if err != nil {
		return 0, err
	}
	if version > latest {
		return 0, unsupportedStoreVersion(version, latest)
	}
	if m.version <= version {
		return version, nil
	}
	if version != m.version-1 {
		return 0, asError(fmt.Errorf("migration %s applied over store version %d", m.name, version))
	}
	if _, err := tx.ExecContext(ctx, m.sql); err != nil {
		return 0, asError(fmt.Errorf("migration %s: %w", m.name, err))
	}
	if err := tx.Commit(); err != nil {
		return 0, asError(err)
	}
	return m.version, nil
}

// Version is the applied store schema version.
func (s *Store) Version() int { return s.version }

// HostID is the random host id created by the first migration.
func (s *Store) HostID() string { return s.hostID }

// Close closes both connection pools.
func (s *Store) Close() error {
	var errs []error
	if s.read != nil {
		errs = append(errs, s.read.Close())
	}
	if s.write != nil {
		errs = append(errs, s.write.Close())
	}
	return asError(errors.Join(errs...))
}

// writeWait bounds how long Write keeps retrying a busy BEGIN IMMEDIATE.
const writeWait = 30 * time.Second

// Write runs fn in a BEGIN IMMEDIATE transaction and rolls it back on error.
//
// SQLite's busy handler isn't fair: under steady contention from other
// processes one writer can wait out the whole busy_timeout while others keep
// winning the lock. A BEGIN that fails busy has done nothing, so Write retries
// it with jittered backoff until writeWait or ctx ends.
func (s *Store) Write(ctx context.Context, fn func(*Tx) error) error {
	deadline := time.Now().Add(writeWait)
	for attempt := 0; ; attempt++ {
		tx, err := s.write.BeginTx(ctx, nil)
		if err == nil {
			return s.finish(ctx, tx, fn)
		}
		if !busy(err) || time.Now().After(deadline) {
			return asError(err)
		}
		pause := time.Duration(5+rand.IntN(20+min(attempt, 20)*5)) * time.Millisecond
		t := time.NewTimer(pause)
		select {
		case <-ctx.Done():
			t.Stop()
			return asError(ctx.Err())
		case <-t.C:
		}
	}
}

// busy reports SQLITE_BUSY or SQLITE_LOCKED, including extended codes.
func busy(err error) bool {
	var sqliteErr *sqlite.Error
	if !errors.As(err, &sqliteErr) {
		return false
	}
	code := sqliteErr.Code() & 0xff
	return code == 5 || code == 6
}

// Read runs fn in a read-only snapshot transaction and rolls it back on error.
func (s *Store) Read(ctx context.Context, fn func(*Tx) error) error {
	return s.run(ctx, s.read, fn)
}

func (s *Store) run(ctx context.Context, db *sql.DB, fn func(*Tx) error) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return asError(err)
	}
	return s.finish(ctx, tx, fn)
}

func (s *Store) finish(ctx context.Context, tx *sql.Tx, fn func(*Tx) error) error {
	defer func() { _ = tx.Rollback() }()
	wrapped := &Tx{tx: tx, ctx: ctx}
	if err := fn(wrapped); err != nil {
		_ = tx.Rollback()
		return asError(err)
	}
	if err := tx.Commit(); err != nil {
		_ = tx.Rollback()
		return asError(err)
	}
	return nil
}

func (tx *Tx) exec(query string, args ...any) (sql.Result, error) {
	result, err := tx.tx.ExecContext(tx.ctx, query, args...)
	if err != nil {
		return nil, mapSQLiteError(err)
	}
	return result, nil
}

func (tx *Tx) queryRow(query string, args ...any) *sql.Row {
	return tx.tx.QueryRowContext(tx.ctx, query, args...)
}

func (tx *Tx) query(query string, args ...any) (*sql.Rows, error) {
	rows, err := tx.tx.QueryContext(tx.ctx, query, args...)
	if err != nil {
		return nil, mapSQLiteError(err)
	}
	return rows, nil
}

func asError(err error) error {
	if err == nil {
		return nil
	}
	return model.AsError(err)
}

func retryBusy(ctx context.Context, operation func() error) error {
	deadline := time.Now().Add(6 * time.Second)
	for {
		err := operation()
		if !isSQLiteBusy(err) {
			return err
		}
		if time.Now().After(deadline) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func isSQLiteBusy(err error) bool {
	var sqliteErr *sqlite.Error
	if errors.As(err, &sqliteErr) {
		code := sqliteErr.Code() & 0xff
		return code == 5 || code == 6
	}
	return false
}

func mapSQLiteError(err error) error {
	if err == nil {
		return nil
	}
	message := err.Error()
	var sqliteErr *sqlite.Error
	if errors.As(err, &sqliteErr) {
		if sqliteErr.Code()&0xff == 5 || sqliteErr.Code()&0xff == 6 {
			return asError(err)
		}
		if sqliteErr.Code()&0xff == 19 {
			switch {
			case strings.Contains(message, "members_live_name"), strings.Contains(message, "members.team_id, members.name"), strings.Contains(message, "teams.name"):
				return model.NewError(model.ErrorNameTaken, "name is already in use").WithDetail("cause", message)
			case strings.Contains(message, "requests_one_open"), strings.Contains(message, "requests.member_id"):
				return model.NewError(model.ErrorLeaseConflict, "member already has an open request").WithDetail("cause", message)
			case strings.Contains(message, "turns_one_live"), strings.Contains(message, "turns.member_id"):
				return model.NewError(model.ErrorLeaseConflict, "member already has a live turn").WithDetail("cause", message)
			}
		}
	}
	return asError(err)
}

func notFound(what string) error {
	return model.NewError(model.ErrorNotFound, what+" not found")
}

func unknownMember() error {
	return model.NewError(model.ErrorUnknownMember, "member not found")
}

func conflict(message string) *model.Error {
	return model.NewError(model.ErrorLeaseConflict, message)
}

func parseTime(value string) (model.Time, error) {
	parsed, err := time.Parse("2006-01-02T15:04:05.000Z", value)
	if err != nil {
		return model.Time{}, asError(err)
	}
	return model.Time{Time: parsed}, nil
}

func dbTime(value model.Time) string { return model.FormatTime(value.Time) }

func dbTimePtr(value *model.Time) any {
	if value == nil {
		return nil
	}
	return dbTime(*value)
}

func dbPtr[T any](value *T) any {
	if value == nil {
		return nil
	}
	return *value
}

func nowIfZero(value model.Time) model.Time {
	if value.IsZero() {
		return model.Time{Time: time.Now()}
	}
	return value
}

func defaultTime(value *model.Time) model.Time {
	if value == nil {
		return model.Time{Time: time.Now()}
	}
	return *value
}

func stringPtr(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	v := value.String
	return &v
}

func timeFromNull(value sql.NullString) (*model.Time, error) {
	if !value.Valid {
		return nil, nil
	}
	parsed, err := parseTime(value.String)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}
