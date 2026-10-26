#!/usr/bin/env python3
"""Apply the store migrations to an in-memory SQLite and exercise them.

  1. Applies internal/store/migrations/*.sql in order with PRAGMA foreign_keys=ON and checks
     each records itself in schema_migrations.
  2. Smoke test: a team, the implicit human and two members, a grid, launches and tokens, a
     message and its delivery, a headless turn with an auto-reply, a lease change (open in
     terminal), a message to `human`, a worktree and a hold, then the reads the runner and
     the app ops make.
  3. Constraints that must refuse bad rows do refuse them.
  4. The pickup, timeline and live-turn queries use their indexes.
  5. Every enum CHECK in the SQL lists exactly the values of its enum in
     schema/common.v1.schema.json.

Python's sqlite3 module only; no sqlite3 CLI needed. Exit status 0 when everything passes.
Usage: scripts/check-migrations.py [repo-root]
"""
from __future__ import annotations

import hashlib
import json
import re
import sqlite3
import sys
from pathlib import Path

ROOT = Path(sys.argv[1]) if len(sys.argv) > 1 else Path(__file__).resolve().parent.parent
MIGRATIONS = ROOT / "internal" / "store" / "migrations"
COMMON = ROOT / "schema" / "common.v1.schema.json"

failures: list[str] = []


def fail(msg: str) -> None:
    failures.append(msg)
    print(f"FAIL  {msg}")


def ok(msg: str) -> None:
    print(f"ok    {msg}")


print(f"sqlite {sqlite3.sqlite_version}")
db = sqlite3.connect(":memory:", isolation_level=None)
db.execute("PRAGMA foreign_keys = ON")

# 1. Migrations -----------------------------------------------------------------------------
files = sorted(MIGRATIONS.glob("[0-9][0-9][0-9][0-9]_*.sql"))
sql_text = ""
for path in files:
    version = int(path.name[:4])
    body = path.read_text()
    sql_text += body
    # One transaction per file, as the Go migrator does it.
    try:
        db.executescript("BEGIN;\n" + body + "\nCOMMIT;")
    except sqlite3.Error as exc:
        if db.in_transaction:
            db.execute("ROLLBACK")
        fail(f"{path.name}: {exc}")
        break
    got = db.execute("SELECT max(version) FROM schema_migrations").fetchone()[0]
    if got != version:
        fail(f"{path.name}: schema_migrations max(version) is {got}, expected {version}")
    else:
        ok(f"{path.name} applied")

fk = db.execute("PRAGMA foreign_key_check").fetchall()
if fk:
    fail(f"foreign_key_check after migrations: {fk}")

# 2. Smoke test -----------------------------------------------------------------------------
NOW = "2026-10-26T14:00:00.000Z"
TEAM = "fmvq4b2z1d8"
HUMAN, WRITER, REVIEWER = "01M5W85RKG4MNPZX45HY43KWJR", "fmvq4b2z3e1", "fmvq4b2z5g6"
L_W1, L_R1, L_R2 = "01M5W85T2CP1XPA7Z3DJ8FSSZ5", "01M5W85VH8AWSH8VHTPRE95B9E", "01M5W85X04E0ZBGJ09TQM83XSS"
MSG1, MSG2 = "01M5W861CRYYK596NGYA1DQ91K", "01M5W865SCDY0YP57RCYBVN5SX"
TURN1 = "01M5W862VM5GQAPENECFSECZP1"
MSG3, WT1 = "01M5W86HGCE65GH2BH8VGS9ZM5", "01M5W86JZ8H3BV4H15G5E4G7X0"
SEAL_W, SEAL_R = "7KQ2M9XA", "C4HT0W3N"


def h(token: str) -> str:
    return hashlib.sha256(token.encode()).hexdigest()


def tx(fn):
    db.execute("BEGIN IMMEDIATE")
    try:
        fn()
        db.execute("COMMIT")
    except Exception:
        db.execute("ROLLBACK")
        raise


def event(type_, data, timeline=0, member=None, message=None):
    db.execute(
        "INSERT INTO events (team_id, type, timeline, member_id, message_id, data) VALUES (?,?,?,?,?,?)",
        (TEAM, type_, timeline, member, message, json.dumps(data)),
    )


