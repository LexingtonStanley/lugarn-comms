package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
)

// InsertTurn inserts a headless turn. The partial unique index enforces one live turn per member.
func (tx *Tx) InsertTurn(p TurnParams) error {
	t := &p.Turn
	if t.Status == "" {
		t.Status = model.TurnStatusStarting
	}
	t.StartedAt = nowIfZero(t.StartedAt)
	var input, output, cached, reasoning any
	var cost any
	if t.Usage != nil {
		input, output, cached, reasoning, cost = dbPtr(t.Usage.InputTokens), dbPtr(t.Usage.OutputTokens), dbPtr(t.Usage.CachedInputTokens), dbPtr(t.Usage.ReasoningTokens), dbPtr(t.Usage.CostUSD)
	}
	_, err := tx.exec(`INSERT INTO turns (id, team_id, member_id, launch_id, lease_generation, harness,
 session_id_start, session_id_end, seal, pid, status, exit_code, error, input_tokens, output_tokens,
 cached_input_tokens, reasoning_tokens, cost_usd, usage_raw, started_at, ended_at)
 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, t.ID, t.Team, p.MemberID,
		t.Launch, t.LeaseGeneration, t.Harness, dbPtr(t.SessionIDStart), dbPtr(t.SessionIDEnd), dbPtr(p.Seal),
		dbPtr(t.PID), t.Status, dbPtr(t.ExitCode), dbPtr(t.Error), input, output, cached, reasoning, cost,
		nullableJSON(p.UsageRaw), dbTime(t.StartedAt), dbTimePtr(t.EndedAt))
	return err
}

func nullableJSON(data json.RawMessage) any {
	if len(data) == 0 {
		return nil
	}
	return string(data)
}

const turnFrom = " FROM turns t JOIN members m ON m.id = t.member_id "
const turnColumns = `t.id, t.team_id, t.member_id, m.name, t.launch_id, t.lease_generation,
 t.harness, t.session_id_start, t.session_id_end, t.seal, t.pid, t.status, t.exit_code,
 t.error, t.input_tokens, t.output_tokens, t.cached_input_tokens, t.reasoning_tokens,
 t.cost_usd, t.usage_raw, t.started_at, t.ended_at`

type scannedTurn struct {
	turn     model.Turn
	seal     *string
	usageRaw json.RawMessage
}

func scanTurn(row scanner) (*scannedTurn, error) {
	var result scannedTurn
	t := &result.turn
	var seal, usageRaw sql.NullString
	var started string
	var ended sql.NullString
	var input, output, cached, reasoning sql.NullInt64
	var cost sql.NullFloat64
	err := row.Scan(&t.ID, &t.Team, &t.Member.ID, &t.Member.Name, &t.Launch, &t.LeaseGeneration,
		&t.Harness, &t.SessionIDStart, &t.SessionIDEnd, &seal, &t.PID, &t.Status, &t.ExitCode,
		&t.Error, &input, &output, &cached, &reasoning, &cost, &usageRaw, &started, &ended)
	if err != nil {
		return nil, err
	}
	result.seal = stringPtr(seal)
	if usageRaw.Valid {
		result.usageRaw = json.RawMessage(usageRaw.String)
	}
	if input.Valid || output.Valid || cached.Valid || reasoning.Valid || cost.Valid {
		t.Usage = &model.Usage{}
	}
	if input.Valid {
		t.Usage.InputTokens = &input.Int64
	}
	if output.Valid {
		t.Usage.OutputTokens = &output.Int64
	}
	if cached.Valid {
		t.Usage.CachedInputTokens = &cached.Int64
	}
	if reasoning.Valid {
		t.Usage.ReasoningTokens = &reasoning.Int64
	}
	if cost.Valid {
		t.Usage.CostUSD = &cost.Float64
	}
	if t.StartedAt, err = parseTime(started); err != nil {
		return nil, err
	}
	if t.EndedAt, err = timeFromNull(ended); err != nil {
		return nil, err
	}
	t.Consumed, t.AutoReplies = []string{}, []string{}
	return &result, nil
}

// GetTurn returns a turn with consumed and auto-reply message ids.
func (tx *Tx) GetTurn(id string) (*model.Turn, error) {
	scanned, err := scanTurn(tx.queryRow("SELECT "+turnColumns+turnFrom+"WHERE t.id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, notFound("turn")
	}
	if err != nil {
		return nil, asError(err)
	}
	if err := tx.loadTurnMessages(&scanned.turn); err != nil {
		return nil, err
	}
	return &scanned.turn, nil
}

func (tx *Tx) loadTurnMessages(t *model.Turn) error {
	rows, err := tx.query("SELECT message_id FROM deliveries WHERE turn_id = ? ORDER BY message_seq", t.ID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return asError(err)
		}
		t.Consumed = append(t.Consumed, id)
	}
	if err := rows.Close(); err != nil {
		return asError(err)
	}
	rows, err = tx.query("SELECT id FROM messages WHERE turn_id = ? AND kind = 'auto_reply' ORDER BY seq", t.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return asError(err)
		}
		t.AutoReplies = append(t.AutoReplies, id)
	}
	return asError(rows.Err())
}

// GetLiveTurn returns the member's starting or running turn.
func (tx *Tx) GetLiveTurn(memberID string) (*model.Turn, error) {
	scanned, err := scanTurn(tx.queryRow("SELECT "+turnColumns+turnFrom+"WHERE t.member_id = ? AND t.status IN ('starting', 'running')", memberID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, notFound("live turn")
	}
	if err != nil {
		return nil, asError(err)
	}
	if err := tx.loadTurnMessages(&scanned.turn); err != nil {
		return nil, err
	}
	return &scanned.turn, nil
}

// ListTurns lists a member's turns newest first.
func (tx *Tx) ListTurns(memberID string, limit int) ([]model.Turn, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := tx.query("SELECT "+turnColumns+turnFrom+"WHERE t.member_id = ? ORDER BY t.started_at DESC, t.id DESC LIMIT ?", memberID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var scanned []*scannedTurn
	for rows.Next() {
		item, err := scanTurn(rows)
		if err != nil {
			return nil, asError(err)
		}
		scanned = append(scanned, item)
	}
	if err := rows.Err(); err != nil {
		return nil, asError(err)
	}
	result := make([]model.Turn, 0, len(scanned))
	for _, item := range scanned {
		if err := tx.loadTurnMessages(&item.turn); err != nil {
			return nil, err
		}
		result = append(result, item.turn)
	}
	return result, nil
}

// CountLiveTurns counts starting and running turns in a team.
func (tx *Tx) CountLiveTurns(teamID string) (int64, error) {
	var count int64
	err := tx.queryRow("SELECT count(*) FROM turns WHERE team_id = ? AND status IN ('starting', 'running')", teamID).Scan(&count)
	return count, asError(err)
}

// GetTurnSeal reads the store-only pending session seal.
func (tx *Tx) GetTurnSeal(turnID string) (*string, error) {
	var seal sql.NullString
	err := tx.queryRow("SELECT seal FROM turns WHERE id = ?", turnID).Scan(&seal)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, notFound("turn")
	}
	if err != nil {
		return nil, asError(err)
	}
	return stringPtr(seal), nil
}

// GetTurnUsageRaw returns the harness's unmodified usage JSON, if present.
func (tx *Tx) GetTurnUsageRaw(turnID string) (json.RawMessage, error) {
	var raw sql.NullString
	err := tx.queryRow("SELECT usage_raw FROM turns WHERE id = ?", turnID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, notFound("turn")
	}
	if err != nil {
		return nil, asError(err)
	}
	if !raw.Valid {
		return nil, nil
	}
	return json.RawMessage(raw.String), nil
}

// UpdateTurn applies patch, optionally guarded by the lease generation recorded at pickup.
func (tx *Tx) UpdateTurn(id string, expectedLeaseGeneration *int64, patch TurnPatch) error {
	var sets []string
	var args []any
	add := func(column string, value any) { sets = append(sets, column+" = ?"); args = append(args, value) }
	if patch.Status != nil {
		add("status", *patch.Status)
	}
	if patch.SessionIDEnd != nil {
		add("session_id_end", dbPtr(*patch.SessionIDEnd))
	}
	if patch.PID != nil {
		add("pid", dbPtr(*patch.PID))
	}
	if patch.ExitCode != nil {
		add("exit_code", dbPtr(*patch.ExitCode))
	}
	if patch.Error != nil {
		add("error", dbPtr(*patch.Error))
	}
	if patch.Usage != nil {
		usage := *patch.Usage
		if usage == nil {
			for _, column := range []string{"input_tokens", "output_tokens", "cached_input_tokens", "reasoning_tokens", "cost_usd"} {
				add(column, nil)
			}
		} else {
			add("input_tokens", dbPtr(usage.InputTokens))
			add("output_tokens", dbPtr(usage.OutputTokens))
			add("cached_input_tokens", dbPtr(usage.CachedInputTokens))
			add("reasoning_tokens", dbPtr(usage.ReasoningTokens))
			add("cost_usd", dbPtr(usage.CostUSD))
		}
	}
	if patch.UsageRaw != nil {
		add("usage_raw", nullableJSON(*patch.UsageRaw))
	}
	if patch.EndedAt != nil {
		add("ended_at", dbTimePtr(*patch.EndedAt))
	}
	if len(sets) == 0 {
		return nil
	}
	query := "UPDATE turns SET " + strings.Join(sets, ", ") + " WHERE id = ?"
	args = append(args, id)
	if expectedLeaseGeneration != nil {
		query += " AND lease_generation = ? AND EXISTS (SELECT 1 FROM leases l WHERE l.member_id = turns.member_id AND l.generation = turns.lease_generation)"
		args = append(args, *expectedLeaseGeneration)
	}
	result, err := tx.exec(query, args...)
	if err != nil {
		return err
	}
	return requireRows(result, conflict("turn is unknown or its lease generation changed"))
}
