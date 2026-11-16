Herdr 0.9.5, lexbox, 2026-11-16. Captured by `python3 scripts/test-herdr-150.py --run`
on the dedicated `lugarn-150` server, using the real broker binary and plain shell
programs registered as pi agents. No model request or signed-in harness was used.

`adopt-before.json` and `adopt-after.json` are identical workspace/tab/pane/agent
lists. `a-members.json` and `b-members.json` show `writer` and `writer-2` with tabs
and workspaces respectively. `a-restart-members.json` retains `writer` after a
running placement edit (no immediate server change) and a team restart.
`brought-members.json` shows alpha managed and beta still external; the exercised
script checks that beta's pane, terminal, harness, folder and state-change sequence
stay unchanged. `message-timeline.json` contains the delivered CHECK150 and the
shell's authenticated CLI reply to human. The forget agent lists are identical.
The final script retains the idle runner shell on forget to preserve the workspace.

The session lists before and after are identical. `lugarn-150` was stopped/deleted
and the script's scratch directory removed. Paths and IDs in these fixtures are
historical locators, not resources that remain running. The Herdr client fixture
test decodes the actual agent and pane shapes, including terminal IDs.
