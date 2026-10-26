package broker

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
	"github.com/LexingtonStanley/lugarn-comms/internal/statedir"
	"github.com/LexingtonStanley/lugarn-comms/internal/store"
)

type cleanupFilesSpy struct {
	fakeLaunchFiles
	deletes int
}

func (f *cleanupFilesSpy) Delete(context.Context, string) error { f.deletes++; return nil }
func cleanupID(t *testing.T) string                             { t.Helper(); id, err := model.NewULID(); must(t, err); return id }
func cleanupLaunch(t *testing.T, b *Broker, member string, generation int64, ended bool) string {
	t.Helper()
	id := cleanupID(t)
	now := b.now()
	l := store.Launch{ID: id, MemberID: member, Generation: generation, Driver: model.ModeHeadless, StartedAt: now}
	if ended {
		l.EndedAt = &now
		l.EndReason = ptr(model.LaunchEndReasonStopped)
	}
	must(t, b.db.Write(context.Background(), func(tx *store.Tx) error { return tx.InsertLaunch(l) }))
	return id
}
func cleanupFile(t *testing.T, path string, at time.Time) {
	t.Helper()
	must(t, os.WriteFile(path, []byte("preserve contents"), 0600))
	must(t, os.Chtimes(path, at, at))
}
func cleanupPresent(t *testing.T, path string, want bool) {
	t.Helper()
	_, err := os.Lstat(path)
	if want {
		must(t, err)
	} else if !os.IsNotExist(err) {
		t.Errorf("cleanup left %s: %v", path, err)
	}
}
func cleanupFixture(t *testing.T) (*Broker, *Runner, time.Time) {
	t.Helper()
	b, _ := setup(t)
	must(t, statedir.Ensure(b.opts.StateDir))
	now := time.Now().UTC().Truncate(time.Second)
	b.opts.Clock = func() time.Time { return now }
	home := t.TempDir()
	must(t, os.MkdirAll(filepath.Join(home, ".lugarn", "conversations"), 0700))
	r := NewRunner(b, "t", RunnerOptions{Files: &cleanupFilesSpy{}, HomeDir: home})
	return b, r, now
}

func TestRunnerCleanupExactNamesAndGrace(t *testing.T) {
	b, r, now := cleanupFixture(t)
	root := filepath.Join(b.opts.StateDir, "launch")
	ended := cleanupLaunch(t, b, "writer", 1, true)
	live := cleanupLaunch(t, b, "reviewer", 1, false)
	var removed, kept []string
	for _, suffix := range []string{".token", ".json", ".settings.json", ".opencode.json"} {
		path := filepath.Join(root, ended+suffix)
		cleanupFile(t, path, now)
		removed = append(removed, path)
		path = filepath.Join(root, live+suffix)
		cleanupFile(t, path, now.Add(-2*time.Minute))
		kept = append(kept, path)
	}
	dir := filepath.Join(root, ended)
	must(t, os.Mkdir(dir, 0700))
	cleanupFile(t, filepath.Join(dir, "message.md"), now)
	removed = append(removed, dir)
	unknown := cleanupID(t)
	for _, tt := range []struct {
		suffix string
		age    time.Duration
		remove bool
	}{{".token", 61 * time.Second, true}, {".json", time.Second, false}, {".settings.json", 60 * time.Second, false}, {".opencode.json", 61 * time.Second, true}} {
		path := filepath.Join(root, unknown+tt.suffix)
		cleanupFile(t, path, now.Add(-tt.age))
		if tt.remove {
			removed = append(removed, path)
		} else {
			kept = append(kept, path)
		}
	}
	oldDir := filepath.Join(root, unknown)
	must(t, os.Mkdir(oldDir, 0700))
	cleanupFile(t, filepath.Join(oldDir, "message.md"), now)
	must(t, os.Chtimes(oldDir, now.Add(-61*time.Second), now.Add(-61*time.Second)))
	removed = append(removed, oldDir)
	for _, name := range []string{ended + ".token.bak", ended + ".process.json", unknown + ".settings.json.bak", cleanupID(t), "not-a-ulid.token"} {
		path := filepath.Join(root, name)
		cleanupFile(t, path, now.Add(-2*time.Minute))
		kept = append(kept, path)
	}
	wrongType := filepath.Join(root, cleanupID(t)+".token")
	must(t, os.Mkdir(wrongType, 0700))
	must(t, os.Chtimes(wrongType, now.Add(-2*time.Minute), now.Add(-2*time.Minute)))
	kept = append(kept, wrongType)
	must(t, r.cleanLaunchFiles(context.Background()))
	for _, path := range removed {
		cleanupPresent(t, path, false)
	}
	for _, path := range kept {
		cleanupPresent(t, path, true)
	}
	if spy := r.opts.Files.(*cleanupFilesSpy); spy.deletes != 0 {
		t.Errorf("broad launch deletion ran %d times instead of deleting listed entries", spy.deletes)
	}
}

