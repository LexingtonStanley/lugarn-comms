#!/usr/bin/env python3
"""Opt-in real #150 regression: python3 scripts/test-herdr-150.py --run.

Uses only a fresh lugarn-test-150-<random> server and plain shell harnesses. Refuses an
existing session, builds the real broker, records fixtures, and cleans up in
finally. The only unscoped Herdr command is the read-only session list.
"""
import json
import secrets
import os
from pathlib import Path
import shlex
import shutil
import subprocess
import sys
import tempfile
import time

ROOT = Path(__file__).resolve().parent.parent
FIXTURES = ROOT / "internal/broker/testdata/herdr-150"
HERDR = shutil.which("herdr")
SESSION = "lugarn-test-150-" + secrets.token_hex(6)


def command(argv, **kwargs):
    return subprocess.run(argv, check=True, text=True, capture_output=True,
                          timeout=60, **kwargs).stdout


def sessions():
    return json.loads(command([HERDR, "session", "list", "--json"]))


def save(name, data):
    (FIXTURES / name).write_text(json.dumps(data, indent=2) + "\n")


def main():
    if sys.argv[1:] != ["--run"]:
        sys.exit(__doc__)
    if not HERDR:
        sys.exit("herdr is not installed")
    before = sessions()
    if any(s["name"] == SESSION for s in before["sessions"]):
        sys.exit("refusing existing test session; leave it to its owner")
    FIXTURES.mkdir(parents=True, exist_ok=True)
    save("sessions-before.json", before)
    scratch = Path(tempfile.mkdtemp(prefix=".herdr-150-", dir=ROOT))
    server = None
    try:
        for name in ["home", "state", "work", "bin"]:
            (scratch / name).mkdir()
        home = scratch / "home"
        for name in [".zshenv", ".zprofile", ".zshrc", ".zlogin", ".bashrc", ".profile"]:
            (home / name).write_text("")
        binary = scratch / "lugarn-comms"
        command(["go", "build", "-p", "4", "-o", str(binary), "./cmd/lugarn-comms"], cwd=ROOT)
        helper = scratch / "bin/pi"
        helper.write_text("""#!/bin/sh
if [ "$1" = --version ]; then echo 'pi 0.63.0'; exit 0; fi
report() { %s --session %s pane report-agent "$HERDR_PANE_ID" --source lugarn-150 --agent pi --state "$1" >/dev/null; }
report idle
printf 'pi test shell ready\\n'
while IFS= read -r line; do
 printf '%%s\\n' "$line"
 case "$line" in
 *CHECK150*)
  report working
  if [ -n "$LUGARN_COMMS_MEMBER" ]; then
   %s send human 'CHECK150 received by plain shell' >/dev/null
  fi
  sleep 1
  report idle
  ;;
 esac
done
""" % (shlex.quote(HERDR), shlex.quote(SESSION), shlex.quote(str(binary))))
        helper.chmod(0o700)
        original_config = os.environ.get("XDG_CONFIG_HOME", str(Path.home() / ".config"))
        env = dict(os.environ, HOME=str(home), ZDOTDIR=str(home),
                   XDG_CONFIG_HOME=original_config, SHELL="/bin/sh",
                   PATH=str(scratch / "bin") + os.pathsep + os.environ["PATH"],
                   LUGARN_COMMS_HOME=str(scratch / "state"),
                   LUGARN_COMMS_POINTER_HOME=str(home))
        log = (scratch / "server.log").open("w")
        server = subprocess.Popen([HERDR, "--session", SESSION, "server"],
                                  stdin=subprocess.DEVNULL, stdout=log, stderr=log,
                                  start_new_session=True)

        def herdr(*args):
            return json.loads(command([HERDR, "--session", SESSION, *args]))

        def app(op, **args):
            raw = command([str(binary), "app", op], env=env,
                          input=json.dumps(dict(protocol=1, op=op, **args)))
            response = json.loads(raw)
            if not response.get("ok"):
                raise AssertionError(response)
            return response["result"]

        def until(label, fn):
            deadline = time.monotonic() + 55
            while time.monotonic() < deadline:
                value = fn()
                if value:
                    return value
                time.sleep(.3)
            raise AssertionError(label + " timed out")

        def snapshot():
            return {kind: herdr(kind, "list") for kind in ["workspace", "tab", "pane", "agent"]}

        def spec(team, names, placement="tabs"):
            return dict(version=1, id=team, name=team, display_name="Team " + team.upper(),
                        pattern="custom", herdr=dict(server=SESSION, placement=placement),
                        members=[dict(id=team + "-" + n, name=n, role="custom", harness="pi",
                                      folder=str(scratch / "work"), profile="workspace", mode="pane",
                                      brief="Use the CLI for team messages.", task=None, host=None)
                                 for n in names], grid=[])

        def running_members(team):
            ms = [m for m in app("members", team=team)["members"] if m["kind"] == "agent"]
            if any(m["stop_reason"] == "launch_failed" for m in ms):
                raise AssertionError(app("timeline", team=team))
            return ms if all(m["state"] == "running" for m in ms) else None

        until("server", lambda: json.loads(command([HERDR, "--session", SESSION, "status", "--json"]))["server"]["running"])
        shell_env = ["--env", "HOME=" + str(home), "--env", "ZDOTDIR=" + str(home)]
        created = herdr("workspace", "create", "--label", "Existing", "--cwd", str(ROOT), *shell_env, "--no-focus")["result"]
        workspace = created["workspace"]["workspace_id"]
        selected = [("alpha", created["root_pane"]["pane_id"])]
        tab = herdr("tab", "create", "--workspace", workspace, "--label", "beta", "--cwd", str(ROOT), *shell_env, "--no-focus")["result"]
        selected.append(("beta", tab["root_pane"]["pane_id"]))
        for name, pane in selected:
            command([HERDR, "--session", SESSION, "pane", "run", pane, shlex.quote(str(helper))])
            until("external shell", lambda: any(a["pane_id"] == pane for a in herdr("agent", "list")["result"]["agents"]))
            herdr("agent", "rename", pane, name)
        for team, placement in [("a", "tabs"), ("b", "workspaces")]:
            app("apply-team", spec=spec(team, ["writer"], placement), if_revision=0)
            save(team + "-start.json", app("start-team", team=team))
            ms = until("members " + team, lambda: running_members(team))
            assert ms[0]["herdr_name"] == ("writer" if team == "a" else "writer-2")
            save(team + "-members.json", ms)
        save("placements-agents.json", herdr("agent", "list"))
        save("placements-workspaces.json", herdr("workspace", "list"))
        old = snapshot()
        app("apply-team", spec=spec("a", ["writer"], "workspaces"), if_revision=1)
        assert old == snapshot(), "placement edit changed live panes"
        app("stop-team", team="a")
        app("start-team", team="a", resume=True)
        ms = until("restart", lambda: running_members("a"))
        assert ms[0]["herdr_name"] == "writer"
        save("a-restart-members.json", ms)
        before_adopt = snapshot()
        save("adopt-before.json", before_adopt)
        adopted = spec("adopted", ["alpha", "beta"])
        for member in adopted["members"]:
            member["folder"] = str(ROOT)
        save("adopt-result.json", app("adopt-team", spec=adopted, workspace=workspace,
                                      members=[dict(member="adopted-" + n, pane=p, agent=n) for n, p in selected]))
        after_adopt = snapshot()
        save("adopt-after.json", after_adopt)
        assert before_adopt == after_adopt, "adoption mutated server"
        save("adopt-members.json", app("members", team="adopted"))
        preview = app("bring-onto-comms", team="adopted", member="adopted-alpha")
        assert preview["conversation"] == "fresh" and preview["request"] is None
        save("bring-preview.json", preview)
        app("start-team", team="adopted", resume=True)
        beta = next(a for a in herdr("agent", "list")["result"]["agents"] if a.get("name") == "beta")
        save("bring-confirm.json", app("bring-onto-comms", team="adopted", member="adopted-alpha",
                                       confirm=True, if_generation=0, expected_session=None))

        def converted():
            ms = app("members", team="adopted")["members"]
            return ms if any(m["id"] == "adopted-alpha" and m["state"] == "running" for m in ms) else None

        ms = until("conversion", converted)
        save("brought-members.json", ms)
        # Closing the selected pane may change Herdr's focus, but must preserve beta's identity.
        beta_after = next(a for a in herdr("agent", "list")["result"]["agents"] if a.get("name") == "beta")
        for key in ["pane_id", "terminal_id", "agent", "cwd", "state_change_seq"]:
            assert beta[key] == beta_after[key], "conversion disturbed beta"
        save("message.json", app("send", team="adopted", to="adopted-alpha", text="CHECK150"))

        def delivered():
            timeline = app("timeline", team="adopted")
            messages = [e["data"]["message"] for e in timeline["events"] if e["type"] == "message.created"]
            sent = any(m["text"] == "CHECK150" and m["deliveries"][0]["state"] == "delivered" for m in messages)
            reply = any(m["text"] == "CHECK150 received by plain shell" for m in messages)
            return timeline if sent and reply else None

        save("message-timeline.json", until("message and authenticated reply", delivered))
        before_forget = herdr("agent", "list")
        save("forget-agents-before.json", before_forget)
        save("forget-result.json", app("forget-team", team="adopted", if_revision=1))
        after_forget = herdr("agent", "list")
        save("forget-agents-after.json", after_forget)
        assert before_forget == after_forget, "forget changed agents"
        assert herdr("workspace", "get", workspace)["result"]["workspace"]["workspace_id"] == workspace
        for team in ["a", "b"]:
            app("stop-team", team=team)
        print("PASS: names, collision, restart, placements, adoption, conversion, message/reply, forget")
    finally:
        if server is not None:
            try:
                command([HERDR, "--session", SESSION, "server", "stop"])
                server.wait(timeout=10)
                command([HERDR, "session", "delete", SESSION])
            finally:
                log.close()
        after = sessions()
        save("sessions-after.json", after)
        print("sessions before:", json.dumps(before))
        print("sessions after:", json.dumps(after))
        shutil.rmtree(scratch)


if __name__ == "__main__":
    main()
