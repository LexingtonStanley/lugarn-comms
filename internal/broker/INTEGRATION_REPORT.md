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

## C2d review addendum — 2026-10-26

This addendum supersedes the implementation descriptions above where the review
changed them. Read `git show 135d1cc` first, then the entire revision brief and the
C2d safety/common brief. Fifteen focused commits implement the requested order:
8, 11, 12, 13, 1, 2, 3, then 4, 5, 6, 7, 9, 10, 14, 15. Each carries the requested
Codex co-author trailer. No push or PR was made.

The earlier default-store fallback is now **confirmed by Nova's review**, rather
than hypothetical. Nova inspected and moved that empty store aside. No inspection
or modification of the person's `~/.lugarn` was performed in this revision round.
No integration code or tool action opened a provider credential file or copied a
provider config home. The live harness inherited the person's own config. All new real probes were opt-in and
used fresh, explicitly named private Herdr sessions, stopped and deleted by their
own session names. There were no name-based process kills.

### Per-item changes and tests

The live checks listed below were each attempted once. Other items use portable
fakes or temporary scratch repositories; no additional paid verification was run.

| Item / commit | Change | Regression coverage and live evidence |
| --- | --- | --- |
| **8** / `0e75171` | Pane preflight refuses existing panes, live turns, unavailable harnesses and unresolved binaries before worktree preparation or lease rotation. The failure defer runs only after a launch attempt begins. Session-before-pane uses an existing headless lease. | `TestOpenPreflightLeavesHeadlessMemberUnchanged` compares the complete before/after member for harness, resolution and live-turn refusals. `TestIntegrationPaneLaunchAndSessionBeforePane` covers an existing headless Codex lease and already-pane refusal. No paid run needed. |
| **11** / `04cd980` | CLI response timeout is `kindTimeout`, separate from server-not-running/unavailable. State timeout is unknown; wait timeout remains working. Observation/settling failures log and back off without ending the runner. A transient follow-up observation during Exited also retries. | `TestNoAnswerIsTimeout` uses a delayed fake CLI and asserts not gone/unavailable. `TestPaneObservationErrorRetriesWithoutStoppingRunner` checks backoff, a subsequent successful observation, unchanged lease, and no fatal worker error. No live slowdown induced. |
| **12** / `9654477` | A headless-home pane exit closes its owned tab before conversion. A recorded workspace with no owned runner pane gets a fresh runner tab, without any label lookup. | Extended `TestIntegrationPaneExitRestoresHeadlessWithoutWaitingOnItself` asserts closure. `TestIntegrationLauncherRecordsWorkspaceIDAndNativeBridge` removes placement and restarts in the same workspace, without creating another workspace. No model needed. |
| **13** / `f2ccf21` | Teardown waits up to 30 seconds, attempts every cleanup stage, and returns `errors.Join` of the failures. Pointer capture, process cancellation, pane closure, ending launches, artifact cleanup, bridge cleanup and placement removal continue after individual errors. | `TestIntegrationTeardownEndsOwnedLaunchArtifacts` injects a pane-close failure and still checks removal of the bridge, token, settings and pointer. The original broker already completes store cleanup when external teardown reports an error. No live server was deliberately killed mid-team. |
| **1** / `4252c6b` | `hostprobe.LoginPath` captures the login interactive shell with null stdin, fixed markers, a private process group and a 10-second deadline. Unsupported csh/tcsh and failures retain an issue. Merge removes empty, relative and duplicate entries. Cache lasts one hour. Executable app/agent/run entry points set process PATH **before constructing the real probe**; hostprobe resolution itself retains its existing API. Start-team/start-herdr refresh, and PATH is explicit in the runner pane, clean shell, detached server env, and systemd `--setenv`. `hello.host.path` is typed, schema-defined and covered by both POSIX and Windows examples. | `TestLoginPathCapture`, `TestLoginPathMergeAndCache`, `TestSystemdEnvironmentOption`, extended `TestStartServerDetaches`, `TestHelloPathCaptureWire`, schema examples. **Live passed:** 18 captured entries; local bin present; private systemd server's pane had it; bare user-manager command did not. [Raw log](testdata/revision-01-live.log). |
| **2** / `ceb4624` | Clean POSIX shell uses `env -u ENV -u BASH_ENV HISTFILE= /bin/sh …`, still drops exported bash functions, and waits up to 20 seconds for rc completion. | `TestCleanShellHistoryIsEmpty` asserts empty assignment instead of unsetting. Existing shell-pin tests remain; hostile-rc live matrix was not repeated. The Codex live probe below exercised the changed bootstrap once. |
| **3** / `9cc3879` | Codex's own regular matching rollout under `$CODEX_HOME/sessions` or `~/.codex/sessions` decides cwd. Only its first `session_meta` line is parsed; no other config-home file is read. Failures use `workdir_missing`, `workdir_changed`, or documented `rollout_missing`. Resume carries supported `--cd <validated cwd>`; before the first delivery per pane generation, recent unwrapped text is checked for the fixed `Working directory · resume` title. A match closes the pane and records launch_failed; no picker answer is sent. | `TestCodexWorkdirUsesRollout` covers missing rollout, matching cwd, mismatch and missing folder; `TestCodexResumePickerClosesWithoutDelivery` checks close/no submit. **Live passed:** one gpt-6-luna brief-only turn; removed A failed workdir_missing; a test-only Herdr proxy opened B and stripped --cd to demonstrate resume_picker detection and close. [Raw log](testdata/revision-03-live.log). |
| **4** / `5d55063` | Plain broker executable paths are used directly. Only non-plain paths need a bridge: under the plain state directory when possible, otherwise the OS temp directory. Bootstrap comments make the runner's plain executable invariant explicit; its existing check rejects an unsafe path. | `TestIntegrationBridgeUsesPlainExecutable` and extended `TestIntegrationRunnerBridgeHandlesSourcePathWithSpaces`. Teardown accepts only validated bridge parents and prefix. **Windows:** if 8.3 names are disabled, a bridge under a plain state path still works; a fallback temp path containing spaces cannot be made plain and fails runner_unavailable/unresolvable. Windows runtime not verified here. |
| **5** / `1098fe8` | Opt-in flow (b) now selects `opencode-go/kimi-k3.1-pro`. Fixed its readiness predicate so an initial stopped member without a stop reason is not confused with a completed launch failure. | **One live attempt failed in the test's readiness predicate**, before any turn was reported. It did not establish a provider or integration failure. Corrected predicate was not rerun live under the once-only rule. `TestOpenCodeSessionBeforePaneAndDelivery` now verifies brief-only/no consumption/no auto-reply, the pane lease, and a delivered message using a fake headless driver and Herdr. **Real Kimi pane delivery remains unverified.** [Raw failed-attempt log](testdata/revision-05-live.log). |
| **6** / `ae9c3a5` | Added state-table rows for pin links, capsule, ready marker, immutable launch/process descriptions, placement, session metadata, PATH cache and optional bridge, with their lifetimes. Documented exact-name/lstat/no-link-traversal cleanup. | Existing `TestRunnerCleanup…` cases cover known ended launches, exact suffixes, unknown-age grace, listing-before-store ordering, and links whose outside targets survive. Teardown test also removes ended per-launch artifacts. No model run needed. |
| **7** / `64c08b0` | Removed broker's duplicate hook writer. `harness.ClaudeHookSettings(launchID, platform, home)` now owns explicit-home quoting, umask 077 and a trailing newline. `Launch.Pane` controls pane-only settings generation; `PointerHome` never changes HOME/provider configuration. | `TestHookAndPointer` invokes two hooks with newline-free input, under an explicit temp pointer home different from HOME, then checks the pointer reader. `TestHeadlessClaudeHasNoPointerSettings` asserts no headless settings file. Full harness suite and Windows vet cover both generated command forms; no paid Claude rerun. |
| **9** / `23db78b` | Stop-member waits, re-reads the member, then captures/closes the pane actually recorded at that time. | `TestStopMemberClosesPaneRecordedWhileWaiting` starts from a pane lease without a pane id and checks closure of the post-wait recorded pane. No live launch race induced. |
| **10** / `d01a572` | Return-headless waits for worker/turn completion **and no delivering rows**, then waits explicitly for Herdr idle/done, captures the pointer, closes, and converts. | `TestReturnHeadlessWaitsForDeliveringBeforeHerdr` holds a delivering row and asserts both the Herdr wait and close come after its completion. No paid run needed. |
| **14** / `36d9cea` | Tree snapshots contain pid, parent and birth together. These are rechecked before recursion and signalling. Linux opens pidfd first, re-verifies, then uses pidfd_send_signal; there is no unsafe fallback if pidfds are unavailable. Darwin rechecks birth and ppid immediately before signalling; it has no Linux pidfd equivalent. | `TestTreeKillRechecksSnapshotIdentity` simulates birth reuse, changed parent, and a change just before signal. Native cancellation and Linux adopted-child tests also pass. Darwin cross-vet passes; no macOS runtime check. |
| **15a–e** / `69decc0` | Reserve worktree row before git, so crashes/cancel/generation loss cannot hide a created tree. Release pane-reason holds even during a person hold. Token-derived stores require an existing regular token and database and skip Ensure/chmod. Exited handles a missing pane id. Every pane close verifies membership in the team's recorded workspace; foreign reused ids are refused. | Worktree test passes a stale generation and asserts no git worktree was created. `TestReturnHeadlessReleasesPaneHoldsDuringPersonHold`, `TestTokenPathCannotCreateOrChmodAStore`, `TestPaneExitWithoutRecordedPaneID`, and `TestClosePaneRefusesReusedIDInForeignWorkspace` cover b–e. Stripped-environment SDK MCP/CLI tests still pass with existing owned token/store paths. No extra live run needed. |

