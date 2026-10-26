# Windows turn jobs

Implements protocol commit `14ab2e6` and decision 69 on `c2/windows-jobs`.
Five focused commits cover job creation, normal turn completion, recovery,
runner exit, and Windows tests. Each has the requested Codex co-author trailer.
No push, ufo access, real harness launch, Herdr command, provider credential
access, or real `~/.lugarn` access was performed.

## Changes

- **Creation and inheritance:** every turn creates `Local\lugarn-comms-<launch id>-<32 random hex characters>` with no kill-on-close. The direct `CreateJobObjectW` call preserves ERROR_ALREADY_EXISTS, which x/sys's wrapper discards on a nonzero return. Collision closes the newly returned handle and fails; it never joins that job. A DuplicateHandle copy grants only JOB_OBJECT_QUERY (0x4), is inheritable, and is passed through AdditionalInheritedHandles. The parent's duplicate closes immediately after Start, including failed starts.
- **Identity:** `harness.Handle.JobName` is immutable after Start. The broker adapter forwards it; `<state>/launch/<launch id>/<turn id>.process.json` stores `job_name` next to `pid` and `birth`. Existing records without a name remain readable. The store/model schema is unchanged.
- **Turn end and cancel:** cancellation terminates the job. Normal root exit queries active membership while the creator still holds the job handle, terminates surviving members, and then closes it. An accounting failure still attempts termination and reports the cleanup error.
- **Recovery and teardown:** Windows `harness.TerminateJob` uses LazyDLL's OpenJobObjectW with QUERY|TERMINATE (0xC). Reconcile, orphan cancellation, stop-member, and stop-team use only an exact recorded name matching the turn's launch and random suffix. FILE_NOT_FOUND permits the existing PID/birth tree fallback; access and other errors do not. A dead/reused root can still have its recorded job cleaned without signalling the replacement PID. No recovery path constructs or guesses a job name.
- **Runner exit audit:** the broker adapter already supplies WithoutCancel to the harness's lifetime context; Runner.Run's ordinary exit does not call team shutdown, Cancel, or descendant cleanup. OS handle closure now leaves the job running. The audit also found and fixed cancellation racing an acknowledgement write: once PID/birth/job metadata is durable, ordinary runner cancellation detaches instead of entering error cleanup. A failure before identity persistence still cancels/reaps the untracked start. Direct harness callers retain their existing explicit context-cancellation behavior.

The os/exec start-to-assignment window remains: a child could spawn before
AssignProcessToJobObject and escape membership. Inheriting a query handle keeps
the name alive but does not assign the child. Eliminating the window requires
suspended creation/thread resumption, which os/exec does not expose. Assignment
failure kills/reaps the root; escaped descendants are not guaranteed captured.
The source comment now states that limit explicitly.

## Tests and Windows commands

The new Windows job tests are self-contained re-executions of the test binary:
no installed harness, shell, provider config, fixture directory, admin setup,
or opt-in live variable is needed. They use only their own processes/jobs and
private temporary directories.

- `TestWindowsJobSurvivesCreatorAndTerminatesFromAnotherProcess`: the child verifies its inherited handle is query-only (termination is denied) and kill-on-close is absent. The creator exits, the child and its leaf remain alive, and a separate process opens the recorded name and terminates both. The test process never opens the job before creator exit, so it cannot accidentally keep the name alive itself.
- `TestWindowsJobRejectsExistingName`: fails with ERROR_ALREADY_EXISTS and verifies the original handle still works.
- `TestWindowsJobNormalCompletionTerminatesRemainingMembers`: lets the root exit normally and verifies job completion kills the remaining leaf.
- `TestRunnerWindowsMissingJobFallsBackToPIDIdentity`: persists a missing name; the fallback refuses an incorrect birth, then terminates the same owned child with its correct recorded birth.

Portable fake coverage also checks job metadata, recorded-name preference,
missing/unrecorded-name fallback, denial without fallback, wrong-launch refusal,
reconcile cleanup after root exit, normal descendant cleanup, and the runner's
acknowledgement-write detach race.

Cross-compile from the repository:

```sh
GOOS=windows GOARCH=amd64 go test -c -o harness.test.exe ./internal/harness
GOOS=windows GOARCH=amd64 go test -c -o broker.test.exe ./internal/broker
```

Copy just those binaries to Windows and run in PowerShell from any directory:

```powershell
.\harness.test.exe -test.v -test.timeout 90s -test.run '^TestWindowsJob'
.\broker.test.exe -test.v -test.timeout 60s -test.run '^TestRunnerWindowsMissingJobFallsBackToPIDIdentity$'
```

Built here, both commands exited 0 with no output. The generated binaries are
`/tmp/lugarn-windows-jobs-3U1W0JAT/harness.test.exe` and
`/tmp/lugarn-windows-jobs-3U1W0JAT/broker.test.exe` (not committed). `file` reported:

```text
harness.test.exe: PE32+ executable (console) x86-64, for MS Windows, 16 sections
broker.test.exe:  PE32+ executable (console) x86-64, for MS Windows, 16 sections
```

**Native Windows tests were compiled, not executed on this Linux host.** Windows
CI/ufo must establish runtime success; ufo verification is left to Nova as asked.
macOS was cross-vetted, not run locally. Linux portable tests ran with the race
detector. The known Windows 8.3 `~` fix on main was left intact.

## Check output

All live switches were 0. Formatting, Linux vet, Windows/amd64 vet,
Darwin/arm64 vet, the Linux race suite, schema validation, and both Windows test
compilations exited 0. Exact check stdout/stderr follows; empty output is empty.

```text
$ gofmt -l .
$ go vet ./...
$ GOOS=windows GOARCH=amd64 go vet ./...
$ GOOS=darwin GOARCH=arm64 go vet ./...
$ go test -race ./...
ok  	github.com/LexingtonStanley/lugarn-comms/cmd/lugarn-comms	7.544s
ok  	github.com/LexingtonStanley/lugarn-comms/internal/agentops	(cached)
ok  	github.com/LexingtonStanley/lugarn-comms/internal/appops	(cached)
ok  	github.com/LexingtonStanley/lugarn-comms/internal/broker	40.598s
ok  	github.com/LexingtonStanley/lugarn-comms/internal/cli	(cached)
ok  	github.com/LexingtonStanley/lugarn-comms/internal/gitwt	1.362s
ok  	github.com/LexingtonStanley/lugarn-comms/internal/harness	5.797s
ok  	github.com/LexingtonStanley/lugarn-comms/internal/herdr	10.707s
ok  	github.com/LexingtonStanley/lugarn-comms/internal/hostprobe	7.035s
ok  	github.com/LexingtonStanley/lugarn-comms/internal/mcpserver	(cached)
ok  	github.com/LexingtonStanley/lugarn-comms/internal/model	1.072s
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

Raw logs are in [testdata/windows-jobs-checks](testdata/windows-jobs-checks).
