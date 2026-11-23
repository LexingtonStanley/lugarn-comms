package store

import (
	"context"
	"database/sql"
	"github.com/LexingtonStanley/lugarn-comms/internal/herdr"
)

// The old name algorithm needs SHA-256, which SQLite does not provide. Run it
// before committing migration 2, including members never launched in old teams.
func migrateHerdrNames(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, "SELECT id, team_id, name FROM members WHERE kind = 'agent' AND herdr_agent IS NULL")
	if err != nil {
		return err
	}
	type item struct{ id, team, name string }
	var items []item
	for rows.Next() {
		var v item
		if err = rows.Scan(&v.id, &v.team, &v.name); err != nil {
			rows.Close()
			return err
		}
		items = append(items, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, v := range items {
		if _, err = tx.ExecContext(ctx, "UPDATE members SET herdr_agent = ? WHERE id = ?", herdr.AgentName(v.team, v.name), v.id); err != nil {
			return err
		}
	}
	// Names must exist before workspace ownership is backfilled, including
	// members that had never launched under migration 1.
	_, err = tx.ExecContext(ctx, `UPDATE members SET herdr_workspace = (SELECT herdr_workspace FROM teams WHERE id = members.team_id) WHERE herdr_agent IS NOT NULL`)
	return err
}

// ReservedHerdrNames includes stopped members: a recorded name is permanent.
func (tx *Tx) ReservedHerdrNames(server string) (map[string]bool, error) {
	rows, err := tx.query(`SELECT m.herdr_agent FROM members m JOIN teams t ON t.id = m.team_id WHERE t.herdr_server = ? AND t.backend = 'herdr' AND m.herdr_agent IS NOT NULL AND m.removed_at IS NULL AND t.forgotten = 0`, server)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	names := map[string]bool{}
	for rows.Next() {
		var name string
		if err = rows.Scan(&name); err != nil {
			return nil, err
		}
		names[name] = true
	}
	return names, rows.Err()
}
