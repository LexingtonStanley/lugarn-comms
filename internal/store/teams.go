package store

import (
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
)

type scanner interface{ Scan(...any) error }

const teamColumns = `id, name, pattern, spec_version, revision, run, state,
 herdr_server, herdr_workspace, hop_budget, review_rounds, per_recipient,
 headless_turns, turn_timeout_seconds, runner_state, runner_pid, runner_pane,
 runner_started_at, runner_heartbeat_at, last_app_contact_at, created_at,
 updated_at, started_at, stopped_at`

func scanTeam(row scanner) (*Team, error) {
	var t Team
	var state, runnerState string
	var workspace, pane, heartbeat, started, stopped, contact sql.NullString
	var pid sql.NullInt64
	var created, updated string
	var runnerStarted sql.NullString
	err := row.Scan(&t.ID, &t.Name, &t.Pattern, &t.SpecVersion, &t.Revision, &t.Run,
		&state, &t.HerdrServer, &workspace, &t.HopBudget, &t.ReviewRounds,
		&t.PerRecipient, &t.HeadlessTurns, &t.TurnTimeoutSeconds, &runnerState,
		&pid, &pane, &runnerStarted, &heartbeat, &contact, &created, &updated,
		&started, &stopped)
	if err != nil {
		return nil, err
	}
	t.State = model.TeamState(state)
	t.Runner.State = model.RunnerState(runnerState)
	t.HerdrWorkspace = stringPtr(workspace)
	if pid.Valid {
		t.Runner.PID = &pid.Int64
	}
	t.Runner.Pane = stringPtr(pane)
	if t.CreatedAt, err = parseTime(created); err != nil {
		return nil, err
	}
	if t.UpdatedAt, err = parseTime(updated); err != nil {
		return nil, err
	}
	if t.Runner.StartedAt, err = timeFromNull(runnerStarted); err != nil {
		return nil, err
	}
	if t.Runner.HeartbeatAt, err = timeFromNull(heartbeat); err != nil {
		return nil, err
	}
	if t.LastAppContactAt, err = timeFromNull(contact); err != nil {
		return nil, err
	}
	if t.StartedAt, err = timeFromNull(started); err != nil {
		return nil, err
	}
	if t.StoppedAt, err = timeFromNull(stopped); err != nil {
		return nil, err
	}
	return &t, nil
}

