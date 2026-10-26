package store

import (
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
)

const memberFrom = ` FROM members m
 JOIN teams t ON t.id = m.team_id
 LEFT JOIN members spawner ON spawner.id = m.spawned_by
 LEFT JOIN worktrees w ON w.id = m.worktree_id
 LEFT JOIN leases l ON l.member_id = m.id
 LEFT JOIN holds h ON h.member_id = m.id `

const memberColumns = `m.id, m.team_id, m.name, m.kind, m.role, m.origin, m.position,
 m.spawned_by, m.spawned_run, m.harness, m.profile, m.mode, m.folder, m.worktree,
 m.worktree_id, m.brief, m.task, m.state, m.stop_reason, m.activity, m.last_driver,
 m.session_id, m.herdr_agent, m.created_at, m.updated_at, m.started_at, m.stopped_at,
 m.removed_at, m.removed_reason, spawner.name, w.path, w.branch, w.removed_at,
 l.driver, l.generation, l.launch_id, l.pane_id, h.held_until, h.released_at,
 t.herdr_server, t.herdr_workspace`

func scanMember(row scanner) (*StoredMember, error) {
	var result StoredMember
	m := &result.Member
	var kind, role, origin, harness, profile, mode, folder, state, stopReason sql.NullString
	var activity, lastDriver, session, herdrAgent, brief, task, worktreeID sql.NullString
	var spawnedBy, spawnerName, worktreePath, worktreeBranch, worktreeRemoved sql.NullString
	var leaseDriver, leaseLaunch, leasePane, heldUntil, holdReleased, herdrWorkspace sql.NullString
	var created, updated string
	var herdrServer string
	var started, stopped, removed sql.NullString
	var removedReason sql.NullString
	var spawnedRun, position, generation sql.NullInt64
	err := row.Scan(&m.ID, &m.Team, &m.Name, &kind, &role, &origin, &position,
		&spawnedBy, &spawnedRun, &harness, &profile, &mode, &folder, &result.Worktree,
		&worktreeID, &brief, &task, &state, &stopReason, &activity, &lastDriver,
		&session, &herdrAgent, &created, &updated, &started, &stopped, &removed,
		&removedReason, &spawnerName, &worktreePath, &worktreeBranch, &worktreeRemoved,
		&leaseDriver, &generation, &leaseLaunch, &leasePane, &heldUntil, &holdReleased,
		&herdrServer, &herdrWorkspace)
	if err != nil {
		return nil, err
	}
	m.Kind = model.MemberKind(kind.String)
	m.Origin = model.MemberOrigin(origin.String)
	m.State = model.MemberState(state.String)
	if role.Valid {
		v := model.Role(role.String)
		m.Role = &v
	}
	if harness.Valid {
		v := model.Harness(harness.String)
		m.Harness = &v
	}
	if profile.Valid {
		v := model.Profile(profile.String)
		m.Profile = &v
	}
	if mode.Valid {
		v := model.Mode(mode.String)
		m.Mode = &v
	}
	if stopReason.Valid {
		v := model.StopReason(stopReason.String)
		m.StopReason = &v
	}
	if activity.Valid {
		v := model.Activity(activity.String)
		m.Activity = &v
	}
	if lastDriver.Valid {
		v := model.Mode(lastDriver.String)
		m.LastDriver = &v
	}
	if removedReason.Valid {
		v := model.RemovedReason(removedReason.String)
		m.RemovedReason = &v
	}
	m.Folder = stringPtr(folder)
	m.Session = stringPtr(session)
	result.Position = nullInt64Ptr(position)
	result.SpawnedRun = nullInt64Ptr(spawnedRun)
	result.WorktreeID = stringPtr(worktreeID)
	result.Brief = stringPtr(brief)
	result.Task = stringPtr(task)
	if spawnedBy.Valid {
		m.SpawnedBy = &model.MemberRef{ID: spawnedBy.String, Name: spawnerName.String}
	}
	if worktreePath.Valid && !worktreeRemoved.Valid {
		m.Worktree = &model.Worktree{Path: worktreePath.String, Branch: worktreeBranch.String}
		m.Workdir = &worktreePath.String
	} else {
		m.Workdir = m.Folder
	}
	if leaseDriver.Valid {
		m.Lease = &model.Lease{Driver: model.Driver(leaseDriver.String), Generation: generation.Int64,
			Launch: stringPtr(leaseLaunch), Pane: stringPtr(leasePane)}
	}
	if heldUntil.Valid && !holdReleased.Valid {
		held, err := parseTime(heldUntil.String)
		if err != nil {
			return nil, err
		}
		if held.After(time.Now()) {
			m.HeldUntil = &held
		}
	}
	if m.Lease != nil && m.Lease.Driver == model.DriverPane {
		m.Herdr = &model.HerdrPlacement{Server: herdrServer, Workspace: stringPtr(herdrWorkspace), Agent: stringPtr(herdrAgent)}
	}
	if m.CreatedAt, err = parseTime(created); err != nil {
		return nil, err
	}
	if result.UpdatedAt, err = parseTime(updated); err != nil {
		return nil, err
	}
	if m.StartedAt, err = timeFromNull(started); err != nil {
		return nil, err
	}
	if m.StoppedAt, err = timeFromNull(stopped); err != nil {
		return nil, err
	}
	if m.RemovedAt, err = timeFromNull(removed); err != nil {
		return nil, err
	}
	m.Sessions = []model.MemberSession{}
	return &result, nil
}

