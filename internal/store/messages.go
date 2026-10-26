package store

import (
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
)

// InsertMessage inserts a message and returns its gap-free store sequence. Deliveries
// are inserted separately in the same transaction.
func (tx *Tx) InsertMessage(p MessageParams) (int64, error) {
	m := &p.Message
	m.CreatedAt = nowIfZero(m.CreatedAt)
	result, err := tx.exec(`INSERT INTO messages (id, team_id, run, sender_id, broadcast, kind, reply_to, text, hop, round, turn_id, created_at)
 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, m.ID, m.Team, p.Run, p.SenderID, m.Broadcast, m.Kind,
		dbPtr(m.ReplyTo), m.Text, dbPtr(m.Hop), dbPtr(m.Round), dbPtr(m.Turn), dbTime(m.CreatedAt))
	if err != nil {
		return 0, err
	}
	seq, err := result.LastInsertId()
	return seq, asError(err)
}

const messageFrom = " FROM messages m JOIN members sender ON sender.id = m.sender_id "
const messageColumns = `m.seq, m.id, m.team_id, m.sender_id, sender.name, m.broadcast, m.kind,
 m.reply_to, m.text, m.hop, m.round, m.turn_id, m.created_at`

func scanMessage(row scanner) (*model.Message, int64, error) {
	var m model.Message
	var seq int64
	var created string
	err := row.Scan(&seq, &m.ID, &m.Team, &m.From.ID, &m.From.Name, &m.Broadcast, &m.Kind,
		&m.ReplyTo, &m.Text, &m.Hop, &m.Round, &m.Turn, &created)
	if err != nil {
		return nil, 0, err
	}
	if m.CreatedAt, err = parseTime(created); err != nil {
		return nil, 0, err
	}
	m.Deliveries = []model.Delivery{}
	return &m, seq, nil
}

// GetMessage returns a message and all current deliveries.
func (tx *Tx) GetMessage(id string) (*model.Message, error) {
	m, _, err := scanMessage(tx.queryRow("SELECT "+messageColumns+messageFrom+"WHERE m.id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, notFound("message")
	}
	if err != nil {
		return nil, asError(err)
	}
	deliveries, err := tx.ListDeliveries(id)
	if err != nil {
		return nil, err
	}
	m.Deliveries = deliveries
	return m, nil
}

// Inbox returns newest-first incoming, outgoing or both kinds of messages. Incoming
// messages are visible only after a delivery is delivered or uncertain.
func (tx *Tx) Inbox(memberID string, direction model.InboxDirection, limit int, before string) ([]model.Message, bool, error) {
	if direction == "" {
		direction = model.InboxDirectionIn
	}
	if limit == 0 {
		limit = 20
	}
	if limit < 1 || limit > 100 {
		return nil, false, model.NewError(model.ErrorInvalidRequest, "inbox limit must be between 1 and 100")
	}
	var condition string
	switch direction {
	case model.InboxDirectionIn:
		condition = `EXISTS (SELECT 1 FROM deliveries d WHERE d.message_id = m.id AND d.recipient_id = ? AND d.state IN ('delivered', 'uncertain'))`
	case model.InboxDirectionOut:
		condition = "m.sender_id = ?"
	case model.InboxDirectionBoth:
		condition = `(m.sender_id = ? OR EXISTS (SELECT 1 FROM deliveries d WHERE d.message_id = m.id AND d.recipient_id = ? AND d.state IN ('delivered', 'uncertain')))`
	default:
		return nil, false, model.NewError(model.ErrorInvalidRequest, "invalid inbox direction")
	}
	args := []any{memberID}
	if direction == model.InboxDirectionBoth {
		args = append(args, memberID)
	}
	query := "SELECT " + messageColumns + messageFrom + "WHERE " + condition
	if before != "" {
		var seq int64
		err := tx.queryRow("SELECT seq FROM messages WHERE id = ?", before).Scan(&seq)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false, notFound("message")
		}
		if err != nil {
			return nil, false, asError(err)
		}
		query += " AND m.seq < ?"
		args = append(args, seq)
	}
	query += " ORDER BY m.seq DESC LIMIT ?"
	args = append(args, limit+1)
	rows, err := tx.query(query, args...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	var messages []*model.Message
	for rows.Next() {
		m, _, err := scanMessage(rows)
		if err != nil {
			return nil, false, asError(err)
		}
		messages = append(messages, m)
	}
	if err := rows.Err(); err != nil {
		return nil, false, asError(err)
	}
	hasMore := len(messages) > limit
	if hasMore {
		messages = messages[:limit]
	}
	result := make([]model.Message, 0, len(messages))
	for _, m := range messages {
		deliveries, err := tx.ListDeliveries(m.ID)
		if err != nil {
			return nil, false, err
		}
		m.Deliveries = deliveries
		result = append(result, *m)
	}
	return result, hasMore, nil
}

// CountMessagesToHuman counts a sender's messages in [before-10m, before).
func (tx *Tx) CountMessagesToHuman(senderID string, before model.Time) (int64, error) {
	before = nowIfZero(before)
	lower := model.FormatTime(before.Add(-10 * time.Minute))
	var count int64
	err := tx.queryRow(`SELECT count(*) FROM messages m JOIN deliveries d ON d.message_id = m.id
 JOIN members recipient ON recipient.id = d.recipient_id
 WHERE m.sender_id = ? AND recipient.name = 'human' AND m.created_at >= ? AND m.created_at < ?`, senderID, lower, dbTime(before)).Scan(&count)
	return count, asError(err)
}

// InsertDelivery inserts one recipient delivery.
func (tx *Tx) InsertDelivery(p DeliveryParams) error {
	d := &p.Delivery
	if d.State == "" {
		d.State = model.DeliveryStateQueued
	}
	var messageSeq int64
	var teamID string
	if err := tx.queryRow("SELECT seq, team_id FROM messages WHERE id = ?", p.MessageID).Scan(&messageSeq, &teamID); errors.Is(err, sql.ErrNoRows) {
		return notFound("message")
	} else if err != nil {
		return asError(err)
	}
	created := defaultTime(p.CreatedAt)
	if d.UpdatedAt.IsZero() {
		d.UpdatedAt = created
	}
	_, err := tx.exec(`INSERT INTO deliveries (message_id, recipient_id, team_id, message_seq, state, reason,
 via, wake, attempts, notify, turn_id, lease_generation, created_at, updated_at, delivered_at)
 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, p.MessageID, p.RecipientID, teamID,
		messageSeq, d.State, dbPtr(d.Reason), dbPtr(d.Via), d.Wake, d.Attempts, dbPtr(d.Notify),
		dbPtr(d.Turn), dbPtr(p.LeaseGeneration), dbTime(created), dbTime(d.UpdatedAt), dbTimePtr(d.DeliveredAt))
	return err
}

