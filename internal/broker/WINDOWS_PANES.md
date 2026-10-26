# Windows panes and cleanup

Implements `385eada`, read before editing, in three focused commits on
`c2/windows-jobs`: `4643dce` (panes), `e4931b3` (termination/diagnostics), and
`d34028a` (ended-launch cleanup). Each carries the requested co-author trailer.
Nova reports the previous Windows job tests and full harness/broker suites passed
on ufo. This round used no ufo access, real Herdr server, live harness, provider
credential file, copied provider home, or real `~/.lugarn` access. Nothing pushed.

## Changes and tests

1. **Windows panes:** `pane-launch <ULID>` reads a private `pane-launch.json`
   capsule and executes the absolute native harness with exact argv, workdir,
   per-launch PATH/environment and the pane's console streams. No harness flags
   pass through PowerShell. The wrapper consumes its own Ctrl+C notifications
   without setting an inherited ignore flag or coupling child lifetime to main's
   signal context; the harness still receives console Ctrl+C. Exit codes pass
   through unchanged. Both helpers now share capsule confinement checks, including
   exact file/launch, explicit state root, regular files and no linked capsule
   parents. The old Windows PowerShell pane-shell bootstrap is removed.

   Windows opens a tab, types only the plain broker path, `pane-launch` and the
   launch id, then polls Agents for up to 30 seconds. Only the expected kind in
   the created pane/workspace is renamed to the derived agent name. Timeout is
   launch_failed/cause agent_not_detected. Herdr already exposed Agents; the new
   RenameAgent call validates the new name and uses the named client's session.
   POSIX still uses its clean shell and agent start.

   Tests: TestWindowsPaneFlowListsThenRenames, TestWindowsPaneDetectionTimeout,
   TestPaneLaunchExactArgvAndExitCode, TestPaneLaunchCapsuleConfinement,
   TestPaneLaunchCommandValidation, TestRenameDetectedAgent. The re-executed test
   child checks all three harnesses' argv, including Codex's quoted -c JSON,
   empty arguments, spaces, backslashes and Unicode; stdin/stdout/stderr, PATH,
   workdir, environment overrides and nonzero exit codes are also checked.

2. **Termination and diagnostics:** TerminateProcess ACCESS_DENIED succeeds only
   if the same retained process handle signals within 500 ms. A still-live denied
   process and wait errors remain failures. Every integration teardown failure
   includes its step and target; stop-team preserves the full joined diagnostics
   in details.cause instead of losing them when unwrapping a protocol error.
   Tests: TestWindowsTerminateProcessAccessDeniedRace uses deterministic fake
   syscalls; TestIntegrationTeardownEndsOwnedLaunchArtifacts and
   TestStopTeamPreservesTeardownStepAndTarget check error context and continued
   cleanup.

3. **Failed conversion:** the previous launch's files are removed immediately
   after the transaction durably ends its lease, before fallible pane setup.
   Cleanup uses a bounded context independent of request cancellation. The
   regression test TestFailedPaneConversionRemovesPreviousLaunchFiles asserts
   token/config/directory deletion *inside* a deliberately failing OpenTab call,
   then checks the ended old launch and failed new member state.

## Cross-compiled Windows tests

These broker/Herdr tests use temporary state and their own test-binary helpers.
They require no installed harness, Herdr, fixture directory, or live opt-in.
Symlink-specific confinement subcases skip when Windows disallows creating links.
The fake typed broker path models the runner's already-plain bridge, independently
of test-binary placement or disabled 8.3 names. Exact-argv tests execute the actual
copied test binary directly, so spaces in its path remain covered by native exec.

```sh
GOOS=windows GOARCH=amd64 go test -c -o broker.test.exe ./internal/broker
GOOS=windows GOARCH=amd64 go test -c -o herdr.test.exe ./internal/herdr
```

PowerShell, from the directory holding just those binaries:

```powershell
.\broker.test.exe -test.v -test.timeout 120s -test.run '^(TestWindowsPane|TestPaneLaunch|TestWindowsTerminateProcessAccessDeniedRace|TestFailedPaneConversion|TestIntegrationTeardown|TestStopTeamPreservesTeardown)'
.\herdr.test.exe -test.v -test.timeout 60s -test.run '^TestRenameDetectedAgent$'
```

Both cross-compiles exited 0 without output. Ready binaries, not committed:
`/tmp/lugarn-windows-panes-uKyUWIv0/broker.test.exe` and
`/tmp/lugarn-windows-panes-uKyUWIv0/herdr.test.exe`. Both were identified as PE32+
console executables, x86-64, for Windows. **Native Windows and macOS tests were
not run on this Linux host.** New Windows pane detection, console Ctrl+C and the
real termination race still need the requested ufo verification; no live result
is claimed here.

## Final check output

All six commands below exited 0, with all seven live switches set to 0.
Formatting and all three vets produced no output. The exact stdout/stderr follows;
raw logs and silent cross-compile logs are in
[testdata/windows-panes-checks](testdata/windows-panes-checks).

