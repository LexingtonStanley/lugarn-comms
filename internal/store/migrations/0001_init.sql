-- lugarn-comms store, migration 0001: the v1 schema.
--
-- Target: SQLite >= 3.45 through modernc.org/sqlite.
-- The store sets these per connection at open time, never here:
--   PRAGMA journal_mode = WAL;  PRAGMA busy_timeout = 5000;  PRAGMA foreign_keys = ON;
-- The migrator runs each file whose version is above MAX(schema_migrations.version) inside
-- one transaction. Each file records itself as its last statement.
--
-- Conventions (docs/protocol.md, "The store"):
--   * Timestamps are TEXT, RFC 3339 UTC with milliseconds: 2026-10-26T14:03:07.123Z.
--   * Booleans are INTEGER 0/1.
--   * Enum columns carry a CHECK with the full value list from schema/common.v1.schema.json;
--     scripts/check-migrations.py compares the two.
--   * Ids: team ids and spec member ids come from the app (opaque); everything else is a ULID.

CREATE TABLE schema_migrations (
  version    INTEGER PRIMARY KEY,
  name       TEXT NOT NULL,
  applied_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
) STRICT;

-- host_id: 32 hex chars, random, made once. It prefixes cursors and is hello's host.id.
CREATE TABLE meta (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
) STRICT;

INSERT INTO meta (key, value) VALUES ('host_id', lower(hex(randomblob(16))));

-- One row per team. The limits columns mirror the spec's `limits` with defaults filled in.
CREATE TABLE teams (
  id                   TEXT PRIMARY KEY CHECK (length(id) BETWEEN 1 AND 64),
  name                 TEXT NOT NULL UNIQUE,
  pattern              TEXT NOT NULL CHECK (pattern IN ('orchestrator', 'manager_workers', 'writer_reviewer', 'pipeline', 'custom')),
  spec_version         INTEGER NOT NULL CHECK (spec_version = 1),
  revision             INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1),
  run                  INTEGER NOT NULL DEFAULT 0 CHECK (run >= 0),
  state                TEXT NOT NULL DEFAULT 'stopped' CHECK (state IN ('stopped', 'starting', 'running', 'stopping')),
  herdr_server         TEXT NOT NULL DEFAULT 'default',
  herdr_workspace      TEXT,
  hop_budget           INTEGER NOT NULL DEFAULT 16 CHECK (hop_budget BETWEEN 1 AND 1000),
  review_rounds        INTEGER NOT NULL DEFAULT 3 CHECK (review_rounds BETWEEN 1 AND 100),
  per_recipient        INTEGER NOT NULL DEFAULT 1 CHECK (per_recipient = 1),
  headless_turns       INTEGER NOT NULL DEFAULT 3 CHECK (headless_turns BETWEEN 1 AND 24),
  turn_timeout_seconds INTEGER NOT NULL DEFAULT 1800 CHECK (turn_timeout_seconds BETWEEN 60 AND 86400),
  runner_state         TEXT NOT NULL DEFAULT 'down' CHECK (runner_state IN ('up', 'down')),
  runner_pid           INTEGER,
  runner_pane          TEXT,
  runner_started_at    TEXT,
  runner_heartbeat_at  TEXT,
  last_app_contact_at  TEXT,
  created_at           TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  updated_at           TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  started_at           TEXT,
  stopped_at           TEXT
) STRICT;

