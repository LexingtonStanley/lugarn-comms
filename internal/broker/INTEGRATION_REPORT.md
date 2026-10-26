# C2d integration report

2026-10-26. Branch `c2/integration`, based on main `a3a7623`. Work is confined to
`internal/broker`, `cmd/lugarn-comms`, and this package's verification artifacts.
The eight build steps were committed in order, followed by focused hardening and
verification commits. No push or PR was made. `.brief/` was not changed.

## Implementation and exported API

- `IntegrationOptions`: trusted executable paths, pointer-home location, model selectors, environment additions, and injectable Herdr/server factories.
- `HerdrAPI`: consumer interface implemented by the merged Herdr client and test fakes.
- `Integration` / `NewIntegration`: construct real harness, launch-file and pane adapters without starting host processes.
- `Integration.Attach`: enable the real app launcher, teardown and start-herdr/capability behavior; bare `broker.New` remains injectable for unit tests.
- `Integration.Runner`: bind headless, files, panes, pane lifecycle, requests, worktree preparation, and process cleanup to the runner.
- `Integration.Launch`: record/reuse a workspace by ID, launch the runner in its owned root pane, and wait for its first heartbeat.
- `Integration.Close`: cooperative shutdown followed by birth-checked process/ancestry cleanup, owned pane closure, and ended-launch cleanup.
- `EnableSubreaper`: Linux child-subreaper setup; a no-op on other platforms.
- `RunPaneShell`: native internal bootstrap into an rc-free shell with an explicitly pinned executable PATH.
- `run --team`: real runtime wiring, `HERDR_PANE_ID`, and SIGINT/SIGTERM context handling in the binary.

Pane leases include derived agent names, tabs, trust-state holds, assigned Claude
sessions, and brief-only first turns for reporting harnesses. The four request
kinds execute with their waits, cancellation, generation fences, and events.
Return-headless waits only for idle/done and confirms the old agent is gone.
Claude pointer handling ignores incomplete/subagent records, reseals, and rebriefs.
Codex pane launches require a stored canonical thread workdir match.

## Driver and lifecycle details

The harness driver uses a process-lifetime context. Its adapter detaches that from
the runner's startup-only context; explicit handle cancellation still kills its
job/group and ancestry. Linux protects harness PIDs from before driver Start
returns until its os/exec Wait has reaped them. The reaper waits on individual
adopted zombie PIDs only. Ordinary Herdr/git subprocesses inherit the runner's
process group and are excluded. On team stop, surviving adopted descendants are
terminated by birth-checked numeric ancestry. Tests cover both preserved exec
exit status and an adopted child surviving its original parent.

For executable paths containing spaces, the launcher makes a private native
hardlink/copy of the broker at a validated plain temporary path. Herdr receives
only that native bridge plus fixed plain arguments. No original executable path
is interpolated into a shell command. The bridge remains until teardown. Windows
uses a short native path where necessary and fails clearly if it cannot provide a
plain executable path.

Before agent start, a native `pane-shell` bootstrap reads its own generated launch
capsule. POSIX then executes this fixed sequence, with PATH assembled in Go:

```text
exec <plain-native-broker-bridge> pane-shell
/usr/bin/env -u ENV -u BASH_ENV -u HISTFILE /bin/sh -c 'printf ready > "$LUGARN_COMMS_PANE_READY"; exec /bin/sh'
```

Exported bash functions are removed too. PATH begins with a per-launch bin
containing only the selected harness's name linked to the resolved executable,
then a complete system PATH. It does not rely on the systemd server's PATH.
Windows starts the native PowerShell with `-NoLogo -NoProfile -NoExit` and an
encoded fixed ready-marker command. Neither bootstrap accepts agent text.

The generated Claude hook targets the explicitly supplied pointer home. The live
runs used a temporary pointer root while preserving the person's real HOME and
provider configuration. No provider credential file was opened/read/copied by the
integration code or tooling, and no copied provider config home was used. The
hostile-rc probes used disposable homes only for native fake harnesses.

## Live verification

All real Herdr commands named fresh `lugarn-test-<random>` sessions. The default
server was never intentionally probed, attached to, stopped, or reconfigured.
Only these tests' owned process PIDs and sessions were cleaned up.