```text
$ gofmt -l .
$ go vet ./...
$ GOOS=windows GOARCH=amd64 go vet ./...
$ GOOS=darwin GOARCH=arm64 go vet ./...
$ go test -race ./...
ok  	github.com/LexingtonStanley/lugarn-comms/cmd/lugarn-comms	(cached)
ok  	github.com/LexingtonStanley/lugarn-comms/internal/agentops	(cached)
ok  	github.com/LexingtonStanley/lugarn-comms/internal/appops	(cached)
ok  	github.com/LexingtonStanley/lugarn-comms/internal/broker	46.919s
ok  	github.com/LexingtonStanley/lugarn-comms/internal/cli	(cached)
ok  	github.com/LexingtonStanley/lugarn-comms/internal/gitwt	(cached)
ok  	github.com/LexingtonStanley/lugarn-comms/internal/harness	(cached)
ok  	github.com/LexingtonStanley/lugarn-comms/internal/herdr	(cached)
ok  	github.com/LexingtonStanley/lugarn-comms/internal/hostprobe	(cached)
ok  	github.com/LexingtonStanley/lugarn-comms/internal/mcpserver	(cached)
ok  	github.com/LexingtonStanley/lugarn-comms/internal/model	(cached)
ok  	github.com/LexingtonStanley/lugarn-comms/internal/policy	(cached)
?   	github.com/LexingtonStanley/lugarn-comms/internal/service	[no test files]
ok  	github.com/LexingtonStanley/lugarn-comms/internal/statedir	(cached)
ok  	github.com/LexingtonStanley/lugarn-comms/internal/store	(cached)
?   	github.com/LexingtonStanley/lugarn-comms/internal/version	[no test files]
ok  	github.com/LexingtonStanley/lugarn-comms/internal/wire	(cached)
?   	github.com/LexingtonStanley/lugarn-comms/schema	[no test files]
$ python3 scripts/validate-schemas.py
ok    app-ops.v1.schema.json: valid draft 2020-12
ok    common.v1.schema.json: valid draft 2020-12
ok    mcp-tools.v1.schema.json: valid draft 2020-12
ok    team-spec.v1.schema.json: valid draft 2020-12
ok    app-ops/apply-team--revision-conflict.response.json
ok    app-ops/apply-team.request.json
ok    app-ops/apply-team.response.json
ok    app-ops/continue-budget--rounds.request.json
ok    app-ops/continue-budget.request.json
ok    app-ops/continue-budget.response.json
ok    app-ops/hello--windows.response.json
ok    app-ops/hello.request.json
ok    app-ops/hello.response.json
ok    app-ops/hold--release.request.json
ok    app-ops/hold--release.response.json
ok    app-ops/hold.request.json
ok    app-ops/hold.response.json
ok    app-ops/members.request.json
ok    app-ops/members.response.json
ok    app-ops/open-terminal--lease-conflict.response.json
ok    app-ops/open-terminal.request.json
ok    app-ops/open-terminal.response.json
ok    app-ops/remove-worktrees.request.json
ok    app-ops/remove-worktrees.response.json
ok    app-ops/resolve-delivery.request.json
ok    app-ops/resolve-delivery.response.json
ok    app-ops/send--all.request.json
ok    app-ops/send.request.json
ok    app-ops/send.response.json
ok    app-ops/start-herdr.request.json
ok    app-ops/start-herdr.response.json
ok    app-ops/start-team--herdr-down.response.json
ok    app-ops/start-team.request.json
ok    app-ops/start-team.response.json
ok    app-ops/status.request.json
ok    app-ops/status.response.json
ok    app-ops/timeline--launch-failed.response.json
ok    app-ops/timeline.request.json
ok    app-ops/timeline.response.json
ok    app-ops/watch--bad-cursor.ndjson (1 lines)
ok    app-ops/watch.ndjson (8 lines)
ok    app-ops/watch.request.json
ok    app-ops/worktrees.request.json
ok    app-ops/worktrees.response.json
ok    mcp/inbox.input.json
ok    mcp/inbox.output.json
ok    mcp/reply.input.json
ok    mcp/reply.output.json
ok    mcp/send--denied.error.json
ok    mcp/send--held.output.json
ok    mcp/send--human-too-long.error.json
ok    mcp/send.input.json
ok    mcp/send.output.json
ok    mcp/spawn--cap-reached.error.json
ok    mcp/spawn--profile-too-high.error.json
ok    mcp/spawn.input.json
ok    mcp/spawn.output.json
ok    mcp/stop.input.json
ok    mcp/stop.output.json
ok    mcp/team.input.json
ok    mcp/team.output.json
ok    team-spec/manager-workers.json
ok    team-spec/orchestrator.json
ok    team-spec/pipeline.json
ok    team-spec/writer-reviewer.json
ok    rejects: grid edge with wake but not message
ok    rejects: member named human
ok    rejects: spawn enabled without a worker brief
ok    rejects: member host set in v1
ok    rejects: per_recipient other than 1
ok    rejects: send with both to and all
ok    rejects: timeline with both before and after
ok    rejects: continue-budget writer without kind rounds and reviewer
ok    rejects: unknown request field
ok    rejects: agent send to all
ok    rejects: agent text to human over 8000 code points
ok    rejects: hold with both ttl_seconds and release
ok    rejects: hold ttl over 120 s
ok    rejects: remove-worktrees with no paths
ok    docs/protocol.md enum table matches the schemas (46 enums)

4 schemas, 61 example files, 0 failure(s)
```