-- Every member Lugarn knows: spec members, spawned workers, and each team's implicit `human`.
-- Rows are never deleted. `removed_at` takes a member out of the live roster and frees its name.
CREATE TABLE members (
  id              TEXT PRIMARY KEY CHECK (length(id) BETWEEN 1 AND 64),
  team_id         TEXT NOT NULL REFERENCES teams (id),
  kind            TEXT NOT NULL CHECK (kind IN ('agent', 'human')),
  name            TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 32),
  role            TEXT CHECK (role IN ('orchestrator', 'manager', 'worker', 'writer', 'reviewer', 'stage', 'custom')),
  origin          TEXT NOT NULL CHECK (origin IN ('spec', 'spawn', 'implicit')),
  position        INTEGER,
  spawned_by      TEXT REFERENCES members (id),
  spawned_run     INTEGER,
  harness         TEXT CHECK (harness IN ('claude', 'codex', 'opencode', 'pi')),
  profile         TEXT CHECK (profile IN ('read-only', 'workspace', 'bypass')),
  mode            TEXT CHECK (mode IN ('headless', 'pane')),
  folder          TEXT,
  worktree        INTEGER NOT NULL DEFAULT 0 CHECK (worktree IN (0, 1)),
  worktree_id     TEXT REFERENCES worktrees (id),
  brief           TEXT,
  task            TEXT,
  host            TEXT,
  state           TEXT NOT NULL DEFAULT 'stopped' CHECK (state IN ('starting', 'running', 'stopping', 'stopped')),
  stop_reason     TEXT CHECK (stop_reason IN ('person', 'spawner', 'team_stopped', 'exited', 'died', 'launch_failed')),
  activity        TEXT CHECK (activity IN ('idle', 'working', 'blocked', 'done', 'unknown')),
  last_driver     TEXT CHECK (last_driver IN ('headless', 'pane')),
  session_id      TEXT,
  herdr_agent     TEXT,
  created_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  updated_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  started_at      TEXT,
  stopped_at      TEXT,
  removed_at      TEXT,
  removed_reason  TEXT CHECK (removed_reason IN ('removed_from_spec', 'new_run')),
  CHECK ((kind = 'human') = (name = 'human')),
  CHECK (kind = 'human' OR (role IS NOT NULL AND harness IS NOT NULL AND profile IS NOT NULL
                            AND mode IS NOT NULL AND folder IS NOT NULL AND brief IS NOT NULL
                            AND origin IN ('spec', 'spawn'))),
  CHECK (kind = 'agent' OR (role IS NULL AND harness IS NULL AND profile IS NULL AND mode IS NULL
                            AND folder IS NULL AND origin = 'implicit' AND state = 'running')),
  CHECK ((origin = 'spawn') = (spawned_by IS NOT NULL)),
  CHECK ((origin = 'spawn') = (spawned_run IS NOT NULL)),
  CHECK (state = 'stopped' OR stop_reason IS NULL),
  CHECK ((removed_at IS NULL) = (removed_reason IS NULL)),
  CHECK (host IS NULL)
) STRICT;

CREATE UNIQUE INDEX members_live_name ON members (team_id, name) WHERE removed_at IS NULL;
CREATE INDEX members_team ON members (team_id, state);
CREATE INDEX members_spawned_by ON members (spawned_by, spawned_run, state) WHERE spawned_by IS NOT NULL;

-- Every worktree the broker created. The broker never removes one on its own; only
-- `remove-worktrees` does, and it sets removed_at. member_id is the member using it now (a
-- restarted worker with the same name reuses its worktree).
CREATE TABLE worktrees (
  id         TEXT PRIMARY KEY CHECK (length(id) = 26),
  team_id    TEXT NOT NULL REFERENCES teams (id),
  member_id  TEXT NOT NULL REFERENCES members (id),
  repo       TEXT NOT NULL,
  path       TEXT NOT NULL,
  branch     TEXT NOT NULL,
  base       TEXT NOT NULL,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  removed_at TEXT
) STRICT;

CREATE UNIQUE INDEX worktrees_live_path ON worktrees (path) WHERE removed_at IS NULL;
CREATE INDEX worktrees_team ON worktrees (team_id) WHERE removed_at IS NULL;

-- The person's hold on a member (the `hold` op). Live while held_until is in the future and
-- released_at is NULL; a renewal moves held_until. One row per member, reused.
CREATE TABLE holds (
  member_id   TEXT PRIMARY KEY REFERENCES members (id),
  team_id     TEXT NOT NULL REFERENCES teams (id),
  since       TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  held_until  TEXT NOT NULL,
  released_at TEXT
) STRICT;