Models were Haiku, `gpt-6-luna`, and `opencode-go/iceman-contributor-free`. Prompts
asked for `ok`, with no file changes. The two recorded completed Claude turns
reported approximately $0.0746 combined. Codex dollar cost was not reported. An
early spawn attempt was stopped after a test-harness parsing error; its cost is
not inferred. No paid exchange was repeated after its success.

### Shell pinning and quoting

Command:

```sh
LUGARN_INTEGRATION_SHELL_LIVE=1 LUGARN_HERDR_LIVE=0 LUGARN_HARNESS_LIVE=0 go test -v ./internal/broker -run '^TestIntegrationLiveShellPinning$' -count=1
```

Native fake Claude/Codex executables passed under both zsh and bash with a hostile
alias, function and PATH prepend. Resolved executable and every generated argv
matched exactly, including paths with spaces. Herdr supplied `HERDR_PANE_ID`
(`w1:p2`, `w1:p3`). Both servers were stopped. Fish was not installed; PowerShell
was not live-tested. No path restriction was needed for the tested POSIX shells.
See [condensed shell transcript](testdata/integration-shell-evidence.log) and the
strict assertions in [the probe](integration_shell_live_linux_test.go).

### Requested flow outcomes

Initial command:

```sh
LUGARN_INTEGRATION_LIVE=1 LUGARN_HERDR_LIVE=0 LUGARN_HARNESS_LIVE=0 go test -v ./internal/broker -run '^TestIntegrationLiveFlows$' -count=1
```

- **(a) Passed.** Headless Codex reviewer succeeded; hop 2 was held. Continue released one delivery with allowance 2; Claude writer succeeded and the next reply held. Both reported sessions and turn usage. Stop-team cleaned two launch IDs and the root pane.
- **(b) Not verified live.** OpenCode reported `ses_e8641b4f5ffeTbjbg1p6pTDf6W`, but its brief-only turn produced no final response within the 60-second turn limit. It became timed_out and the pane launch stopped. It consumed no messages and posted no auto-reply. No pane delivery was forced after that failure.
- **(c) Partial.** Open-terminal succeeded and Claude reached its trust dialog (`blocked`). Return-headless was not exercised live through that dialog: no trust answer/keys were injected. Request conversion, ready-state waits, pointer capture and unused-session handling are covered with fakes.
- **(d) Passed on the targeted rerun.** The first command successfully spawned a worktree worker, but the test mistakenly combined stderr with the JSON stdout. After fixing that test bug, the targeted command below created `lugarn/live-d/worker`, completed its Claude turn, and stopped it from the spawner with reason spawner. Stop-team cleaned both launch IDs. Explicit worktree removal refused dirty work, as required; it was not forced.
- **(e) Passed.** A zero-model idle-team run explicitly verified the recorded live runner process identity exited, both member launch IDs had no remaining token/config/delivery directory/pointer, and the root pane disappeared. Recorded harness PIDs from the earlier flows were also checked and were gone.

Targeted reruns:

```sh
LUGARN_INTEGRATION_LIVE=1 LUGARN_HERDR_LIVE=0 LUGARN_HARNESS_LIVE=0 go test -v ./internal/broker -run '^TestIntegrationLiveFlows/d-spawn-worktree-stop$' -count=1
LUGARN_INTEGRATION_LIVE=1 LUGARN_HERDR_LIVE=0 LUGARN_HARNESS_LIVE=0 go test -v ./internal/broker -run '^TestIntegrationLiveFlows/e-empty-idle-teardown$' -count=1
```

Evidence: [initial run, including failures](testdata/integration-live-first.log),
[spawn rerun](testdata/integration-live-spawn.log),
[teardown run](testdata/integration-live-teardown.log),
[final-source zero-model teardown](testdata/integration-live-final-teardown.log), and
[recorded process check](testdata/integration-live-process-check.log).
The owned sessions were subsequently confirmed stopped and deleted; see
[session cleanup](testdata/integration-session-cleanup.log).

To distinguish OpenCode's failure from this integration, one standalone baseline
used the person's own config, without our MCP/config overlay:

```text
opencode run --standalone --format json --model opencode-go/iceman-contributor-free
stdin: Reply with only the word ok. Do not use tools.
elapsed_seconds: 25.04
timed_out: True
exit_code: -9
stderr_bytes: 0
```