func TestRunnerCleanupPreservesOtherTeams(t *testing.T) {
	b, r, now := cleanupFixture(t)
	folder := t.TempDir()
	must(t, b.db.Write(context.Background(), func(tx *store.Tx) error {
		if err := tx.CreateTeam(store.Team{ID: "foreign-team", Name: "foreign", Pattern: model.PatternCustom}); err != nil {
			return err
		}
		return tx.InsertMember(store.MemberParams{ID: "foreign-member", TeamID: "foreign-team", Name: "foreign", Kind: model.MemberKindAgent, Origin: model.MemberOriginSpec, Role: ptr(model.RoleWorker), Harness: ptr(model.HarnessCodex), Profile: ptr(model.ProfileReadOnly), Mode: ptr(model.ModeHeadless), Folder: &folder, Brief: ptr("brief"), State: model.MemberStateStopped})
	}))
	for generation := int64(1); generation <= 2; generation++ {
		id := cleanupLaunch(t, b, "foreign-member", generation, generation == 2)
		path := filepath.Join(b.opts.StateDir, "launch", id+".token")
		cleanupFile(t, path, now.Add(-2*time.Minute))
		must(t, r.cleanLaunchFiles(context.Background()))
		cleanupPresent(t, path, true)
	}
}

func TestRunnerCleanupPointerFilesAreExactKnownAndRegular(t *testing.T) {
	b, r, now := cleanupFixture(t)
	root := filepath.Join(r.opts.HomeDir, ".lugarn", "conversations")
	ended := cleanupLaunch(t, b, "writer", 1, true)
	live := cleanupLaunch(t, b, "reviewer", 1, false)
	directory := cleanupLaunch(t, b, "writer", 2, true)
	exact := filepath.Join(root, ended)
	cleanupFile(t, exact, now)
	var kept []string
	for _, name := range []string{live, cleanupID(t), ended + ".json", ended + ".token"} {
		path := filepath.Join(root, name)
		cleanupFile(t, path, now.Add(-2*time.Minute))
		kept = append(kept, path)
	}
	path := filepath.Join(root, directory)
	must(t, os.Mkdir(path, 0700))
	kept = append(kept, path)
	must(t, r.cleanLaunchFiles(context.Background()))
	cleanupPresent(t, exact, false)
	for _, path := range kept {
		cleanupPresent(t, path, true)
	}
}

func TestRunnerCleanupSymlinksDoNotTouchTargets(t *testing.T) {
	b, r, now := cleanupFixture(t)
	root := filepath.Join(b.opts.StateDir, "launch")
	outside := t.TempDir()
	target := filepath.Join(outside, "target")
	must(t, os.Mkdir(target, 0700))
	sentinel := filepath.Join(target, "sentinel")
	cleanupFile(t, sentinel, now.Add(-2*time.Minute))
	ended := cleanupLaunch(t, b, "writer", 1, true)
	link := filepath.Join(root, ended)
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	tokenLink := filepath.Join(root, ended+".token")
	must(t, os.Symlink(sentinel, tokenLink))
	pointerLink := filepath.Join(r.opts.HomeDir, ".lugarn", "conversations", ended)
	must(t, os.Symlink(sentinel, pointerLink))
	freshUnknown := filepath.Join(root, cleanupID(t)+".token")
	must(t, os.Symlink(sentinel, freshUnknown))
	must(t, r.cleanLaunchFiles(context.Background()))
	cleanupPresent(t, link, false)
	cleanupPresent(t, tokenLink, false)
	cleanupPresent(t, pointerLink, true)
	cleanupPresent(t, freshUnknown, true)
	cleanupPresent(t, sentinel, true)
	raw, err := os.ReadFile(sentinel)
	must(t, err)
	if string(raw) != "preserve contents" {
		t.Fatal("symlink target changed")
	}
}