def apply_team():
    db.execute(
        "INSERT INTO teams (id, name, pattern, spec_version) VALUES (?, 'login-fix', 'writer_reviewer', 1)",
        (TEAM,),
    )
    db.execute("INSERT INTO hop_counters (team_id, allowance) VALUES (?, 16)", (TEAM,))
    db.execute(
        "INSERT INTO members (id, team_id, kind, name, origin, state) VALUES (?, ?, 'human', 'human', 'implicit', 'running')",
        (HUMAN, TEAM),
    )
    for mid, name, role, harness, profile, mode in [
        (WRITER, "writer", "writer", "claude", "workspace", "pane"),
        (REVIEWER, "reviewer", "reviewer", "codex", "read-only", "headless"),
    ]:
        db.execute(
            """INSERT INTO members (id, team_id, kind, name, role, origin, position, harness, profile,
                                    mode, folder, brief)
               VALUES (?, ?, 'agent', ?, ?, 'spec', ?, ?, ?, ?, '/home/lex/code/flare', 'brief')""",
            (mid, TEAM, name, role, 0 if name == "writer" else 1, harness, profile, mode),
        )
        db.execute("INSERT INTO leases (member_id) VALUES (?)", (mid,))
    db.executemany(
        "INSERT INTO grid (team_id, from_id, to_id, capability) VALUES (?,?,?,?)",
        [(TEAM, WRITER, REVIEWER, "message"), (TEAM, WRITER, REVIEWER, "wake"),
         (TEAM, REVIEWER, WRITER, "message")],
    )
    event("team.applied", {"revision": 1}, timeline=1)


def start_team():
    db.execute("UPDATE teams SET run = run + 1, state = 'running', runner_state = 'up' WHERE id = ?", (TEAM,))
    for mid, launch, driver, session, pane in [
        (WRITER, L_W1, "pane", "7d0c1e52-8f3a-4b6e-9a21-5c4f0b9e2d17", "pane-3"),
        (REVIEWER, L_R1, "headless", None, None),
    ]:
        db.execute(
            "INSERT INTO launches (id, member_id, generation, driver, session_id_start, pane_id) VALUES (?,?,1,?,?,?)",
            (launch, mid, driver, session, pane),
        )
        db.execute("INSERT INTO tokens (launch_id, member_id, hash) VALUES (?,?,?)", (launch, mid, h("tok-" + launch)))
        db.execute(
            "UPDATE leases SET driver = ?, generation = generation + 1, launch_id = ?, pane_id = ? WHERE member_id = ? AND generation = 0",
            (driver, launch, pane, mid),
        )
        db.execute(
            "UPDATE members SET state = 'running', last_driver = ?, session_id = ?, started_at = ? WHERE id = ?",
            (driver, session, NOW, mid),
        )
        if session:
            db.execute(
                "INSERT INTO member_sessions (member_id, ordinal, session_id, harness, source, launch_id, seal) VALUES (?,1,?,?,'assigned',?,?)",
                (mid, session, "claude", launch, SEAL_W),
            )
        event("lease.changed", {"from": "none", "to": driver, "cause": "start"}, timeline=1, member=mid)


def writer_sends():
    used = db.execute("UPDATE hop_counters SET used = used + 1 WHERE team_id = ? RETURNING used", (TEAM,)).fetchone()[0]
    db.execute("INSERT INTO round_counters (team_id, writer_id, reviewer_id, used, allowance) VALUES (?,?,?,1,3)",
               (TEAM, WRITER, REVIEWER))
    seq = db.execute(
        """INSERT INTO messages (id, team_id, run, sender_id, kind, text, hop, round)
           VALUES (?, ?, 1, ?, 'message', 'Please review lugarn/login-fix/writer.', ?, 1) RETURNING seq""",
        (MSG1, TEAM, WRITER, used),
    ).fetchone()[0]
    db.execute(
        "INSERT INTO deliveries (message_id, recipient_id, team_id, message_seq, state, wake) VALUES (?,?,?,?, 'queued', 1)",
        (MSG1, REVIEWER, TEAM, seq),
    )
    event("message.created", {"message": MSG1}, timeline=1, message=MSG1)


