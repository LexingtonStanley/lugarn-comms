package store

import (
	"encoding/json"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
)

// AppendEvent appends an immutable event and returns its sequence.
func (tx *Tx) AppendEvent(event Event) (int64, error) {
	event.At = nowIfZero(event.At)
	if len(event.Data) == 0 {
		event.Data = json.RawMessage(`{}`)
	}
	result, err := tx.exec(`INSERT INTO events (team_id, type, at, timeline, member_id, message_id, data)
 VALUES (?, ?, ?, ?, ?, ?, ?)`, dbPtr(event.TeamID), event.Type, dbTime(event.At), event.Timeline,
		dbPtr(event.MemberID), dbPtr(event.MessageID), string(event.Data))
	if err != nil {
		return 0, err
	}
	seq, err := result.LastInsertId()
	return seq, asError(err)
}

func scanEvent(row scanner) (*Event, error) {
	var event Event
	var at, data string
	if err := row.Scan(&event.Seq, &event.TeamID, &event.Type, &at, &event.Timeline, &event.MemberID, &event.MessageID, &data); err != nil {
		return nil, err
	}
	var err error
	if event.At, err = parseTime(at); err != nil {
		return nil, err
	}
	event.Data = json.RawMessage(data)
	return &event, nil
}

const eventColumns = "seq, team_id, type, at, timeline, member_id, message_id, data"

// CurrentEventSeq returns the newest event sequence, or zero for an empty log.
func (tx *Tx) CurrentEventSeq() (int64, error) {
	var seq int64
	err := tx.queryRow("SELECT COALESCE(MAX(seq), 0) FROM events").Scan(&seq)
	return seq, asError(err)
}

// ReplayEvents returns ascending events after after. A nil teamID includes all teams
// and host-level events. A non-positive limit means no explicit limit.
func (tx *Tx) ReplayEvents(after int64, teamID *string, limit int) ([]Event, error) {
	query := "SELECT " + eventColumns + " FROM events WHERE seq > ?"
	args := []any{after}
	if teamID != nil {
		query += " AND team_id = ?"
		args = append(args, *teamID)
	}
	query += " ORDER BY seq"
	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}
	return tx.queryEvents(query, args...)
}

func (tx *Tx) queryEvents(query string, args ...any) ([]Event, error) {
	rows, err := tx.query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []Event
	for rows.Next() {
		event, err := scanEvent(rows)
		if err != nil {
			return nil, asError(err)
		}
		result = append(result, *event)
	}
	return result, asError(rows.Err())
}

// Timeline pages timeline events and always returns them in ascending order. At
// most one of before or after may be nonzero.
func (tx *Tx) Timeline(teamID string, before, after int64, limit int) (*TimelinePage, error) {
	if before != 0 && after != 0 {
		return nil, model.NewError(model.ErrorInvalidRequest, "timeline accepts only one of before and after")
	}
	if limit == 0 {
		limit = 50
	}
	if limit < 1 || limit > 200 {
		return nil, model.NewError(model.ErrorInvalidRequest, "timeline limit must be between 1 and 200")
	}
	page := &TimelinePage{}
	var events []Event
	var err error
	if after != 0 {
		events, err = tx.queryEvents("SELECT "+eventColumns+" FROM events WHERE team_id = ? AND timeline = 1 AND seq > ? ORDER BY seq LIMIT ?", teamID, after, limit+1)
		if err != nil {
			return nil, err
		}
		page.HasMoreAfter = len(events) > limit
		if page.HasMoreAfter {
			events = events[:limit]
		}
	} else {
		query := "SELECT " + eventColumns + " FROM events WHERE team_id = ? AND timeline = 1"
		args := []any{teamID}
		if before != 0 {
			query += " AND seq < ?"
			args = append(args, before)
		}
		query += " ORDER BY seq DESC LIMIT ?"
		args = append(args, limit+1)
		events, err = tx.queryEvents(query, args...)
		if err != nil {
			return nil, err
		}
		page.HasMoreBefore = len(events) > limit
		if page.HasMoreBefore {
			events = events[:limit]
		}
		for left, right := 0, len(events)-1; left < right; left, right = left+1, right-1 {
			events[left], events[right] = events[right], events[left]
		}
	}
	if len(events) > 0 {
		if err := tx.queryRow("SELECT EXISTS(SELECT 1 FROM events WHERE team_id = ? AND timeline = 1 AND seq < ?)", teamID, events[0].Seq).Scan(&page.HasMoreBefore); err != nil {
			return nil, asError(err)
		}
		if err := tx.queryRow("SELECT EXISTS(SELECT 1 FROM events WHERE team_id = ? AND timeline = 1 AND seq > ?)", teamID, events[len(events)-1].Seq).Scan(&page.HasMoreAfter); err != nil {
			return nil, asError(err)
		}
	}
	page.Events = events
	return page, nil
}

// GetEvent returns one event by sequence.
func (tx *Tx) GetEvent(seq int64) (*Event, error) {
	events, err := tx.queryEvents("SELECT "+eventColumns+" FROM events WHERE seq = ?", seq)
	if err != nil {
		return nil, err
	}
	if len(events) == 0 {
		return nil, notFound("event")
	}
	return &events[0], nil
}
