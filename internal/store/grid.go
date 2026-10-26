package store

import (
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
)

// ReplaceGrid replaces all of a team's capabilities atomically within the transaction.
func (tx *Tx) ReplaceGrid(teamID string, edges []GridEdge) error {
	if _, err := tx.exec("DELETE FROM grid WHERE team_id = ?", teamID); err != nil {
		return err
	}
	return tx.AddGridEdges(teamID, edges)
}

// AddGridEdges inserts capabilities without removing existing edges.
func (tx *Tx) AddGridEdges(teamID string, edges []GridEdge) error {
	for _, edge := range edges {
		for _, capability := range edge.Can {
			_, err := tx.exec("INSERT INTO grid (team_id, from_id, to_id, capability) VALUES (?, ?, ?, ?)", teamID, edge.FromID, edge.ToID, capability)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// ListGrid returns all grid capabilities ordered by pair and capability.
func (tx *Tx) ListGrid(teamID string) ([]GridRow, error) {
	rows, err := tx.query("SELECT team_id, from_id, to_id, capability, created_at FROM grid WHERE team_id = ? ORDER BY from_id, to_id, capability", teamID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []GridRow
	for rows.Next() {
		var row GridRow
		var created string
		if err := rows.Scan(&row.TeamID, &row.FromID, &row.ToID, &row.Capability, &created); err != nil {
			return nil, asError(err)
		}
		if row.CreatedAt, err = parseTime(created); err != nil {
			return nil, err
		}
		result = append(result, row)
	}
	return result, asError(rows.Err())
}

// GetGridEdge returns one pair's capabilities, or nil when no capability exists.
func (tx *Tx) GetGridEdge(fromID, toID string) (*GridEdge, error) {
	rows, err := tx.query("SELECT team_id, capability FROM grid WHERE from_id = ? AND to_id = ? ORDER BY capability", fromID, toID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result *GridEdge
	for rows.Next() {
		var teamID string
		var capability model.GridCapability
		if err := rows.Scan(&teamID, &capability); err != nil {
			return nil, asError(err)
		}
		if result == nil {
			result = &GridEdge{TeamID: teamID, FromID: fromID, ToID: toID}
		}
		result.Can = append(result.Can, capability)
	}
	if err := rows.Err(); err != nil {
		return nil, asError(err)
	}
	return result, nil
}

// InsertWorktree records a broker-created worktree.
func (tx *Tx) InsertWorktree(w WorktreeRecord) error {
	w.CreatedAt = nowIfZero(w.CreatedAt)
	_, err := tx.exec(`INSERT INTO worktrees (id, team_id, member_id, repo, path, branch, base, created_at, removed_at)
 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, w.ID, w.TeamID, w.MemberID, w.Repo, w.Path, w.Branch, w.Base, dbTime(w.CreatedAt), dbTimePtr(w.RemovedAt))
	return err
}

func scanWorktree(row scanner) (*WorktreeRecord, error) {
	var w WorktreeRecord
	var created string
	var removed sql.NullString
	if err := row.Scan(&w.ID, &w.TeamID, &w.MemberID, &w.Repo, &w.Path, &w.Branch, &w.Base, &created, &removed); err != nil {
		return nil, err
	}
	var err error
	if w.CreatedAt, err = parseTime(created); err != nil {
		return nil, err
	}
	if w.RemovedAt, err = timeFromNull(removed); err != nil {
		return nil, err
	}
	return &w, nil
}

const worktreeColumns = "id, team_id, member_id, repo, path, branch, base, created_at, removed_at"

// GetWorktree returns a worktree by id.
func (tx *Tx) GetWorktree(id string) (*WorktreeRecord, error) {
	w, err := scanWorktree(tx.queryRow("SELECT "+worktreeColumns+" FROM worktrees WHERE id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, notFound("worktree")
	}
	return w, asError(err)
}

// GetWorktreeByPath returns a worktree by its unique live path, including its removal history.
func (tx *Tx) GetWorktreeByPath(path string) (*WorktreeRecord, error) {
	w, err := scanWorktree(tx.queryRow("SELECT "+worktreeColumns+" FROM worktrees WHERE path = ? ORDER BY (removed_at IS NULL) DESC, created_at DESC, rowid DESC LIMIT 1", path))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, notFound("worktree")
	}
	return w, asError(err)
}

// ListWorktrees lists broker-created worktrees, optionally including removed rows.
func (tx *Tx) ListWorktrees(teamID string, includeRemoved bool) ([]WorktreeRecord, error) {
	query := "SELECT " + worktreeColumns + " FROM worktrees WHERE team_id = ?"
	if !includeRemoved {
		query += " AND removed_at IS NULL"
	}
	query += " ORDER BY created_at, id"
	rows, err := tx.query(query, teamID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []WorktreeRecord
	for rows.Next() {
		w, err := scanWorktree(rows)
		if err != nil {
			return nil, asError(err)
		}
		result = append(result, *w)
	}
	return result, asError(rows.Err())
}

// RemoveWorktree marks a live worktree removed.
func (tx *Tx) RemoveWorktree(id string, at model.Time) error {
	result, err := tx.exec("UPDATE worktrees SET removed_at = ? WHERE id = ? AND removed_at IS NULL", dbTime(nowIfZero(at)), id)
	if err != nil {
		return err
	}
	return requireRows(result, notFound("worktree"))
}

// PutHold creates or renews a member's single hold.
func (tx *Tx) PutHold(h Hold) error {
	h.Since = nowIfZero(h.Since)
	_, err := tx.exec(`INSERT INTO holds (member_id, team_id, since, held_until, released_at)
 VALUES (?, ?, ?, ?, ?) ON CONFLICT(member_id) DO UPDATE SET team_id = excluded.team_id,
 held_until = excluded.held_until, released_at = excluded.released_at`, h.MemberID, h.TeamID, dbTime(h.Since), dbTime(h.HeldUntil), dbTimePtr(h.ReleasedAt))
	return err
}

// GetHold returns a member's hold, including a released or elapsed hold.
func (tx *Tx) GetHold(memberID string) (*Hold, error) {
	var h Hold
	var since, until string
	var released sql.NullString
	err := tx.queryRow("SELECT member_id, team_id, since, held_until, released_at FROM holds WHERE member_id = ?", memberID).Scan(&h.MemberID, &h.TeamID, &since, &until, &released)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, notFound("hold")
	}
	if err != nil {
		return nil, asError(err)
	}
	if h.Since, err = parseTime(since); err != nil {
		return nil, err
	}
	if h.HeldUntil, err = parseTime(until); err != nil {
		return nil, err
	}
	if h.ReleasedAt, err = timeFromNull(released); err != nil {
		return nil, err
	}
	return &h, nil
}

// ReleaseHold releases a hold at at.
func (tx *Tx) ReleaseHold(memberID string, at model.Time) error {
	result, err := tx.exec("UPDATE holds SET released_at = ? WHERE member_id = ? AND released_at IS NULL", dbTime(nowIfZero(at)), memberID)
	if err != nil {
		return err
	}
	return requireRows(result, notFound("hold"))
}

// PutSpawnSettings inserts or replaces a member's fully defaulted spawn settings.
func (tx *Tx) PutSpawnSettings(s SpawnSettings) error {
	var harnesses any
	if s.Harnesses != nil {
		encoded, err := json.Marshal(*s.Harnesses)
		if err != nil {
			return err
		}
		harnesses = string(encoded)
	}
	_, err := tx.exec(`INSERT INTO spawn_settings (member_id, enabled, running_at_once, per_run, harnesses, mode, folder, max_profile, brief)
 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT(member_id) DO UPDATE SET enabled = excluded.enabled,
 running_at_once = excluded.running_at_once, per_run = excluded.per_run, harnesses = excluded.harnesses,
 mode = excluded.mode, folder = excluded.folder, max_profile = excluded.max_profile, brief = excluded.brief`,
		s.MemberID, s.Enabled, s.RunningAtOnce, s.PerRun, harnesses, s.Mode, s.Folder, s.MaxProfile, dbPtr(s.Brief))
	return err
}

// GetSpawnSettings returns a member's settings.
func (tx *Tx) GetSpawnSettings(memberID string) (*SpawnSettings, error) {
	var s SpawnSettings
	var harnesses, brief sql.NullString
	err := tx.queryRow("SELECT member_id, enabled, running_at_once, per_run, harnesses, mode, folder, max_profile, brief FROM spawn_settings WHERE member_id = ?", memberID).Scan(
		&s.MemberID, &s.Enabled, &s.RunningAtOnce, &s.PerRun, &harnesses, &s.Mode, &s.Folder, &s.MaxProfile, &brief)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, notFound("spawn settings")
	}
	if err != nil {
		return nil, asError(err)
	}
	if harnesses.Valid {
		var decoded []model.Harness
		if err := json.Unmarshal([]byte(harnesses.String), &decoded); err != nil {
			return nil, err
		}
		s.Harnesses = &decoded
	}
	s.Brief = stringPtr(brief)
	return &s, nil
}

// DeleteSpawnSettings removes settings when a spec no longer supplies them.
func (tx *Tx) DeleteSpawnSettings(memberID string) error {
	_, err := tx.exec("DELETE FROM spawn_settings WHERE member_id = ?", memberID)
	return err
}

// GetSpawnCount reads the derived counters for a spawner.
func (tx *Tx) GetSpawnCount(spawnerID string) (*SpawnCount, error) {
	var count SpawnCount
	err := tx.queryRow("SELECT spawner_id, team_id, run, running, this_run FROM spawn_counts WHERE spawner_id = ?", spawnerID).Scan(&count.SpawnerID, &count.TeamID, &count.Run, &count.Running, &count.ThisRun)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, notFound("spawn settings")
	}
	return &count, asError(err)
}

// ListSpawnCounts reads all derived spawn counters.
func (tx *Tx) ListSpawnCounts(teamID string) ([]SpawnCount, error) {
	rows, err := tx.query("SELECT spawner_id, team_id, run, running, this_run FROM spawn_counts WHERE team_id = ? ORDER BY spawner_id", teamID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []SpawnCount
	for rows.Next() {
		var count SpawnCount
		if err := rows.Scan(&count.SpawnerID, &count.TeamID, &count.Run, &count.Running, &count.ThisRun); err != nil {
			return nil, asError(err)
		}
		result = append(result, count)
	}
	return result, asError(rows.Err())
}