def reviewer_turn():
    # A Codex first turn: the seal is drawn now and kept on the turn until the thread id arrives.
    db.execute(
        "INSERT INTO turns (id, team_id, member_id, launch_id, lease_generation, harness, pid, seal) VALUES (?,?,?,?,1,'codex',4242,?)",
        (TURN1, TEAM, REVIEWER, L_R1, SEAL_R),
    )
    db.execute(
        "UPDATE deliveries SET state = 'delivering', via = 'headless', attempts = attempts + 1, turn_id = ?, lease_generation = 1 WHERE message_id = ? AND recipient_id = ? AND state = 'queued'",
        (TURN1, MSG1, REVIEWER),
    )
    # thread.started: the harness acknowledged the prompt.
    thread = "019a2f3c-5b7d-7e10-8c42-6d1f0a3b9e55"
    db.execute("UPDATE turns SET status = 'running' WHERE id = ?", (TURN1,))
    db.execute(
        "UPDATE deliveries SET state = 'delivered', delivered_at = ? WHERE turn_id = ? AND state = 'delivering'",
        (NOW, TURN1),
    )
    db.execute("UPDATE members SET session_id = ?, activity = 'working' WHERE id = ?", (thread, REVIEWER))
    db.execute(
        """INSERT INTO member_sessions (member_id, ordinal, session_id, harness, source, launch_id, seal, briefed_at)
           SELECT ?, 1, ?, 'codex', 'reported', ?, seal, ? FROM turns WHERE id = ?""",
        (REVIEWER, thread, L_R1, NOW, TURN1),
    )


def reviewer_turn_ends():
    db.execute(
        """UPDATE turns SET status = 'succeeded', exit_code = 0, ended_at = ?, session_id_end = ?,
                            input_tokens = 48211, output_tokens = 3120, cached_input_tokens = 40960
           WHERE id = ?""",
        (NOW, "019a2f3c-5b7d-7e10-8c42-6d1f0a3b9e55", TURN1),
    )
    used = db.execute("UPDATE hop_counters SET used = used + 1 WHERE team_id = ? RETURNING used", (TEAM,)).fetchone()[0]
    seq = db.execute(
        """INSERT INTO messages (id, team_id, run, sender_id, kind, reply_to, text, hop, turn_id)
           VALUES (?, ?, 1, ?, 'auto_reply', ?, 'Two problems: ...', ?, ?) RETURNING seq""",
        (MSG2, TEAM, REVIEWER, MSG1, used, TURN1),
    ).fetchone()[0]
    db.execute(
        "INSERT INTO deliveries (message_id, recipient_id, team_id, message_seq, state) VALUES (?,?,?,?, 'queued')",
        (MSG2, WRITER, TEAM, seq),
    )
    db.execute("UPDATE members SET activity = 'idle' WHERE id = ?", (REVIEWER,))
    event("message.created", {"message": MSG2}, timeline=1, message=MSG2)


def open_terminal_reviewer():
    gen = db.execute("SELECT generation FROM leases WHERE member_id = ?", (REVIEWER,)).fetchone()[0]
    db.execute("UPDATE launches SET ended_at = ?, end_reason = 'converted' WHERE id = ?", (NOW, L_R1))
    db.execute("UPDATE tokens SET revoked_at = ? WHERE launch_id = ?", (NOW, L_R1))
    db.execute(
        "INSERT INTO launches (id, member_id, generation, driver, session_id_start, pane_id) VALUES (?,?,?,?,?,?)",
        (L_R2, REVIEWER, gen + 1, "pane", "019a2f3c-5b7d-7e10-8c42-6d1f0a3b9e55", "pane-7"),
    )
    db.execute("INSERT INTO tokens (launch_id, member_id, hash) VALUES (?,?,?)", (L_R2, REVIEWER, h("tok-" + L_R2)))
    changed = db.execute(
        "UPDATE leases SET driver = 'pane', generation = generation + 1, launch_id = ?, pane_id = 'pane-7' WHERE member_id = ? AND generation = ?",
        (L_R2, REVIEWER, gen),
    ).rowcount
    if changed != 1:
        raise RuntimeError("lease generation check failed")
    db.execute("UPDATE members SET last_driver = 'pane' WHERE id = ?", (REVIEWER,))
    event("lease.changed", {"from": "headless", "to": "pane", "cause": "open_terminal"}, timeline=1, member=REVIEWER)


