package store

import (
	"database/sql"
	"errors"
	"github.com/LexingtonStanley/lugarn-comms/internal/model"
)

func (tx *Tx) GetExternalMember(id string) (*model.ExternalMember, error) {
	var v model.ExternalMember
	var session sql.NullString
	err := tx.queryRow("SELECT workspace, pane, agent, terminal, session, activity, available FROM external_members WHERE member_id = ?", id).Scan(&v.Workspace, &v.Pane, &v.Agent, &v.Terminal, &session, &v.Activity, &v.Available)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	v.Session = stringPtr(session)
	return &v, asError(err)
}
func (tx *Tx) PutExternalMember(id string, v model.ExternalMember) error {
	_, err := tx.exec(`INSERT INTO external_members (member_id, workspace, pane, agent, terminal, session, activity, available) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
 ON CONFLICT(member_id) DO UPDATE SET session=excluded.session, activity=excluded.activity, available=excluded.available`, id, v.Workspace, v.Pane, v.Agent, v.Terminal, dbPtr(v.Session), v.Activity, v.Available)
	return err
}
func (tx *Tx) DeleteExternalMember(id string) error {
	_, err := tx.exec("DELETE FROM external_members WHERE member_id = ?", id)
	return err
}

// PaneTracked prevents duplicate ownership in this store, including a tracked
// external pane and a member launched by another team.
func (tx *Tx) PaneTracked(server, pane string) (bool, error) {
	var n int
	err := tx.queryRow(`SELECT count(*) FROM members m JOIN teams t ON t.id=m.team_id
 LEFT JOIN external_members e ON e.member_id=m.id LEFT JOIN leases l ON l.member_id=m.id
 WHERE t.herdr_server=? AND t.backend='herdr' AND t.forgotten=0 AND m.removed_at IS NULL AND (e.pane=? OR l.pane_id=?)`, server, pane, pane).Scan(&n)
	return n > 0, asError(err)
}

// MarkExternalRestarting records the explicit restart authorization and the
// latest validated resume metadata before the pane can be closed.
func (tx *Tx) MarkExternalRestarting(id string, v model.ExternalMember) error {
	if err := tx.PutExternalMember(id, v); err != nil {
		return err
	}
	_, err := tx.exec("UPDATE external_members SET restarting = 1 WHERE member_id = ?", id)
	return err
}
func (tx *Tx) ExternalRestarting(id string) (bool, error) {
	var restarting bool
	err := tx.queryRow("SELECT restarting FROM external_members WHERE member_id = ?", id).Scan(&restarting)
	return restarting, asError(err)
}