-- Spawn settings of spec members. Spawned members have none (no nesting in v1).
-- harnesses: a JSON array of harness names, or NULL for every harness the host supports.
CREATE TABLE spawn_settings (
  member_id       TEXT PRIMARY KEY REFERENCES members (id),
  enabled         INTEGER NOT NULL CHECK (enabled IN (0, 1)),
  running_at_once INTEGER NOT NULL DEFAULT 3 CHECK (running_at_once BETWEEN 1 AND 24),
  per_run         INTEGER NOT NULL DEFAULT 8 CHECK (per_run BETWEEN 1 AND 24),
  harnesses       TEXT CHECK (harnesses IS NULL OR (json_valid(harnesses) AND json_type(harnesses) = 'array')),
  mode            TEXT NOT NULL DEFAULT 'headless' CHECK (mode IN ('headless', 'pane')),
  folder          TEXT NOT NULL DEFAULT 'worktree' CHECK (folder IN ('worktree', 'shared')),
  max_profile     TEXT NOT NULL DEFAULT 'workspace' CHECK (max_profile IN ('read-only', 'workspace', 'bypass')),
  brief           TEXT,
  CHECK (enabled = 0 OR brief IS NOT NULL)
) STRICT;

-- The grid: one row per granted capability. No row means denied. `human` never appears:
-- its row and column are implicit. A `wake` row requires a `message` row for the same pair.
CREATE TABLE grid (
  team_id    TEXT NOT NULL REFERENCES teams (id),
  from_id    TEXT NOT NULL REFERENCES members (id),
  to_id      TEXT NOT NULL REFERENCES members (id),
  capability TEXT NOT NULL CHECK (capability IN ('message', 'wake')),
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  PRIMARY KEY (from_id, to_id, capability),
  CHECK (from_id <> to_id)
) STRICT, WITHOUT ROWID;

CREATE INDEX grid_team ON grid (team_id);

-- One launch per lease generation that has a driver. Its id names the token file, the
-- per-launch MCP config and #136's pointer file.
CREATE TABLE launches (
  id               TEXT PRIMARY KEY CHECK (length(id) = 26),
  member_id        TEXT NOT NULL REFERENCES members (id),
  generation       INTEGER NOT NULL CHECK (generation >= 1),
  driver           TEXT NOT NULL CHECK (driver IN ('headless', 'pane')),
  session_id_start TEXT,
  pane_id          TEXT,
  started_at       TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  ended_at         TEXT,
  end_reason       TEXT CHECK (end_reason IN ('converted', 'stopped', 'died')),
  UNIQUE (member_id, generation),
  CHECK ((ended_at IS NULL) = (end_reason IS NULL))
) STRICT;

-- The driver lease: exactly one row per agent member. Every change bumps `generation`.
CREATE TABLE leases (
  member_id  TEXT PRIMARY KEY REFERENCES members (id),
  driver     TEXT NOT NULL DEFAULT 'none' CHECK (driver IN ('headless', 'pane', 'none')),
  generation INTEGER NOT NULL DEFAULT 0 CHECK (generation >= 0),
  launch_id  TEXT REFERENCES launches (id),
  pane_id    TEXT,
  updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  CHECK ((driver = 'none') = (launch_id IS NULL)),
  CHECK (driver = 'pane' OR pane_id IS NULL)
) STRICT;

-- Launch tokens: SHA-256 of the token as 64 lower-case hex chars. The token itself is never stored.
CREATE TABLE tokens (
  launch_id  TEXT PRIMARY KEY REFERENCES launches (id),
  member_id  TEXT NOT NULL REFERENCES members (id),
  hash       TEXT NOT NULL UNIQUE CHECK (length(hash) = 64 AND hash NOT GLOB '*[^0-9a-f]*'),
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  revoked_at TEXT
) STRICT;

CREATE UNIQUE INDEX tokens_one_active ON tokens (member_id) WHERE revoked_at IS NULL;