RATE_LIMIT_SQL = """SELECT count(*) FROM messages m
   WHERE m.sender_id = ? AND m.created_at > ?
     AND EXISTS (SELECT 1 FROM deliveries d WHERE d.message_id = m.id AND d.recipient_id = ?)"""


def reviewer_tells_human():
    # Rate limit: count this sender's messages to human in the last 10 minutes.
    recent = db.execute(RATE_LIMIT_SQL, (REVIEWER, "2026-10-26T13:50:00.000Z", HUMAN)).fetchone()[0]
    seq = db.execute(
        "INSERT INTO messages (id, team_id, run, sender_id, kind, text) VALUES (?, ?, 1, ?, 'message', 'Writer and I disagree on the retry policy.') RETURNING seq",
        (MSG3, TEAM, REVIEWER),
    ).fetchone()[0]
    db.execute(
        "INSERT INTO deliveries (message_id, recipient_id, team_id, message_seq, state, via, notify, delivered_at) VALUES (?,?,?,?, 'delivered', 'human', ?, ?)",
        (MSG3, HUMAN, TEAM, seq, 1 if recent < 5 else 0, NOW),
    )


def writer_worktree_and_hold():
    db.execute(
        """INSERT INTO worktrees (id, team_id, member_id, repo, path, branch, base)
           VALUES (?, ?, ?, '/home/lex/code/flare', '/home/lex/code/flare-wt/login-fix-writer', 'lugarn/login-fix/writer', 'main')""",
        (WT1, TEAM, WRITER),
    )
    db.execute("UPDATE members SET worktree = 1, worktree_id = ? WHERE id = ?", (WT1, WRITER))
    db.execute(
        "INSERT INTO holds (member_id, team_id, held_until) VALUES (?, ?, '2026-10-26T14:01:00.000Z')",
        (WRITER, TEAM),
    )


try:
    for step in (apply_team, start_team, writer_sends, reviewer_turn, reviewer_turn_ends,
                 open_terminal_reviewer, reviewer_tells_human, writer_worktree_and_hold):
        tx(step)
        ok(f"smoke: {step.__name__}")
except Exception as exc:
    fail(f"smoke: {exc}")

# Reads.
lease = db.execute("SELECT driver, generation, pane_id FROM leases WHERE member_id = ?", (REVIEWER,)).fetchone()
if lease != ("pane", 2, "pane-7"):
    fail(f"reviewer lease is {lease}, expected ('pane', 2, 'pane-7')")
else:
    ok("lease moved headless -> pane at generation 2")

active = db.execute("SELECT launch_id FROM tokens WHERE member_id = ? AND revoked_at IS NULL", (REVIEWER,)).fetchall()
if active != [(L_R2,)]:
    fail(f"reviewer active tokens {active}")
else:
    ok("old launch token revoked, new one active")

pickup = db.execute(
    """SELECT d.message_id FROM deliveries d
       WHERE d.recipient_id = ? AND d.state = 'queued' ORDER BY d.message_seq LIMIT 10""",
    (WRITER,),
).fetchall()
if pickup != [(MSG2,)]:
    fail(f"pickup for writer returned {pickup}")
else:
    ok("pickup finds the auto-reply queued for writer")

turn = db.execute(
    """SELECT t.status, (SELECT group_concat(message_id) FROM deliveries WHERE turn_id = t.id),
              (SELECT group_concat(id) FROM messages WHERE turn_id = t.id AND kind = 'auto_reply')
       FROM turns t WHERE t.id = ?""",
    (TURN1,),
).fetchone()
if turn != ("succeeded", MSG1, MSG2):
    fail(f"turn record {turn}")
else:
    ok("turn record: consumed message and auto-reply resolve")

timeline = db.execute(
    "SELECT type FROM events WHERE team_id = ? AND timeline = 1 AND seq < 1000 ORDER BY seq DESC LIMIT 50", (TEAM,)
).fetchall()
if len(timeline) != 6:
    fail(f"timeline returned {len(timeline)} items, expected 6")
else:
    ok("timeline page reads 6 items")

sessions = db.execute("SELECT session_id, source FROM member_sessions WHERE member_id = ? ORDER BY ordinal", (REVIEWER,)).fetchall()
ok(f"reviewer session history: {sessions}") if len(sessions) == 1 else fail(f"sessions {sessions}")

