package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
)

func TestHCMigrationPreservesLegacyBackend(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "v2.db")
	db, e := sql.Open("sqlite", path)
	if e != nil {
		t.Fatal(e)
	}
	for _, name := range []string{"0001_init", "0002_herdr_identity"} {
		raw, e := migrationFiles.ReadFile("migrations/" + name + ".sql")
		if e != nil {
			t.Fatal(e)
		}
		if _, e = db.Exec(string(raw)); e != nil {
			t.Fatal(e)
		}
	}
	if _, e = db.Exec(`INSERT INTO teams(id,name,pattern,spec_version,herdr_workspace) VALUES('old','old','custom',1,'workspace-old')`); e != nil {
		t.Fatal(e)
	}
	db.Close()
	s, e := Open(ctx, path)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	if e = s.Read(ctx, func(tx *Tx) error {
		team, e := tx.GetTeam("old")
		if e != nil {
			return e
		}
		if team.BackendChoice != model.HerdrBackendChoiceHerdr || team.Backend == nil || *team.Backend != model.HerdrBackendHerdr || team.HCCoreID != nil || *team.HerdrWorkspace != "workspace-old" {
			t.Fatalf("migration changed legacy team: %+v", team)
		}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	if e = s.Write(ctx, func(tx *Tx) error {
		return tx.CreateTeam(Team{ID: "auto", Name: "auto", Pattern: model.PatternCustom, BackendChoice: model.HerdrBackendChoiceAuto})
	}); e != nil {
		t.Fatal(e)
	}
	if e = s.Read(ctx, func(tx *Tx) error {
		team, e := tx.GetTeam("auto")
		if e == nil && team.Backend != nil {
			t.Fatal("unstarted auto team already resolved")
		}
		return e
	}); e != nil {
		t.Fatal(e)
	}
}