### Live commands and limits

Only these three revision live commands ran, once each:

```sh
LUGARN_INTEGRATION_PATH_LIVE=1 LUGARN_HERDR_LIVE=0 LUGARN_HARNESS_LIVE=0 go test -v ./internal/broker -run '^TestIntegrationLiveLoginPath$' -count=1
LUGARN_INTEGRATION_CODEX_CWD_LIVE=1 LUGARN_HERDR_LIVE=0 LUGARN_HARNESS_LIVE=0 go test -v ./internal/broker -run '^TestIntegrationLiveCodexWorkdir$' -count=1
LUGARN_INTEGRATION_LIVE=1 LUGARN_HERDR_LIVE=0 LUGARN_HARNESS_LIVE=0 go test -v ./internal/broker -run '^TestIntegrationLiveFlows/b-opencode-pane$' -count=1
```

PATH evidence records shape only, never the captured PATH/environment:

```text
capture shape: entries=18 local_bin=true elapsed_with_server_ms=384
server pane local_bin=true
bare user-manager command local_bin=false
```

The 384 ms measurement includes the capture **and** start-herdr; it is an upper
bound for capture time, not an isolated capture benchmark. Start-herdr received
`KillUserProcesses(true)` from the injected logind report solely to select the
real systemd method on this KillUserProcesses=no host. This changed no host
logind setting. The bare systemd-run comparison printed only a presence boolean.
Session `lugarn-test-ecafd397fb69c715` was stopped and deleted.