notify = db.execute("SELECT notify FROM deliveries WHERE message_id = ?", (MSG3,)).fetchone()
ok("message to human delivered with notify = 1") if notify == (1,) else fail(f"human delivery notify {notify}")

seal = db.execute("SELECT seal FROM member_sessions WHERE member_id = ?", (REVIEWER,)).fetchone()
ok("Codex session row took the seal its first turn was briefed with") if seal == (SEAL_R,) else fail(f"seal {seal}")

wt = db.execute(
    """SELECT w.path, m.state FROM worktrees w JOIN members m ON m.id = w.member_id
       WHERE w.team_id = ? AND w.removed_at IS NULL""", (TEAM,)).fetchall()
ok("worktrees listing joins the member") if wt == [("/home/lex/code/flare-wt/login-fix-writer", "running")] else fail(f"worktrees {wt}")

hold = db.execute(
    "SELECT 1 FROM holds WHERE member_id = ? AND released_at IS NULL AND held_until > ?",
    (WRITER, NOW)).fetchone()
ok("live hold found for writer") if hold == (1,) else fail("no live hold")

counts = db.execute("SELECT * FROM spawn_counts").fetchall()
ok("spawn_counts view queries (no spawners in this team)") if counts == [] else fail(f"spawn_counts {counts}")

fk = db.execute("PRAGMA foreign_key_check").fetchall()
ok("foreign_key_check clean") if not fk else fail(f"foreign_key_check: {fk}")

# 3. Constraints that must refuse -----------------------------------------------------------
def must_fail(label: str, sql: str, args=(), expect: str = "") -> None:
    """The statement must raise IntegrityError, and its message must contain `expect`."""
    db.execute("SAVEPOINT neg")
    try:
        db.execute(sql, args)
    except sqlite3.IntegrityError as exc:
        if expect in str(exc):
            ok(f"refuses: {label}")
        else:
            fail(f"refuses {label} for the wrong reason: {exc}")
    else:
        fail(f"accepted: {label}")
    finally:
        db.execute("ROLLBACK TO neg")
        db.execute("RELEASE neg")


must_fail("held delivery without a reason",
          "UPDATE deliveries SET state = 'held', reason = NULL WHERE message_id = ?", (MSG2,), expect="CHECK")
must_fail("queued delivery with a reason",
          "UPDATE deliveries SET reason = 'budget' WHERE message_id = ?", (MSG2,), expect="CHECK")
must_fail("uncertain delivery with a held reason",
          "UPDATE deliveries SET state = 'uncertain', reason = 'budget' WHERE message_id = ?", (MSG2,), expect="CHECK")
must_fail("delivered without delivered_at",
          "UPDATE deliveries SET state = 'delivered' WHERE message_id = ?", (MSG2,), expect="CHECK")
must_fail("second live turn for one member",
          "INSERT INTO turns (id, team_id, member_id, launch_id, lease_generation, harness) VALUES ('01M5W868Q48VCD1GDJFMGT83PX', ?, ?, ?, 2, 'claude'),"
          " ('01M5W86A60T891WB09B9Y73MY6', ?, ?, ?, 2, 'claude')",
          (TEAM, WRITER, L_W1, TEAM, WRITER, L_W1), expect="turns.member_id")
L_SPARE = "01M5W86G1GTCPM5Q1NXW1RNJ47"
db.execute("INSERT INTO launches (id, member_id, generation, driver, ended_at, end_reason) VALUES (?, ?, 9, 'pane', ?, 'stopped')",
           (L_SPARE, WRITER, NOW))
must_fail("second active token for one member",
          "INSERT INTO tokens (launch_id, member_id, hash) VALUES (?, ?, ?)", (L_SPARE, WRITER, h("x")),
          expect="tokens.member_id")
must_fail("duplicate live member name",
          "INSERT INTO members (id, team_id, kind, name, role, origin, harness, profile, mode, folder, brief) "
          "VALUES ('dup', ?, 'agent', 'writer', 'worker', 'spec', 'pi', 'workspace', 'pane', '/tmp', 'b')", (TEAM,), expect="members.team_id, members.name")
