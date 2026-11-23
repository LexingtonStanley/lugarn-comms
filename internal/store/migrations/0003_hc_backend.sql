ALTER TABLE teams ADD COLUMN backend_choice TEXT NOT NULL DEFAULT 'herdr' CHECK (backend_choice IN ('auto', 'herdr', 'hc'));
ALTER TABLE teams ADD COLUMN backend TEXT DEFAULT 'herdr' CHECK (backend IN ('herdr', 'hc'));
ALTER TABLE teams ADD COLUMN hc_core_id TEXT;
-- Private runtime identities, never exposed as new wire fields. Keep historical
-- managed launches for cleanup after a failed or interrupted lease transition.
CREATE TABLE hc_terminals (
 team_id TEXT NOT NULL REFERENCES teams(id),
 address TEXT NOT NULL,
 core_id TEXT NOT NULL,
 member_id TEXT REFERENCES members(id),
 launch_id TEXT REFERENCES launches(id),
 pid INTEGER NOT NULL,
 foreground_pid INTEGER NOT NULL DEFAULT 0,
 space TEXT NOT NULL,
 tab TEXT NOT NULL,
 external INTEGER NOT NULL DEFAULT 0 CHECK (external IN (0, 1)),
 PRIMARY KEY(team_id, core_id, address)
) STRICT;
INSERT INTO schema_migrations (version, name) VALUES (3, '0003_hc_backend');