Codex evidence:

```text
gpt-6-luna brief-only turn session=01a17a0a-e515-71f3-9e01-b4e84c417749
removed rollout cwd A: launch_failed cause=workdir_missing
test-only pane cwd B: launch_failed cause=resume_picker; pane closed; no delivery submitted
```

The test-only proxy bypass exists only in `_test.go`; production has no bypass
flag. Session `lugarn-test-b5dcb93c897e0499` was stopped and deleted. One tiny
brief-only model turn ran; no dollar cost was reported, so none is inferred.

For the supported picker-prevention option I inspected `codex resume --help`
and `codex --help`. The [official OpenAI CLI reference](https://learn.chatgpt.com/docs/developer-commands)
also documents explicit `--cd` taking precedence over `tui.resume_cwd`; the
[official sample config](https://learn.chatgpt.com/docs/config-file/config-sample)
documents current/session preferences. I chose explicit --cd while still checking
the visible fixed picker title. No user config file was read or edited.

OpenCode evidence is a **failed** test run, retained verbatim. Its predicate
observed the member's initial stopped/no-reason state, and no turn was listed
before failure. The team and private server were cleaned up. This does not prove
that no provider work started during the cleanup race, so no spend is inferred.
Session `lugarn-test-729f2f99b95ef534` was stopped and deleted. The corrected test
was deliberately not run again; real Kimi session-before-pane/delivery still needs
verification. macOS, Windows, PowerShell and fish were not checked live this round.

### PR-description follow-ups retained unchanged

These remain follow-ups, not work added to this round:

- Windows `KILL_ON_JOB_CLOSE` versus detach (Lex will take it on ufo).
- MCP tool annotations.
- The store accessor for worktree reassignment.
- The Codex review-mode inconsistency (the original spec references were lines 154/1649 versus 1673).

### Revision final checks

`jsonschema==4.26.0` was installed with `python3 -m pip install --user`;
this distro rejected that first command as externally managed. Repeating it with
`--user --break-system-packages` installed only user packages and succeeded.
No system package files were changed.

All seven live switches were `0` for the final checks: `LUGARN_HERDR_LIVE`,
`LUGARN_HARNESS_LIVE`, `LUGARN_INTEGRATION_SHELL_LIVE`, `LUGARN_INTEGRATION_LIVE`,
`LUGARN_INTEGRATION_PROVIDER_DIAGNOSTIC`, `LUGARN_INTEGRATION_PATH_LIVE`, and
`LUGARN_INTEGRATION_CODEX_CWD_LIVE`. PATH included `$HOME/.local/go/bin` for Go.

The first complete unit pass caught a timestamp in the new hello example without
the protocol's required milliseconds. It was corrected to `.000Z`; round-trip and
schema validation now both pass. No outstanding portable check failures remain.
Exact final stdout/stderr follows; empty output is left empty rather than invented.

All six commands exited **0**. Full stdout/stderr:

```text
$ gofmt -l .
$ go vet ./...
$ GOOS=windows GOARCH=amd64 go vet ./...
$ GOOS=darwin GOARCH=arm64 go vet ./...
$ go test -race ./...
ok  	github.com/LexingtonStanley/lugarn-comms/cmd/lugarn-comms	(cached)
ok  	github.com/LexingtonStanley/lugarn-comms/internal/agentops	(cached)
ok  	github.com/LexingtonStanley/lugarn-comms/internal/appops	(cached)
ok  	github.com/LexingtonStanley/lugarn-comms/internal/broker	37.701s
ok  	github.com/LexingtonStanley/lugarn-comms/internal/cli	(cached)
ok  	github.com/LexingtonStanley/lugarn-comms/internal/gitwt	(cached)
ok  	github.com/LexingtonStanley/lugarn-comms/internal/harness	(cached)
ok  	github.com/LexingtonStanley/lugarn-comms/internal/herdr	(cached)
ok  	github.com/LexingtonStanley/lugarn-comms/internal/hostprobe	(cached)
ok  	github.com/LexingtonStanley/lugarn-comms/internal/mcpserver	(cached)
ok  	github.com/LexingtonStanley/lugarn-comms/internal/model	1.059s
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

4 schemas, 60 example files, 0 failure(s)
```

Raw check logs: [format](testdata/revision-check-gofmt.log), [Linux vet](testdata/revision-check-vet-linux.log), [Windows vet](testdata/revision-check-vet-windows.log), [Darwin vet](testdata/revision-check-vet-darwin.log), [race](testdata/revision-check-race.log), [schema validation](testdata/revision-check-schemas.log). `git diff --check 135d1cc..HEAD` also produced no output and exited 0.