must_fail("agent named human",
          "INSERT INTO members (id, team_id, kind, name, role, origin, harness, profile, mode, folder, brief) "
          "VALUES ('h2', ?, 'agent', 'human', 'worker', 'spec', 'pi', 'workspace', 'pane', '/tmp', 'b')", (TEAM,), expect="CHECK")
must_fail("spawned member without spawned_run",
          "INSERT INTO members (id, team_id, kind, name, role, origin, spawned_by, harness, profile, mode, folder, brief) "
          "VALUES ('s1', ?, 'agent', 'w9', 'worker', 'spawn', ?, 'pi', 'workspace', 'pane', '/tmp', 'b')", (TEAM, WRITER), expect="CHECK")
must_fail("grid self edge",
          "INSERT INTO grid (team_id, from_id, to_id, capability) VALUES (?, ?, ?, 'message')", (TEAM, WRITER, WRITER), expect="CHECK")
must_fail("message to an unknown member (foreign key)",
          "INSERT INTO deliveries (message_id, recipient_id, team_id, message_seq, state) VALUES (?, 'nobody', ?, 1, 'queued')",
          (MSG1, TEAM), expect="FOREIGN KEY")
must_fail("lease driver none with a launch",
          "UPDATE leases SET driver = 'none' WHERE member_id = ?", (WRITER,), expect="CHECK")
must_fail("auto_reply without reply_to",
          "INSERT INTO messages (id, team_id, run, sender_id, kind, text, turn_id) VALUES ('01M5W86BMW3FCH26W14WMCHWYF', ?, 1, ?, 'auto_reply', 't', ?)",
          (TEAM, REVIEWER, TURN1), expect="CHECK")
must_fail("stop_reason on a running member",
          "UPDATE members SET stop_reason = 'person' WHERE id = ?", (WRITER,), expect="CHECK")
must_fail("member host set in v1",
          "UPDATE members SET host = 'ufo' WHERE id = ?", (WRITER,), expect="CHECK")
must_fail("unknown event type",
          "INSERT INTO events (team_id, type, data) VALUES (?, 'message.sent', '{}')", (TEAM,), expect="CHECK")
must_fail("session seal that isn't 8 Crockford base32 chars",
          "UPDATE member_sessions SET seal = 'abcd1234' WHERE member_id = ?", (WRITER,), expect="CHECK")
must_fail("notify on a delivery to an agent",
          "UPDATE deliveries SET notify = 1 WHERE message_id = ?", (MSG2,), expect="CHECK")
must_fail("delivery to human without notify",
          "UPDATE deliveries SET notify = NULL WHERE message_id = ?", (MSG3,), expect="CHECK")
must_fail("second live worktree on one path",
          "INSERT INTO worktrees (id, team_id, member_id, repo, path, branch, base) VALUES ('01M5W86ME4NTH82F7AG3BCKKDJ', ?, ?, '/r', '/home/lex/code/flare-wt/login-fix-writer', 'b', 'main')",
          (TEAM, REVIEWER), expect="worktrees.path")
must_fail("second open request for one member",
          "INSERT INTO requests (id, team_id, kind, member_id, requested_by) VALUES ('01M5W86D3RGCW8T7SWM4FV4DK7', ?, 'stop-member', ?, ?),"
          " ('01M5W86EJM9Q9G8XE6SZAEAVSN', ?, 'open-terminal', ?, ?)",
          (TEAM, WRITER, HUMAN, TEAM, WRITER, HUMAN), expect="requests.member_id")

# 4. Query plans ----------------------------------------------------------------------------
def plan_uses(label: str, sql: str, index: str, args=()) -> None:
    plan = " | ".join(r[3] for r in db.execute("EXPLAIN QUERY PLAN " + sql, args))
    ok(f"plan: {label} uses {index}") if index in plan else fail(f"plan: {label}: {plan}")


plan_uses("runner pickup",
          "SELECT message_id FROM deliveries WHERE recipient_id = ? AND state = 'queued' ORDER BY message_seq LIMIT 10",
          "deliveries_pickup", (WRITER,))
plan_uses("timeline page",
          "SELECT seq FROM events WHERE team_id = ? AND timeline = 1 AND seq < ? ORDER BY seq DESC LIMIT 50",
          "events_timeline", (TEAM, 10**9))
