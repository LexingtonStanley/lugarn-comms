-- Existing derived names are backfilled by migrateHerdrNames in this same transaction.
ALTER TABLE teams ADD COLUMN display_name TEXT;
ALTER TABLE teams ADD COLUMN placement TEXT NOT NULL DEFAULT 'tabs' CHECK (placement IN ('tabs', 'workspaces'));
ALTER TABLE teams ADD COLUMN adopted INTEGER NOT NULL DEFAULT 0 CHECK (adopted IN (0, 1));
ALTER TABLE teams ADD COLUMN forgotten INTEGER NOT NULL DEFAULT 0 CHECK (forgotten IN (0, 1));
ALTER TABLE members ADD COLUMN herdr_workspace TEXT;
UPDATE members SET herdr_workspace = (SELECT herdr_workspace FROM teams WHERE id = members.team_id)
 WHERE herdr_agent IS NOT NULL;
CREATE TABLE external_members (
 member_id TEXT PRIMARY KEY REFERENCES members(id),
 workspace TEXT NOT NULL, pane TEXT NOT NULL, agent TEXT NOT NULL, terminal TEXT NOT NULL,
 session TEXT, activity TEXT NOT NULL DEFAULT 'unknown' CHECK (activity IN ('idle', 'working', 'blocked', 'done', 'unknown')),
 available INTEGER NOT NULL DEFAULT 1 CHECK (available IN (0, 1)),
 restarting INTEGER NOT NULL DEFAULT 0 CHECK (restarting IN (0, 1))
) STRICT;
-- pi has no v1 session ID; its provenance seal and briefing live with its launch.
ALTER TABLE launches ADD COLUMN pane_seal TEXT;
ALTER TABLE launches ADD COLUMN pane_briefed INTEGER NOT NULL DEFAULT 0 CHECK (pane_briefed IN (0, 1));
INSERT INTO schema_migrations (version, name) VALUES (2, '0002_herdr_identity');