func nullInt64Ptr(v sql.NullInt64) *int64 {
	if !v.Valid {
		return nil
	}
	n := v.Int64
	return &n
}

// InsertMember inserts an immutable member identity and its initial state.
func (tx *Tx) InsertMember(p MemberParams) error {
	if p.State == "" {
		p.State = model.MemberStateStopped
	}
	created := defaultTime(p.CreatedAt)
	updated := created
	if p.UpdatedAt != nil {
		updated = *p.UpdatedAt
	}
	_, err := tx.exec(`INSERT INTO members (
 id, team_id, kind, name, role, origin, position, spawned_by, spawned_run, harness,
 profile, mode, folder, worktree, worktree_id, brief, task, state, stop_reason,
 activity, last_driver, session_id, herdr_agent, created_at, updated_at, started_at,
 stopped_at, removed_at, removed_reason
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.ID, p.TeamID, p.Kind, p.Name, dbPtr(p.Role), p.Origin, dbPtr(p.Position),
		dbPtr(p.SpawnedBy), dbPtr(p.SpawnedRun), dbPtr(p.Harness), dbPtr(p.Profile),
		dbPtr(p.Mode), dbPtr(p.Folder), p.Worktree, dbPtr(p.WorktreeID), dbPtr(p.Brief),
		dbPtr(p.Task), p.State, dbPtr(p.StopReason), dbPtr(p.Activity), dbPtr(p.LastDriver),
		dbPtr(p.SessionID), dbPtr(p.HerdrAgent), dbTime(created), dbTime(updated),
		dbTimePtr(p.StartedAt), dbTimePtr(p.StoppedAt), dbTimePtr(p.RemovedAt), dbPtr(p.RemovedWhy))
	return err
}

// GetStoredMember returns a member and its store-only configuration.
func (tx *Tx) GetStoredMember(id string) (*StoredMember, error) {
	m, err := scanMember(tx.queryRow("SELECT "+memberColumns+memberFrom+"WHERE m.id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, unknownMember()
	}
	if err != nil {
		return nil, asError(err)
	}
	if err := tx.loadMemberExtras(m); err != nil {
		return nil, err
	}
	return m, nil
}

// GetMember returns the common.v1 member aggregate.
func (tx *Tx) GetMember(id string) (*model.Member, error) {
	m, err := tx.GetStoredMember(id)
	if err != nil {
		return nil, err
	}
	return &m.Member, nil
}

// GetLiveMember returns a live member by team and name.
func (tx *Tx) GetLiveMember(teamID, name string) (*model.Member, error) {
	m, err := scanMember(tx.queryRow("SELECT "+memberColumns+memberFrom+"WHERE m.team_id = ? AND m.name = ? AND m.removed_at IS NULL", teamID, name))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, unknownMember()
	}
	if err != nil {
		return nil, asError(err)
	}
	if err := tx.loadMemberExtras(m); err != nil {
		return nil, err
	}
	return &m.Member, nil
}

// ListMembers returns team members in insertion order, optionally including removed rows.
func (tx *Tx) ListMembers(teamID string, includeRemoved bool) ([]model.Member, error) {
	query := "SELECT " + memberColumns + memberFrom + "WHERE m.team_id = ?"
	if !includeRemoved {
		query += " AND m.removed_at IS NULL"
	}
	query += " ORDER BY m.created_at, m.id"
	rows, err := tx.query(query, teamID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var stored []*StoredMember
	for rows.Next() {
		m, err := scanMember(rows)
		if err != nil {
			return nil, asError(err)
		}
		stored = append(stored, m)
	}
	if err := rows.Err(); err != nil {
		return nil, asError(err)
	}
	members := make([]model.Member, 0, len(stored))
	for _, m := range stored {
		if err := tx.loadMemberExtras(m); err != nil {
			return nil, err
		}
		members = append(members, m.Member)
	}
	return members, nil
}

func (tx *Tx) loadMemberExtras(m *StoredMember) error {
	sessions, err := tx.ListMemberSessions(m.Member.ID)
	if err != nil {
		return err
	}
	for _, session := range sessions {
		m.Member.Sessions = append(m.Member.Sessions, model.MemberSession{ID: session.SessionID,
			Ordinal: session.Ordinal, Source: session.Source, Launch: session.LaunchID, FirstSeenAt: session.FirstSeenAt})
	}
	return nil
}

// UpdateMember applies a configuration patch.
func (tx *Tx) UpdateMember(id string, patch MemberPatch, at model.Time) error {
	var sets []string
	var args []any
	add := func(column string, value any) { sets = append(sets, column+" = ?"); args = append(args, value) }
	if patch.Name != nil {
		add("name", *patch.Name)
	}
	if patch.Role != nil {
		add("role", dbPtr(*patch.Role))
	}
	if patch.Position != nil {
		add("position", dbPtr(*patch.Position))
	}
	if patch.Harness != nil {
		add("harness", dbPtr(*patch.Harness))
	}
	if patch.Profile != nil {
		add("profile", dbPtr(*patch.Profile))
	}
	if patch.Mode != nil {
		add("mode", dbPtr(*patch.Mode))
	}
	if patch.Folder != nil {
		add("folder", dbPtr(*patch.Folder))
	}
	if patch.Worktree != nil {
		add("worktree", *patch.Worktree)
	}
	if patch.WorktreeID != nil {
		add("worktree_id", dbPtr(*patch.WorktreeID))
	}
	if patch.Brief != nil {
		add("brief", dbPtr(*patch.Brief))
	}
	if patch.Task != nil {
		add("task", dbPtr(*patch.Task))
	}
	if patch.LastDriver != nil {
		add("last_driver", dbPtr(*patch.LastDriver))
	}
	if patch.HerdrAgent != nil {
		add("herdr_agent", dbPtr(*patch.HerdrAgent))
	}
	if len(sets) == 0 {
		return nil
	}
	add("updated_at", dbTime(nowIfZero(at)))
	args = append(args, id)
	result, err := tx.exec("UPDATE members SET "+strings.Join(sets, ", ")+" WHERE id = ?", args...)
	if err != nil {
		return err
	}
	return requireRows(result, unknownMember())
}

// SetMemberState updates state and its lifecycle timestamps.
func (tx *Tx) SetMemberState(id string, state model.MemberState, reason *model.StopReason, startedAt, stoppedAt **model.Time, at model.Time) error {
	sets := []string{"state = ?", "stop_reason = ?"}
	args := []any{state, dbPtr(reason)}
	if startedAt != nil {
		sets = append(sets, "started_at = ?")
		args = append(args, dbTimePtr(*startedAt))
	}
	if stoppedAt != nil {
		sets = append(sets, "stopped_at = ?")
		args = append(args, dbTimePtr(*stoppedAt))
	}
	sets = append(sets, "updated_at = ?")
	args = append(args, dbTime(nowIfZero(at)), id)
	result, err := tx.exec("UPDATE members SET "+strings.Join(sets, ", ")+" WHERE id = ?", args...)
	if err != nil {
		return err
	}
	return requireRows(result, unknownMember())
}

// SetMemberActivity updates activity, including clearing it with nil.
func (tx *Tx) SetMemberActivity(id string, activity *model.Activity, at model.Time) error {
	result, err := tx.exec("UPDATE members SET activity = ?, updated_at = ? WHERE id = ?", dbPtr(activity), dbTime(nowIfZero(at)), id)
	if err != nil {
		return err
	}
	return requireRows(result, unknownMember())
}

// RemoveMember marks a live member removed without deleting its history.
func (tx *Tx) RemoveMember(id string, reason model.RemovedReason, at model.Time) error {
	at = nowIfZero(at)
	result, err := tx.exec("UPDATE members SET removed_at = ?, removed_reason = ?, updated_at = ? WHERE id = ? AND removed_at IS NULL", dbTime(at), reason, dbTime(at), id)
	if err != nil {
		return err
	}
	return requireRows(result, unknownMember())
}

// SetMemberSession changes the current session id; history is managed separately.
func (tx *Tx) SetMemberSession(id string, session *string, at model.Time) error {
	result, err := tx.exec("UPDATE members SET session_id = ?, updated_at = ? WHERE id = ?", dbPtr(session), dbTime(nowIfZero(at)), id)
	if err != nil {
		return err
	}
	return requireRows(result, unknownMember())
}