-- Every session id a member has had, in order. members.session_id is the current one.
-- seal: 8 Crockford base32 chars from the OS CSPRNG, ending every real header in the session.
CREATE TABLE member_sessions (
  member_id     TEXT NOT NULL REFERENCES members (id),
  ordinal       INTEGER NOT NULL CHECK (ordinal >= 1),
  session_id    TEXT NOT NULL CHECK (length(session_id) BETWEEN 1 AND 128),
  harness       TEXT NOT NULL CHECK (harness IN ('claude', 'codex', 'opencode', 'pi')),
  source        TEXT NOT NULL CHECK (source IN ('assigned', 'reported', 'pointer')),
  launch_id     TEXT REFERENCES launches (id),
  seal          TEXT NOT NULL CHECK (length(seal) = 8 AND seal NOT GLOB '*[^0-9A-HJKMNP-TV-Z]*'),
  briefed_at    TEXT,
  first_seen_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  PRIMARY KEY (member_id, ordinal),
  UNIQUE (member_id, session_id)
) STRICT, WITHOUT ROWID;

-- Hop budget counters, one row per team. The base is teams.hop_budget.
CREATE TABLE hop_counters (
  team_id      TEXT PRIMARY KEY REFERENCES teams (id),
  used         INTEGER NOT NULL DEFAULT 0 CHECK (used >= 0),
  allowance    INTEGER NOT NULL CHECK (allowance >= 1),
  exhausted_at TEXT,
  updated_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
) STRICT;

-- Review-round counters, one row per (writer, reviewer) pair that has exchanged a message
-- this run. The base is teams.review_rounds.
CREATE TABLE round_counters (
  team_id      TEXT NOT NULL REFERENCES teams (id),
  writer_id    TEXT NOT NULL REFERENCES members (id),
  reviewer_id  TEXT NOT NULL REFERENCES members (id),
  used         INTEGER NOT NULL DEFAULT 0 CHECK (used >= 0),
  allowance    INTEGER NOT NULL CHECK (allowance >= 1),
  exhausted_at TEXT,
  updated_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  PRIMARY KEY (writer_id, reviewer_id),
  CHECK (writer_id <> reviewer_id)
) STRICT, WITHOUT ROWID;

CREATE INDEX round_counters_team ON round_counters (team_id);

-- Headless turns. Consumed messages are deliveries.turn_id; auto-replies are messages with
-- kind 'auto_reply' and this turn_id. seal: the seal a session's first turn was briefed with,
-- kept here until a harness that reports its id (Codex, OpenCode) reports it.
CREATE TABLE turns (
  id                  TEXT PRIMARY KEY CHECK (length(id) = 26),
  team_id             TEXT NOT NULL REFERENCES teams (id),
  member_id           TEXT NOT NULL REFERENCES members (id),
  launch_id           TEXT NOT NULL REFERENCES launches (id),
  lease_generation    INTEGER NOT NULL CHECK (lease_generation >= 1),
  harness             TEXT NOT NULL CHECK (harness IN ('claude', 'codex', 'opencode', 'pi')),
  session_id_start    TEXT,
  session_id_end      TEXT,
  seal                TEXT CHECK (seal IS NULL OR (length(seal) = 8 AND seal NOT GLOB '*[^0-9A-HJKMNP-TV-Z]*')),
  pid                 INTEGER,
  status              TEXT NOT NULL DEFAULT 'starting' CHECK (status IN ('starting', 'running', 'succeeded', 'failed', 'cancelled', 'timed_out', 'lost')),
  exit_code           INTEGER,
  error               TEXT,
  input_tokens        INTEGER CHECK (input_tokens >= 0),
  output_tokens       INTEGER CHECK (output_tokens >= 0),
  cached_input_tokens INTEGER CHECK (cached_input_tokens >= 0),
  reasoning_tokens    INTEGER CHECK (reasoning_tokens >= 0),
  cost_usd            REAL CHECK (cost_usd >= 0),
  usage_raw           TEXT CHECK (usage_raw IS NULL OR json_valid(usage_raw)),
  started_at          TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  ended_at            TEXT,
  CHECK ((status IN ('starting', 'running')) = (ended_at IS NULL))
) STRICT;

-- At most one live turn per member (per_recipient = 1).
CREATE UNIQUE INDEX turns_one_live ON turns (member_id) WHERE status IN ('starting', 'running');
-- The team-wide cap on concurrent headless turns counts these.
CREATE INDEX turns_team_live ON turns (team_id) WHERE status IN ('starting', 'running');
CREATE INDEX turns_member ON turns (member_id, started_at);