const deliveryColumns = `d.message_id, d.message_seq, d.recipient_id, recipient.name, d.state,
 d.reason, d.via, d.wake, d.attempts, d.notify, d.turn_id, d.lease_generation, d.updated_at, d.delivered_at`
const deliveryFrom = " FROM deliveries d JOIN members recipient ON recipient.id = d.recipient_id "

func scanDelivery(row scanner) (*DeliveryItem, error) {
	var item DeliveryItem
	d := &item.Delivery
	var updated string
	var delivered sql.NullString
	var leaseGeneration sql.NullInt64
	err := row.Scan(&item.MessageID, &item.MessageSeq, &d.To.ID, &d.To.Name, &d.State,
		&d.Reason, &d.Via, &d.Wake, &d.Attempts, &d.Notify, &d.Turn, &leaseGeneration, &updated, &delivered)
	if err != nil {
		return nil, err
	}
	item.LeaseGeneration = nullInt64Ptr(leaseGeneration)
	if d.UpdatedAt, err = parseTime(updated); err != nil {
		return nil, err
	}
	if d.DeliveredAt, err = timeFromNull(delivered); err != nil {
		return nil, err
	}
	return &item, nil
}

// GetDelivery returns one message/recipient delivery.
func (tx *Tx) GetDelivery(messageID, recipientID string) (*model.Delivery, error) {
	item, err := scanDelivery(tx.queryRow("SELECT "+deliveryColumns+deliveryFrom+"WHERE d.message_id = ? AND d.recipient_id = ?", messageID, recipientID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, notFound("delivery")
	}
	if err != nil {
		return nil, asError(err)
	}
	return &item.Delivery, nil
}

// ListDeliveries lists all current deliveries for a message.
func (tx *Tx) ListDeliveries(messageID string) ([]model.Delivery, error) {
	rows, err := tx.query("SELECT "+deliveryColumns+deliveryFrom+"WHERE d.message_id = ? ORDER BY d.recipient_id", messageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []model.Delivery
	for rows.Next() {
		item, err := scanDelivery(rows)
		if err != nil {
			return nil, asError(err)
		}
		result = append(result, item.Delivery)
	}
	return result, asError(rows.Err())
}

// NextDeliveries lists queued deliveries for a recipient in message sequence order.
func (tx *Tx) NextDeliveries(recipientID string, limit int) ([]DeliveryItem, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := tx.query("SELECT "+deliveryColumns+deliveryFrom+"WHERE d.recipient_id = ? AND d.state = 'queued' ORDER BY d.message_seq LIMIT ?", recipientID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []DeliveryItem
	for rows.Next() {
		item, err := scanDelivery(rows)
		if err != nil {
			return nil, asError(err)
		}
		result = append(result, *item)
	}
	return result, asError(rows.Err())
}

// ListTeamDeliveries lists a team's deliveries in message order, optionally
// restricted to states. With no states it returns every delivery.
func (tx *Tx) ListTeamDeliveries(teamID string, states ...model.DeliveryState) ([]DeliveryItem, error) {
	query := "SELECT " + deliveryColumns + deliveryFrom + "WHERE d.team_id = ?"
	args := []any{teamID}
	if len(states) > 0 {
		placeholders := make([]string, len(states))
		for i, state := range states {
			placeholders[i] = "?"
			args = append(args, state)
		}
		query += " AND d.state IN (" + strings.Join(placeholders, ",") + ")"
	}
	query += " ORDER BY d.message_seq, d.recipient_id"
	rows, err := tx.query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []DeliveryItem
	for rows.Next() {
		item, err := scanDelivery(rows)
		if err != nil {
			return nil, asError(err)
		}
		result = append(result, *item)
	}
	return result, asError(rows.Err())
}

// CountDeliveries counts a team's deliveries in one state.
func (tx *Tx) CountDeliveries(teamID string, state model.DeliveryState) (int64, error) {
	var count int64
	err := tx.queryRow("SELECT count(*) FROM deliveries WHERE team_id = ? AND state = ?", teamID, state).Scan(&count)
	return count, asError(err)
}

// UpdateDelivery changes a delivery only from expectedState.
func (tx *Tx) UpdateDelivery(messageID, recipientID string, expectedState model.DeliveryState, patch DeliveryPatch) error {
	var sets []string
	var args []any
	add := func(column string, value any) { sets = append(sets, column+" = ?"); args = append(args, value) }
	add("state", patch.State)
	if patch.Reason != nil {
		add("reason", dbPtr(*patch.Reason))
	}
	if patch.Via != nil {
		add("via", dbPtr(*patch.Via))
	}
	if patch.Wake != nil {
		add("wake", *patch.Wake)
	}
	if patch.Attempts != nil {
		add("attempts", *patch.Attempts)
	}
	if patch.Notify != nil {
		add("notify", dbPtr(*patch.Notify))
	}
	if patch.Turn != nil {
		add("turn_id", dbPtr(*patch.Turn))
	}
	if patch.LeaseGeneration != nil {
		add("lease_generation", dbPtr(*patch.LeaseGeneration))
	}
	add("updated_at", dbTime(nowIfZero(patch.UpdatedAt)))
	if patch.DeliveredAt != nil {
		add("delivered_at", dbTimePtr(*patch.DeliveredAt))
	}
	args = append(args, messageID, recipientID, expectedState)
	query := "UPDATE deliveries SET " + strings.Join(sets, ", ") + " WHERE message_id = ? AND recipient_id = ? AND state = ?"
	if patch.ExpectedLeaseGeneration != nil {
		query += " AND lease_generation = ? AND EXISTS (SELECT 1 FROM leases l WHERE l.member_id = deliveries.recipient_id AND l.generation = deliveries.lease_generation)"
		args = append(args, *patch.ExpectedLeaseGeneration)
	}
	result, err := tx.exec(query, args...)
	if err != nil {
		return err
	}
	if n, err := result.RowsAffected(); err != nil {
		return asError(err)
	} else if n == 0 {
		current, getErr := tx.GetDelivery(messageID, recipientID)
		if getErr != nil {
			return getErr
		}
		return conflict("delivery state changed").WithDetails(map[string]any{"expected": expectedState, "state": current.State})
	}
	return nil
}

// PickUpDeliveries atomically marks the next queued batch delivering and increments attempts.
func (tx *Tx) PickUpDeliveries(recipientID string, limit int, via *model.DeliveryVia, turn *string, leaseGeneration *int64, at model.Time) ([]DeliveryItem, error) {
	items, err := tx.NextDeliveries(recipientID, limit)
	if err != nil {
		return nil, err
	}
	for i := range items {
		item := &items[i]
		attempts := item.Delivery.Attempts + 1
		noReason := (*model.DeliveryReason)(nil)
		noDelivery := (*model.Time)(nil)
		patch := DeliveryPatch{State: model.DeliveryStateDelivering, Reason: &noReason,
			Via: &via, Turn: &turn, LeaseGeneration: &leaseGeneration,
			Attempts: &attempts, UpdatedAt: at, DeliveredAt: &noDelivery}
		if err := tx.UpdateDelivery(item.MessageID, item.Delivery.To.ID, model.DeliveryStateQueued, patch); err != nil {
			return nil, err
		}
		current, getErr := tx.GetDelivery(item.MessageID, item.Delivery.To.ID)
		if getErr != nil {
			return nil, getErr
		}
		item.Delivery = *current
		item.LeaseGeneration = leaseGeneration
	}
	return items, nil
}

// RenumberMessage writes the current hop and review round after a policy reset.
// The caller must hold the write transaction that decided the reset. Recorded
// message.created events remain immutable; timeline refreshes current messages.
func (tx *Tx) RenumberMessage(id string, hop, round *int64) error {
	result, err := tx.exec("UPDATE messages SET hop = ?, round = ? WHERE id = ?", dbPtr(hop), dbPtr(round), id)
	if err != nil {
		return err
	}
	return requireRows(result, notFound("message"))
}