It also produced step_start events but no final reply. Only that baseline's own
process tree was cancelled/reaped. This supports an OpenCode/free-backend issue
outside the integration; it does not establish the precise upstream cause.
[Filtered baseline evidence](testdata/opencode-provider-probe.log).

That standalone baseline was a manual diagnostic and I missed the required opt-in
environment guard for that invocation. The committed reproducer now has a separate
`LUGARN_INTEGRATION_PROVIDER_DIAGNOSTIC=1` guard
(`TestIntegrationLiveOpenCodeBaseline`). It was not run again, to avoid another
model invocation. The flow and shell test invocations used their opt-in switches.


## Findings, choices, and remaining verification limits

- **MCP state isolation:** review after the first live run found that a Codex MCP process with stripped environment could fall back to the default state directory. Agent entry points now derive the store from the absolute generated launch-token path, before environment fallback. Built-binary tests remove the state environment and verify the intended store. The first live run preceded that fix; I cannot assert whether that earlier fallback occurred, and did not inspect or alter the forbidden real `~/.lugarn` to find out.
- Herdr preflight runs outside the write transaction, using the configured client; a concurrent writer remains available during a blocked probe (regression tested).
- A missing owned root-pane placement for an existing workspace fails clearly rather than adopting a pane or workspace by label.
- An assigned Claude ID and an initialized-but-unbriefed session are distinct. Private session metadata records initialization so the first pane uses `--session-id`, while a later headless turn resumes and still receives its pending brief. The regression needs no live paid warm-up.
- `internal/model` still lacks persistent process-birth identity, immutable effective launch settings, session initialization, and Codex session workdir. These use private state-side metadata; model was not edited.
- Store gap: `worktrees.member_id` has no reassignment accessor for reuse by a respawned identity. This code retains the recorded creator and resolves active users through member worktree links for listing/removal. Any active link prevents removal, including when a newer stopped link exists. A store accessor remains desirable to normalize the recorded current owner; store was not edited.
- Spec inconsistency: `docs/protocol.md:154` and `:1649` say a Codex reviewer runs in review mode, while `:1673` leaves the headless review flags open. The merged `harness.Launch` has no role/review selector. This integration uses that driver's ordinary Codex exec plus the member's role/brief; it does not invent unverified review flags.
- Front-door follow-up: `internal/mcpserver/server.go:29` currently supplies no tool annotations, including read-only hints for team/inbox. C2c's report describes a plan-mode issue with a fake server missing readOnlyHint. This integration's binary MCP tests verify authentication/dispatch, but no further paid Claude tool-call run was spent resolving that mismatch. The merged front-door package was not edited here.
- Source-level Windows follow-up: `internal/harness/process_windows.go:18` sets `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE`. When the runner executable exits, that appears inconsistent with preserving live turns on non-team-stop cancellation (the runner cancellation contract in `runner.go` and the C2a brief's Clean exits). The merged harness package was not edited; Windows runtime detach behavior needs verification/correction there.
- macOS, Windows, PowerShell, fish, OpenCode 1.x, real logind KillUserProcesses=yes/SSH logout, and an answered Claude trust dialog were not verified live in this work. The merged systemd API is used and its option/result wiring is unit-tested.

## Final checks

All live switches are disabled for the portable suite. Exact output follows below
and is also retained in the check logs.

Every command exited 0. Environment: `LUGARN_HERDR_LIVE=0`,
`LUGARN_HARNESS_LIVE=0`, `LUGARN_INTEGRATION_SHELL_LIVE=0`,
`LUGARN_INTEGRATION_LIVE=0`, `LUGARN_INTEGRATION_PROVIDER_DIAGNOSTIC=0`.

```text
$ gofmt -l .
$ go vet ./...
$ GOOS=windows GOARCH=amd64 go vet ./...
$ GOOS=darwin GOARCH=arm64 go vet ./...
$ go test -race ./...
ok  	github.com/LexingtonStanley/lugarn-comms/cmd/lugarn-comms	4.605s
ok  	github.com/LexingtonStanley/lugarn-comms/internal/agentops	(cached)
ok  	github.com/LexingtonStanley/lugarn-comms/internal/appops	(cached)
ok  	github.com/LexingtonStanley/lugarn-comms/internal/broker	34.452s
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
```