-- Messages. `seq` orders everything (per-recipient delivery, inbox paging); `id` is the
-- public ULID. A broadcast (from `human` only) has one delivery per recipient.
CREATE TABLE messages (
  seq        INTEGER PRIMARY KEY AUTOINCREMENT,
  id         TEXT NOT NULL UNIQUE CHECK (length(id) = 26),
  team_id    TEXT NOT NULL REFERENCES teams (id),
  run        INTEGER NOT NULL CHECK (run >= 1),
  sender_id  TEXT NOT NULL REFERENCES members (id),
  broadcast  INTEGER NOT NULL DEFAULT 0 CHECK (broadcast IN (0, 1)),
  kind       TEXT NOT NULL CHECK (kind IN ('message', 'reply', 'auto_reply', 'task')),
  reply_to   TEXT REFERENCES messages (id),
  text       TEXT NOT NULL CHECK (length(text) BETWEEN 1 AND 65536),
  hop        INTEGER CHECK (hop >= 1),
  round      INTEGER CHECK (round >= 1),
  turn_id    TEXT REFERENCES turns (id),
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  CHECK ((kind IN ('reply', 'auto_reply')) = (reply_to IS NOT NULL)),
  CHECK (kind <> 'auto_reply' OR turn_id IS NOT NULL)
) STRICT;

CREATE INDEX messages_team_seq ON messages (team_id, seq);
CREATE INDEX messages_sender_seq ON messages (sender_id, seq);
-- The per-member rate limit on messages to `human` (5 in any 10 minutes).
CREATE INDEX messages_sender_created ON messages (sender_id, created_at);
CREATE INDEX messages_turn ON messages (turn_id) WHERE turn_id IS NOT NULL;

-- One delivery per (message, recipient). message_seq is copied from messages.seq so the
-- runner's pickup query ("oldest deliverable for this recipient") is one index range.
CREATE TABLE deliveries (
  message_id       TEXT NOT NULL REFERENCES messages (id),
  recipient_id     TEXT NOT NULL REFERENCES members (id),
  team_id          TEXT NOT NULL REFERENCES teams (id),
  message_seq      INTEGER NOT NULL,
  state            TEXT NOT NULL CHECK (state IN ('queued', 'delivering', 'delivered', 'held', 'uncertain', 'denied', 'expired')),
  reason           TEXT CHECK (reason IN ('recipient_blocked', 'recipient_unknown', 'person_control', 'budget', 'rounds', 'delivery_failed', 'no_activity', 'runner_restart', 'turn_cancelled', 'grid', 'team_stopped', 'cancelled')),
  via              TEXT CHECK (via IN ('pane', 'headless', 'human')),
  wake             INTEGER NOT NULL DEFAULT 0 CHECK (wake IN (0, 1)),
  attempts         INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
  notify           INTEGER CHECK (notify IN (0, 1)),
  turn_id          TEXT REFERENCES turns (id),
  lease_generation INTEGER,
  created_at       TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  updated_at       TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  delivered_at     TEXT,
  PRIMARY KEY (message_id, recipient_id),
  CHECK (CASE state
           WHEN 'held'      THEN reason IS NOT NULL AND reason IN ('recipient_blocked', 'recipient_unknown', 'person_control', 'budget', 'rounds', 'delivery_failed')
           WHEN 'uncertain' THEN reason IS NOT NULL AND reason IN ('no_activity', 'runner_restart', 'turn_cancelled')
           WHEN 'denied'    THEN reason IS NOT NULL AND reason IN ('grid')
           WHEN 'expired'   THEN reason IS NOT NULL AND reason IN ('team_stopped', 'cancelled')
           ELSE reason IS NULL
         END),
  CHECK ((state = 'delivered') = (delivered_at IS NOT NULL)),
  CHECK ((via IS 'human') = (notify IS NOT NULL))
) STRICT, WITHOUT ROWID;