func TestRunnerCleanupUsesListedSnapshotThenCurrentStore(t *testing.T) {
	b, r, now := cleanupFixture(t)
	root := filepath.Join(b.opts.StateDir, "launch")
	old := now.Add(-2 * time.Minute)
	orphan := filepath.Join(root, cleanupID(t)+".token")
	cleanupFile(t, orphan, old)
	activeID := cleanupID(t)
	active := filepath.Join(root, activeID+".token")
	cleanupFile(t, active, old)
	entries, err := os.ReadDir(root)
	must(t, err)
	// A launch committed after the listing must be visible to cleanup's store read.
	must(t, b.db.Write(context.Background(), func(tx *store.Tx) error {
		return tx.InsertLaunch(store.Launch{ID: activeID, MemberID: "writer", Generation: 1, Driver: model.ModeHeadless, StartedAt: b.now()})
	}))
	later := filepath.Join(root, cleanupID(t)+".token")
	cleanupFile(t, later, old)
	must(t, r.cleanListedLaunchFiles(context.Background(), root, entries))
	cleanupPresent(t, orphan, false)
	cleanupPresent(t, active, true)
	cleanupPresent(t, later, true)
}

func TestRunnerCleanupOldUnknownSymlinksAreUnlinked(t *testing.T) {
	b, r, _ := cleanupFixture(t)
	outside := t.TempDir()
	sentinel := filepath.Join(outside, "sentinel")
	cleanupFile(t, sentinel, time.Now())
	root := filepath.Join(b.opts.StateDir, "launch")
	fileLink := filepath.Join(root, cleanupID(t)+".settings.json")
	if err := os.Symlink(sentinel, fileLink); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	directoryLink := filepath.Join(root, cleanupID(t))
	must(t, os.Symlink(outside, directoryLink))
	future := time.Now().Add(2 * time.Minute)
	b.opts.Clock = func() time.Time { return future }
	must(t, r.cleanLaunchFiles(context.Background()))
	cleanupPresent(t, fileLink, false)
	cleanupPresent(t, directoryLink, false)
	cleanupPresent(t, sentinel, true)
}

func TestRunnerCleanupRejectsLinkedLaunchRoot(t *testing.T) {
	b, r, now := cleanupFixture(t)
	ended := cleanupLaunch(t, b, "writer", 1, true)
	outside := t.TempDir()
	target := filepath.Join(outside, ended+".token")
	cleanupFile(t, target, now)
	root := filepath.Join(b.opts.StateDir, "launch")
	must(t, os.Remove(root))
	if err := os.Symlink(outside, root); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	code(t, r.cleanLaunchFiles(context.Background()), model.ErrorInternal)
	cleanupPresent(t, target, true)
}

func TestRunnerCleanupDoesNotTraversePointerParentLinks(t *testing.T) {
	for _, parent := range []string{".lugarn", "conversations"} {
		t.Run(parent, func(t *testing.T) {
			b, r, now := cleanupFixture(t)
			ended := cleanupLaunch(t, b, "writer", 1, true)
			outside := t.TempDir()
			targetRoot := outside
			link := filepath.Join(r.opts.HomeDir, ".lugarn")
			if parent == ".lugarn" {
				targetRoot = filepath.Join(outside, "conversations")
				must(t, os.Mkdir(targetRoot, 0700))
			} else {
				link = filepath.Join(link, "conversations")
			}
			target := filepath.Join(targetRoot, ended)
			cleanupFile(t, target, now)
			must(t, os.RemoveAll(link))
			if err := os.Symlink(outside, link); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			must(t, r.cleanLaunchFiles(context.Background()))
			cleanupPresent(t, target, true)
			cleanupPresent(t, link, true)
		})
	}
}

func TestRunnerCleanupMissingLaunchDirectoryStillCleansKnownPointer(t *testing.T) {
	b, r, now := cleanupFixture(t)
	ended := cleanupLaunch(t, b, "writer", 1, true)
	pointer := filepath.Join(r.opts.HomeDir, ".lugarn", "conversations", ended)
	cleanupFile(t, pointer, now)
	must(t, os.Remove(filepath.Join(b.opts.StateDir, "launch")))
	must(t, r.cleanLaunchFiles(context.Background()))
	cleanupPresent(t, pointer, false)
}
