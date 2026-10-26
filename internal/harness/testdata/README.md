Recorded on lexbox, 2026-10-26, behind LUGARN_HARNESS_LIVE=1. These are complete,
unmodified stdout streams from real runs, inspected for secrets before adding.
They contain session/message ids, local paths, tool/server names, usage and `ok`;
no credentials, tokens, tool results containing personal data, or prompt secrets.
Claude's init lists the existing user's servers; no tools were called.

Each ran in its own fresh temporary directory, prompt `Reply with the word ok.`
on stdin, with LUGARN_COMMS_HOME pointing to a child state directory:

    claude -p --session-id b09c3f46-e877-4e7d-bbb6-c7db563a68f4 --output-format stream-json --verbose --permission-mode plan --model haiku
    codex -s read-only -m gpt-6-luna -c 'model_reasoning_effort="low"' exec --json --skip-git-repo-check -
    OPENCODE_CONFIG=<temporary permission file> opencode run --format json --standalone -m opencode-go/iceman-contributor-free

The OpenCode file contained {"permission":{"edit":"deny","bash":"deny"}}.
Versions: Claude Code 2.1.310, codex-cli 0.164.0, OpenCode v2.1.4.
All three exited 0 with empty stderr and final text `ok`.
OpenCode emitted repeated step_start events, then text, with no step_finish or usage.
The fixture prompt had no MCP registration; live_test.go tests registration separately.
