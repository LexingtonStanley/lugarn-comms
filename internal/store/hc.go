package store

// HCTerminal records the identity returned by terminal.run, or a validated
// external binding. Addresses are scoped by core, never by the session alone.
type HCTerminal struct {
	Team, Core, Address string
	Member, Launch      *string
	PID, ForegroundPID  int64
	Space, Tab          string
	External            bool
}

func (tx *Tx) PutHCTerminal(t HCTerminal) error {
	_, e := tx.exec(`INSERT INTO hc_terminals(team_id,core_id,address,member_id,launch_id,pid,foreground_pid,space,tab,external) VALUES(?,?,?,?,?,?,?,?,?,?)`, t.Team, t.Core, t.Address, dbPtr(t.Member), dbPtr(t.Launch), t.PID, t.ForegroundPID, t.Space, t.Tab, t.External)
	return e
}
func (tx *Tx) HCTerminals(team string) ([]HCTerminal, error) {
	rows, e := tx.query(`SELECT team_id,core_id,address,member_id,launch_id,pid,foreground_pid,space,tab,external FROM hc_terminals WHERE team_id=?`, team)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var out []HCTerminal
	for rows.Next() {
		var t HCTerminal
		if e = rows.Scan(&t.Team, &t.Core, &t.Address, &t.Member, &t.Launch, &t.PID, &t.ForegroundPID, &t.Space, &t.Tab, &t.External); e != nil {
			return nil, e
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
func (tx *Tx) ReservedHCNames(server string) (map[string]bool, error) {
	rows, e := tx.query(`SELECT m.herdr_agent FROM members m JOIN teams t ON t.id=m.team_id WHERE t.herdr_server=? AND t.backend='hc' AND t.forgotten=0 AND m.removed_at IS NULL AND m.herdr_agent IS NOT NULL`, server)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var name string
		if e = rows.Scan(&name); e != nil {
			return nil, e
		}
		out[name] = true
	}
	return out, rows.Err()
}
func (tx *Tx) HCPaneTracked(core, address string) (bool, error) {
	var n int
	e := tx.queryRow(`SELECT count(*) FROM hc_terminals h JOIN teams t ON t.id=h.team_id JOIN members m ON m.id=h.member_id WHERE h.core_id=? AND h.address=? AND t.forgotten=0 AND m.removed_at IS NULL`, core, address).Scan(&n)
	return n > 0, e
}
