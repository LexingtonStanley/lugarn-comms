package store

import (
	"database/sql"
	"errors"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
)

// InsertLaunch records a launch.
func (tx *Tx) InsertLaunch(l Launch) error {
	l.StartedAt = nowIfZero(l.StartedAt)
	_, err := tx.exec(`INSERT INTO launches (id, member_id, generation, driver, session_id_start, pane_id, started_at, ended_at, end_reason)
 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, l.ID, l.MemberID, l.Generation, l.Driver, dbPtr(l.SessionIDStart), dbPtr(l.PaneID), dbTime(l.StartedAt), dbTimePtr(l.EndedAt), dbPtr(l.EndReason))
	return err
}

func scanLaunch(row scanner) (*Launch, error) {
	var l Launch
	var started string
	var ended sql.NullString
	if err := row.Scan(&l.ID, &l.MemberID, &l.Generation, &l.Driver, &l.SessionIDStart, &l.PaneID, &started, &ended, &l.EndReason); err != nil {
		return nil, err
	}
	var err error
	if l.StartedAt, err = parseTime(started); err != nil {
		return nil, err
	}
	if l.EndedAt, err = timeFromNull(ended); err != nil {
		return nil, err
	}
	return &l, nil
}

const launchColumns = "id, member_id, generation, driver, session_id_start, pane_id, started_at, ended_at, end_reason"

// GetLaunch returns a launch by id.
func (tx *Tx) GetLaunch(id string) (*Launch, error) {
	l, err := scanLaunch(tx.queryRow("SELECT "+launchColumns+" FROM launches WHERE id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, notFound("launch")
	}
	return l, asError(err)
}

// ListLaunches lists launches for a team, oldest first. teamID may be empty for all teams.
func (tx *Tx) ListLaunches(teamID string) ([]Launch, error) {
	query := "SELECT l.id, l.member_id, l.generation, l.driver, l.session_id_start, l.pane_id, l.started_at, l.ended_at, l.end_reason FROM launches l"
	var args []any
	if teamID != "" {
		query += " JOIN members m ON m.id = l.member_id WHERE m.team_id = ?"
		args = append(args, teamID)
	}
	query += " ORDER BY l.started_at, l.id"
	rows, err := tx.query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []Launch
	for rows.Next() {
		l, err := scanLaunch(rows)
		if err != nil {
			return nil, asError(err)
		}
		result = append(result, *l)
	}
	return result, asError(rows.Err())
}

// EndLaunch ends a live launch and refuses if it was already ended.
func (tx *Tx) EndLaunch(id string, reason model.LaunchEndReason, at model.Time) error {
	result, err := tx.exec("UPDATE launches SET ended_at = ?, end_reason = ? WHERE id = ? AND ended_at IS NULL", dbTime(nowIfZero(at)), reason, id)
	if err != nil {
		return err
	}
	return requireRows(result, conflict("launch is already ended or unknown"))
}

// InsertLease creates a member's generation-zero or initial lease.
func (tx *Tx) InsertLease(memberID string, lease model.Lease, at model.Time) error {
	_, err := tx.exec("INSERT INTO leases (member_id, driver, generation, launch_id, pane_id, updated_at) VALUES (?, ?, ?, ?, ?, ?)",
		memberID, lease.Driver, lease.Generation, dbPtr(lease.Launch), dbPtr(lease.Pane), dbTime(nowIfZero(at)))
	return err
}

// GetLease returns a member's lease.
func (tx *Tx) GetLease(memberID string) (*LeaseRecord, error) {
	var record LeaseRecord
	var updated string
	err := tx.queryRow("SELECT member_id, driver, generation, launch_id, pane_id, updated_at FROM leases WHERE member_id = ?", memberID).Scan(&record.MemberID, &record.Lease.Driver, &record.Lease.Generation, &record.Lease.Launch, &record.Lease.Pane, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, notFound("lease")
	}
	if err != nil {
		return nil, asError(err)
	}
	if record.UpdatedAt, err = parseTime(updated); err != nil {
		return nil, err
	}
	return &record, nil
}

// ListLeases lists all leases in a team.
func (tx *Tx) ListLeases(teamID string) ([]LeaseRecord, error) {
	rows, err := tx.query(`SELECT l.member_id, l.driver, l.generation, l.launch_id, l.pane_id, l.updated_at
 FROM leases l JOIN members m ON m.id = l.member_id WHERE m.team_id = ? ORDER BY l.member_id`, teamID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []LeaseRecord
	for rows.Next() {
		var record LeaseRecord
		var updated string
		if err := rows.Scan(&record.MemberID, &record.Lease.Driver, &record.Lease.Generation, &record.Lease.Launch, &record.Lease.Pane, &updated); err != nil {
			return nil, asError(err)
		}
		if record.UpdatedAt, err = parseTime(updated); err != nil {
			return nil, err
		}
		result = append(result, record)
	}
	return result, asError(rows.Err())
}

// CompareAndSwapLease updates only when both driver and generation match.
func (tx *Tx) CompareAndSwapLease(memberID string, expectedDriver model.Driver, expectedGeneration int64, next model.Lease, at model.Time) error {
	result, err := tx.exec(`UPDATE leases SET driver = ?, generation = ?, launch_id = ?, pane_id = ?, updated_at = ?
 WHERE member_id = ? AND driver = ? AND generation = ?`, next.Driver, next.Generation, dbPtr(next.Launch), dbPtr(next.Pane), dbTime(nowIfZero(at)), memberID, expectedDriver, expectedGeneration)
	if err != nil {
		return err
	}
	if n, err := result.RowsAffected(); err != nil {
		return asError(err)
	} else if n == 0 {
		actual, getErr := tx.GetLease(memberID)
		if getErr != nil {
			return getErr
		}
		return conflict("lease driver or generation changed").WithDetails(map[string]any{"driver": actual.Lease.Driver, "generation": actual.Lease.Generation})
	}
	return nil
}

// InsertToken stores an active token hash.
func (tx *Tx) InsertToken(token Token) error {
	token.CreatedAt = nowIfZero(token.CreatedAt)
	_, err := tx.exec("INSERT INTO tokens (launch_id, member_id, hash, created_at, revoked_at) VALUES (?, ?, ?, ?, ?)", token.LaunchID, token.MemberID, token.Hash, dbTime(token.CreatedAt), dbTimePtr(token.RevokedAt))
	return err
}

// GetTokenByHash looks up a hash whether active or revoked.
func (tx *Tx) GetTokenByHash(hash string) (*Token, error) {
	var token Token
	var created string
	var revoked sql.NullString
	err := tx.queryRow("SELECT launch_id, member_id, hash, created_at, revoked_at FROM tokens WHERE hash = ?", hash).Scan(&token.LaunchID, &token.MemberID, &token.Hash, &created, &revoked)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, notFound("token")
	}
	if err != nil {
		return nil, asError(err)
	}
	if token.CreatedAt, err = parseTime(created); err != nil {
		return nil, err
	}
	if token.RevokedAt, err = timeFromNull(revoked); err != nil {
		return nil, err
	}
	return &token, nil
}

// RevokeToken revokes a launch's active token.
func (tx *Tx) RevokeToken(launchID string, at model.Time) error {
	result, err := tx.exec("UPDATE tokens SET revoked_at = ? WHERE launch_id = ? AND revoked_at IS NULL", dbTime(nowIfZero(at)), launchID)
	if err != nil {
		return err
	}
	return requireRows(result, notFound("active token"))
}

// RevokeMemberTokens revokes every active token for a member.
func (tx *Tx) RevokeMemberTokens(memberID string, at model.Time) (int64, error) {
	result, err := tx.exec("UPDATE tokens SET revoked_at = ? WHERE member_id = ? AND revoked_at IS NULL", dbTime(nowIfZero(at)), memberID)
	if err != nil {
		return 0, err
	}
	n, err := result.RowsAffected()
	return n, asError(err)
}

// InsertMemberSession appends one session id and its secret seal.
func (tx *Tx) InsertMemberSession(session MemberSessionRecord) error {
	session.FirstSeenAt = nowIfZero(session.FirstSeenAt)
	_, err := tx.exec(`INSERT INTO member_sessions (member_id, ordinal, session_id, harness, source, launch_id, seal, briefed_at, first_seen_at)
 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, session.MemberID, session.Ordinal, session.SessionID, session.Harness, session.Source, dbPtr(session.LaunchID), session.Seal, dbTimePtr(session.BriefedAt), dbTime(session.FirstSeenAt))
	return err
}

