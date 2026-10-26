package store

import (
	"database/sql"
	"errors"
)

// PutHopCounter inserts or replaces a team's hop counter.
func (tx *Tx) PutHopCounter(counter HopCounter) error {
	counter.UpdatedAt = nowIfZero(counter.UpdatedAt)
	_, err := tx.exec(`INSERT INTO hop_counters (team_id, used, allowance, exhausted_at, updated_at)
 VALUES (?, ?, ?, ?, ?) ON CONFLICT(team_id) DO UPDATE SET used = excluded.used,
 allowance = excluded.allowance, exhausted_at = excluded.exhausted_at, updated_at = excluded.updated_at`,
		counter.TeamID, counter.Used, counter.Allowance, dbTimePtr(counter.ExhaustedAt), dbTime(counter.UpdatedAt))
	return err
}

// GetHopCounter reads a team's hop counter.
func (tx *Tx) GetHopCounter(teamID string) (*HopCounter, error) {
	var counter HopCounter
	var exhausted sql.NullString
	var updated string
	err := tx.queryRow("SELECT team_id, used, allowance, exhausted_at, updated_at FROM hop_counters WHERE team_id = ?", teamID).Scan(&counter.TeamID, &counter.Used, &counter.Allowance, &exhausted, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, notFound("hop counter")
	}
	if err != nil {
		return nil, asError(err)
	}
	if counter.UpdatedAt, err = parseTime(updated); err != nil {
		return nil, err
	}
	if counter.ExhaustedAt, err = timeFromNull(exhausted); err != nil {
		return nil, err
	}
	return &counter, nil
}

// DeleteHopCounter removes a team's counter (used when resetting run state).
func (tx *Tx) DeleteHopCounter(teamID string) error {
	_, err := tx.exec("DELETE FROM hop_counters WHERE team_id = ?", teamID)
	return err
}

// PutRoundCounter inserts or replaces one writer/reviewer counter.
func (tx *Tx) PutRoundCounter(counter RoundCounter) error {
	counter.UpdatedAt = nowIfZero(counter.UpdatedAt)
	_, err := tx.exec(`INSERT INTO round_counters (team_id, writer_id, reviewer_id, used, allowance, exhausted_at, updated_at)
 VALUES (?, ?, ?, ?, ?, ?, ?) ON CONFLICT(writer_id, reviewer_id) DO UPDATE SET team_id = excluded.team_id,
 used = excluded.used, allowance = excluded.allowance, exhausted_at = excluded.exhausted_at, updated_at = excluded.updated_at`,
		counter.TeamID, counter.WriterID, counter.ReviewerID, counter.Used, counter.Allowance, dbTimePtr(counter.ExhaustedAt), dbTime(counter.UpdatedAt))
	return err
}

// GetRoundCounter reads one writer/reviewer counter.
func (tx *Tx) GetRoundCounter(writerID, reviewerID string) (*RoundCounter, error) {
	var counter RoundCounter
	var exhausted sql.NullString
	var updated string
	err := tx.queryRow("SELECT team_id, writer_id, reviewer_id, used, allowance, exhausted_at, updated_at FROM round_counters WHERE writer_id = ? AND reviewer_id = ?", writerID, reviewerID).Scan(
		&counter.TeamID, &counter.WriterID, &counter.ReviewerID, &counter.Used, &counter.Allowance, &exhausted, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, notFound("round counter")
	}
	if err != nil {
		return nil, asError(err)
	}
	if counter.ExhaustedAt, err = timeFromNull(exhausted); err != nil {
		return nil, err
	}
	if counter.UpdatedAt, err = parseTime(updated); err != nil {
		return nil, err
	}
	return &counter, nil
}

// ListRoundCounters lists all pairs in a team.
func (tx *Tx) ListRoundCounters(teamID string) ([]RoundCounter, error) {
	rows, err := tx.query("SELECT team_id, writer_id, reviewer_id, used, allowance, exhausted_at, updated_at FROM round_counters WHERE team_id = ? ORDER BY writer_id, reviewer_id", teamID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []RoundCounter
	for rows.Next() {
		var counter RoundCounter
		var exhausted sql.NullString
		var updated string
		if err := rows.Scan(&counter.TeamID, &counter.WriterID, &counter.ReviewerID, &counter.Used, &counter.Allowance, &exhausted, &updated); err != nil {
			return nil, asError(err)
		}
		if counter.ExhaustedAt, err = timeFromNull(exhausted); err != nil {
			return nil, err
		}
		if counter.UpdatedAt, err = parseTime(updated); err != nil {
			return nil, err
		}
		result = append(result, counter)
	}
	return result, asError(rows.Err())
}

// DeleteRoundCounters removes all review counters for a team on a fresh run.
func (tx *Tx) DeleteRoundCounters(teamID string) error {
	_, err := tx.exec("DELETE FROM round_counters WHERE team_id = ?", teamID)
	return err
}