// CreateTeam inserts a team. Zero-valued limits receive protocol defaults.
func (tx *Tx) CreateTeam(t Team) error {
	if t.SpecVersion == 0 {
		t.SpecVersion = 1
	}
	if t.Revision == 0 {
		t.Revision = 1
	}
	if t.State == "" {
		t.State = model.TeamStateStopped
	}
	if t.HerdrServer == "" {
		t.HerdrServer = model.DefaultHerdrServer
	}
	if t.HopBudget == 0 {
		t.HopBudget = model.DefaultHopBudget
	}
	if t.ReviewRounds == 0 {
		t.ReviewRounds = model.DefaultReviewRounds
	}
	if t.PerRecipient == 0 {
		t.PerRecipient = model.DefaultPerRecipient
	}
	if t.HeadlessTurns == 0 {
		t.HeadlessTurns = model.DefaultHeadlessTurns
	}
	if t.TurnTimeoutSeconds == 0 {
		t.TurnTimeoutSeconds = model.DefaultTurnTimeoutSeconds
	}
	if t.Runner.State == "" {
		t.Runner.State = model.RunnerStateDown
	}
	now := model.Time{Time: time.Now()}
	if t.CreatedAt.IsZero() {
		t.CreatedAt = now
	}
	if t.UpdatedAt.IsZero() {
		t.UpdatedAt = t.CreatedAt
	}
	_, err := tx.exec(`INSERT INTO teams (
 id, name, pattern, spec_version, revision, run, state, herdr_server,
 herdr_workspace, hop_budget, review_rounds, per_recipient, headless_turns,
 turn_timeout_seconds, runner_state, runner_pid, runner_pane, runner_started_at,
 runner_heartbeat_at, last_app_contact_at, created_at, updated_at, started_at, stopped_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.ID, t.Name, t.Pattern, t.SpecVersion, t.Revision, t.Run, t.State,
		t.HerdrServer, dbPtr(t.HerdrWorkspace), t.HopBudget, t.ReviewRounds,
		t.PerRecipient, t.HeadlessTurns, t.TurnTimeoutSeconds, t.Runner.State,
		dbPtr(t.Runner.PID), dbPtr(t.Runner.Pane), dbTimePtr(t.Runner.StartedAt),
		dbTimePtr(t.Runner.HeartbeatAt), dbTimePtr(t.LastAppContactAt), dbTime(t.CreatedAt),
		dbTime(t.UpdatedAt), dbTimePtr(t.StartedAt), dbTimePtr(t.StoppedAt))
	return err
}

// GetTeam returns a team by id.
func (tx *Tx) GetTeam(id string) (*Team, error) {
	t, err := scanTeam(tx.queryRow("SELECT "+teamColumns+" FROM teams WHERE id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, notFound("team")
	}
	return t, asError(err)
}

// GetTeamByName returns a team by its unique name.
func (tx *Tx) GetTeamByName(name string) (*Team, error) {
	t, err := scanTeam(tx.queryRow("SELECT "+teamColumns+" FROM teams WHERE name = ?", name))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, notFound("team")
	}
	return t, asError(err)
}

// ListTeams returns all teams ordered by name.
func (tx *Tx) ListTeams() ([]Team, error) {
	rows, err := tx.query("SELECT " + teamColumns + " FROM teams ORDER BY name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var teams []Team
	for rows.Next() {
		team, err := scanTeam(rows)
		if err != nil {
			return nil, asError(err)
		}
		teams = append(teams, *team)
	}
	return teams, asError(rows.Err())
}

// UpdateTeam applies patch and updates updated_at.
func (tx *Tx) UpdateTeam(id string, patch TeamPatch, at model.Time) error {
	var sets []string
	var args []any
	add := func(column string, value any) {
		sets = append(sets, column+" = ?")
		args = append(args, value)
	}
	if patch.Name != nil {
		add("name", *patch.Name)
	}
	if patch.Pattern != nil {
		add("pattern", *patch.Pattern)
	}
	if patch.State != nil {
		add("state", *patch.State)
	}
	if patch.HerdrServer != nil {
		add("herdr_server", *patch.HerdrServer)
	}
	if patch.HerdrWorkspace != nil {
		add("herdr_workspace", dbPtr(*patch.HerdrWorkspace))
	}
	if patch.HopBudget != nil {
		add("hop_budget", *patch.HopBudget)
	}
	if patch.ReviewRounds != nil {
		add("review_rounds", *patch.ReviewRounds)
	}
	if patch.PerRecipient != nil {
		add("per_recipient", *patch.PerRecipient)
	}
	if patch.HeadlessTurns != nil {
		add("headless_turns", *patch.HeadlessTurns)
	}
	if patch.TurnTimeoutSeconds != nil {
		add("turn_timeout_seconds", *patch.TurnTimeoutSeconds)
	}
	if patch.StartedAt != nil {
		add("started_at", dbTimePtr(*patch.StartedAt))
	}
	if patch.StoppedAt != nil {
		add("stopped_at", dbTimePtr(*patch.StoppedAt))
	}
	if len(sets) == 0 {
		return nil
	}
	add("updated_at", dbTime(nowIfZero(at)))
	args = append(args, id)
	result, err := tx.exec("UPDATE teams SET "+strings.Join(sets, ", ")+" WHERE id = ?", args...)
	if err != nil {
		return err
	}
	return requireRows(result, notFound("team"))
}

// BumpTeamRevision conditionally increments revision and returns its new value.
func (tx *Tx) BumpTeamRevision(id string, expected int64, at model.Time) (int64, error) {
	result, err := tx.exec("UPDATE teams SET revision = revision + 1, updated_at = ? WHERE id = ? AND revision = ?", dbTime(nowIfZero(at)), id, expected)
	if err != nil {
		return 0, err
	}
	if n, err := result.RowsAffected(); err != nil {
		return 0, asError(err)
	} else if n == 0 {
		team, getErr := tx.GetTeam(id)
		if getErr != nil {
			return 0, getErr
		}
		return 0, model.NewError(model.ErrorRevisionConflict, "team revision changed").WithDetails(map[string]any{"expected": expected, "actual": team.Revision})
	}
	return expected + 1, nil
}

// BumpTeamRun increments the run counter.
func (tx *Tx) BumpTeamRun(id string, at model.Time) (int64, error) {
	result, err := tx.exec("UPDATE teams SET run = run + 1, updated_at = ? WHERE id = ?", dbTime(nowIfZero(at)), id)
	if err != nil {
		return 0, err
	}
	if err := requireRows(result, notFound("team")); err != nil {
		return 0, err
	}
	var run int64
	if err := tx.queryRow("SELECT run FROM teams WHERE id = ?", id).Scan(&run); err != nil {
		return 0, asError(err)
	}
	return run, nil
}

// Heartbeat records the runner's complete runtime record.
func (tx *Tx) Heartbeat(teamID string, runner model.Runner, at model.Time) error {
	result, err := tx.exec(`UPDATE teams SET runner_state = ?, runner_pid = ?, runner_pane = ?,
 runner_started_at = ?, runner_heartbeat_at = ?, updated_at = ? WHERE id = ?`,
		runner.State, dbPtr(runner.PID), dbPtr(runner.Pane), dbTimePtr(runner.StartedAt),
		dbTimePtr(runner.HeartbeatAt), dbTime(nowIfZero(at)), teamID)
	if err != nil {
		return err
	}
	return requireRows(result, notFound("team"))
}

// TouchAppContact updates contact at most once per minute. A nil teamID touches all teams.
func (tx *Tx) TouchAppContact(teamID *string, at model.Time) (bool, error) {
	at = nowIfZero(at)
	threshold := model.FormatTime(at.Add(-time.Minute))
	query := "UPDATE teams SET last_app_contact_at = ? WHERE (last_app_contact_at IS NULL OR last_app_contact_at <= ?)"
	args := []any{dbTime(at), threshold}
	if teamID != nil {
		query += " AND id = ?"
		args = append(args, *teamID)
	}
	result, err := tx.exec(query, args...)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n > 0, asError(err)
}

func requireRows(result sql.Result, refusal error) error {
	n, err := result.RowsAffected()
	if err != nil {
		return asError(err)
	}
	if n == 0 {
		return refusal
	}
	return nil
}
