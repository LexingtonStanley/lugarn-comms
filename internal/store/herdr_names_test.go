package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/LexingtonStanley/lugarn-comms/internal/herdr"
)

func TestHerdrMigrationPreservesOldNamesAndNeverLaunchedMembers(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "old.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := migrationFiles.ReadFile("migrations/0001_init.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(string(raw)); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO teams (id,name,pattern,spec_version,herdr_workspace) VALUES ('old','old','custom',1,'w1');
 INSERT INTO members (id,team_id,kind,name,role,origin,harness,profile,mode,folder,brief,herdr_agent)
 VALUES ('a','old','agent','alpha','custom','spec','pi','workspace','pane','/tmp','b','keep-this'),
 ('b','old','agent','abcdefghijklmnopqrstuvwxyz012345','custom','spec','pi','workspace','pane','/tmp','b',NULL);`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	for n := 0; n < 2; n++ {
		s, err := Open(ctx, path)
		if err != nil {
			t.Fatal(err)
		}
		err = s.Read(ctx, func(tx *Tx) error {
			a, e := tx.GetStoredMember("a")
			if e != nil {
				return e
			}
			b, e := tx.GetStoredMember("b")
			if e != nil {
				return e
			}
			if b.HerdrWorkspace == nil || *b.HerdrWorkspace != "w1" {
				t.Fatalf("never-launched member lost workspace: %+v", b)
			}
			if *a.HerdrAgent != "keep-this" || *a.HerdrWorkspace != "w1" || *b.HerdrAgent != herdr.AgentName("old", b.Member.Name) {
				t.Fatalf("migration changed names: %+v %+v", a, b)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		s.Close()
	}
}