plan_uses("watch replay",
          "SELECT seq FROM events WHERE team_id = ? AND seq > ? ORDER BY seq LIMIT 500",
          "events_team_seq", (TEAM, 0))
plan_uses("concurrent headless turns",
          "SELECT count(*) FROM turns WHERE team_id = ? AND status IN ('starting', 'running')",
          "turns_team_live", (TEAM,))
plan_uses("agent inbox (in)",
          "SELECT message_id FROM deliveries WHERE recipient_id = ? AND state IN ('delivered', 'uncertain') ORDER BY message_seq DESC LIMIT 20",
          "deliveries_pickup", (WRITER,))
plan_uses("token lookup", "SELECT launch_id FROM tokens WHERE hash = ?", "sqlite_autoindex_tokens", (h("x"),))
plan_uses("messages to human rate limit", RATE_LIMIT_SQL, "messages_sender_created", (REVIEWER, NOW, HUMAN))
plan_uses("worktrees listing", "SELECT path FROM worktrees WHERE team_id = ? AND removed_at IS NULL",
          "worktrees_team", (TEAM,))

# 5. Enum CHECKs against schema/common.v1.schema.json ----------------------------------------
common = json.loads(COMMON.read_text())["$defs"]
ENUM_COLUMNS = {
    ("teams", "pattern"): "Pattern",
    ("teams", "state"): "TeamState",
    ("teams", "runner_state"): "RunnerState",
    ("members", "kind"): "MemberKind",
    ("members", "role"): "Role",
    ("members", "origin"): "MemberOrigin",
    ("members", "harness"): "Harness",
    ("members", "profile"): "Profile",
    ("members", "mode"): "Mode",
    ("members", "state"): "MemberState",
    ("members", "stop_reason"): "StopReason",
    ("members", "activity"): "Activity",
    ("members", "last_driver"): "Mode",
    ("members", "removed_reason"): "RemovedReason",
    ("spawn_settings", "mode"): "Mode",
    ("spawn_settings", "folder"): "SpawnFolder",
    ("spawn_settings", "max_profile"): "Profile",
    ("grid", "capability"): "GridCapability",
    ("launches", "driver"): "Mode",
    ("launches", "end_reason"): "LaunchEndReason",
    ("leases", "driver"): "Driver",
    ("member_sessions", "harness"): "Harness",
    ("member_sessions", "source"): "SessionSource",
    ("turns", "harness"): "Harness",
    ("turns", "status"): "TurnStatus",
    ("messages", "kind"): "MessageKind",
    ("deliveries", "state"): "DeliveryState",
    ("deliveries", "reason"): "DeliveryReason",
    ("deliveries", "via"): "DeliveryVia",
    ("requests", "kind"): "RequestKind",
    ("requests", "status"): "RequestStatus",
    ("requests", "error_code"): "ErrorCode",
    ("events", "type"): "EventType",
}
tables = {m.group(1): m.group(2) for m in re.finditer(r"CREATE TABLE (\w+) \((.*?)\n\)", sql_text, re.S)}
seen = set()
for (table, column), enum in ENUM_COLUMNS.items():
    body = tables.get(table)
    if body is None:
        fail(f"enum check: table {table} not found")
        continue
    m = re.search(rf"CHECK \(\s*{column}\s+IN\s*\(([^)]*)\)\s*\)", body)
    if not m:
        fail(f"enum check: {table}.{column} has no simple IN (...) CHECK")
        continue
    values = re.findall(r"'([^']*)'", m.group(1))
    if values != common[enum]["enum"]:
        fail(f"enum check: {table}.{column} {values} != {enum} {common[enum]['enum']}")
    else:
        seen.add((table, column))
# Any other simple enum CHECK in the SQL must be in the map, so nothing goes unchecked.
for table, body in tables.items():
    for m in re.finditer(r"CHECK \(\s*(\w+)\s+IN\s*\(\s*'", body):
        if (table, m.group(1)) not in ENUM_COLUMNS:
            fail(f"enum check: {table}.{m.group(1)} has an enum CHECK that isn't mapped to a schema enum")
if len(seen) == len(ENUM_COLUMNS):
    ok(f"SQL enum CHECKs match schema/common.v1.schema.json ({len(seen)} columns)")

print()
print(f"{len(files)} migration(s), {len(failures)} failure(s)")
sys.exit(1 if failures else 0)