CREATE INDEX deliveries_pickup ON deliveries (recipient_id, state, message_seq);
CREATE INDEX deliveries_team_state ON deliveries (team_id, state);
CREATE INDEX deliveries_turn ON deliveries (turn_id) WHERE turn_id IS NOT NULL;

-- Asynchronous actions for the runner: open-terminal, return-headless, stop-member, spawn.
CREATE TABLE requests (
  id            TEXT PRIMARY KEY CHECK (length(id) = 26),
  team_id       TEXT NOT NULL REFERENCES teams (id),
  kind          TEXT NOT NULL CHECK (kind IN ('open-terminal', 'return-headless', 'stop-member', 'spawn')),
  member_id     TEXT REFERENCES members (id),
  requested_by  TEXT NOT NULL REFERENCES members (id),
  args          TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(args) AND json_type(args) = 'object'),
  status        TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'running', 'succeeded', 'failed')),
  error_code    TEXT CHECK (error_code IN ('internal', 'invalid_request', 'unauthenticated', 'unsupported_version', 'not_found', 'unknown_member', 'denied', 'spawn_disabled', 'spawn_cap_reached', 'harness_not_allowed', 'profile_too_high', 'nesting_not_allowed', 'name_taken', 'team_not_running', 'team_running', 'runner_unavailable', 'herdr_unavailable', 'harness_unavailable', 'launch_failed', 'lease_conflict', 'revision_conflict', 'cursor_invalid', 'timeout')),
  error_message TEXT,
  created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  started_at    TEXT,
  finished_at   TEXT,
  CHECK ((status = 'failed') = (error_code IS NOT NULL)),
  CHECK ((status IN ('succeeded', 'failed')) = (finished_at IS NOT NULL))
) STRICT;

-- One open request per member: a second one gets lease_conflict.
CREATE UNIQUE INDEX requests_one_open ON requests (member_id) WHERE status IN ('pending', 'running') AND member_id IS NOT NULL;
CREATE INDEX requests_open ON requests (team_id, created_at) WHERE status IN ('pending', 'running');

-- The event log: the audit record, the source of `watch`, and (where timeline = 1) the
-- team timeline. Append-only. The cursor is "<host_id>.<seq>".
CREATE TABLE events (
  seq        INTEGER PRIMARY KEY AUTOINCREMENT,
  team_id    TEXT REFERENCES teams (id),
  type       TEXT NOT NULL CHECK (type IN ('team.applied', 'team.state_changed', 'runner.state_changed', 'member.added', 'member.state_changed', 'member.activity_changed', 'member.session_added', 'member.removed', 'lease.changed', 'message.created', 'delivery.changed', 'turn.started', 'turn.finished', 'policy.denied', 'budget.changed', 'rounds.changed', 'request.changed', 'auth.rejected', 'worktree.removed')),
  at         TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  timeline   INTEGER NOT NULL DEFAULT 0 CHECK (timeline IN (0, 1)),
  member_id  TEXT REFERENCES members (id),
  message_id TEXT REFERENCES messages (id),
  data       TEXT NOT NULL CHECK (json_valid(data) AND json_type(data) = 'object')
) STRICT;

CREATE INDEX events_team_seq ON events (team_id, seq);
CREATE INDEX events_timeline ON events (team_id, seq) WHERE timeline = 1;

-- Spawn counters, derived so they can't drift. Cap checks read this view inside the same
-- BEGIN IMMEDIATE transaction that inserts the new member.
--   running:  the spawner's workers that are starting, running or stopping (any run)
--   this_run: workers spawned since the team's last start, whatever their state
CREATE VIEW spawn_counts AS
SELECT
  s.member_id AS spawner_id,
  m.team_id   AS team_id,
  t.run       AS run,
  (SELECT count(*) FROM members w
    WHERE w.spawned_by = s.member_id AND w.state IN ('starting', 'running', 'stopping')) AS running,
  (SELECT count(*) FROM members w
    WHERE w.spawned_by = s.member_id AND w.spawned_run = t.run) AS this_run
FROM spawn_settings s
JOIN members m ON m.id = s.member_id
JOIN teams t ON t.id = m.team_id;

INSERT INTO schema_migrations (version, name) VALUES (1, '0001_init');