// ListMemberSessions returns session history in ordinal order, including seals.
func (tx *Tx) ListMemberSessions(memberID string) ([]MemberSessionRecord, error) {
	rows, err := tx.query("SELECT member_id, ordinal, session_id, harness, source, launch_id, seal, briefed_at, first_seen_at FROM member_sessions WHERE member_id = ? ORDER BY ordinal", memberID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []MemberSessionRecord
	for rows.Next() {
		var session MemberSessionRecord
		var first string
		var briefed sql.NullString
		if err := rows.Scan(&session.MemberID, &session.Ordinal, &session.SessionID, &session.Harness, &session.Source, &session.LaunchID, &session.Seal, &briefed, &first); err != nil {
			return nil, asError(err)
		}
		if session.FirstSeenAt, err = parseTime(first); err != nil {
			return nil, err
		}
		if session.BriefedAt, err = timeFromNull(briefed); err != nil {
			return nil, err
		}
		result = append(result, session)
	}
	return result, asError(rows.Err())
}

// GetMemberSession returns one session by member and session id.
func (tx *Tx) GetMemberSession(memberID, sessionID string) (*MemberSessionRecord, error) {
	var session MemberSessionRecord
	var first string
	var briefed sql.NullString
	err := tx.queryRow("SELECT member_id, ordinal, session_id, harness, source, launch_id, seal, briefed_at, first_seen_at FROM member_sessions WHERE member_id = ? AND session_id = ?", memberID, sessionID).Scan(&session.MemberID, &session.Ordinal, &session.SessionID, &session.Harness, &session.Source, &session.LaunchID, &session.Seal, &briefed, &first)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, notFound("member session")
	}
	if err != nil {
		return nil, asError(err)
	}
	if session.FirstSeenAt, err = parseTime(first); err != nil {
		return nil, err
	}
	if session.BriefedAt, err = timeFromNull(briefed); err != nil {
		return nil, err
	}
	return &session, nil
}

// MarkSessionBriefed records that a session received its preamble and brief.
func (tx *Tx) MarkSessionBriefed(memberID string, ordinal int64, at model.Time) error {
	result, err := tx.exec("UPDATE member_sessions SET briefed_at = ? WHERE member_id = ? AND ordinal = ?", dbTime(nowIfZero(at)), memberID, ordinal)
	if err != nil {
		return err
	}
	return requireRows(result, notFound("member session"))
}
