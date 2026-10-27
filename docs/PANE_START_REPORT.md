The brief is implemented on `c3/pane-start`, based on `bd7ce69`. Pane startup uses the brief's **fallback (c)**: Herdr 0.9.5 cannot guarantee that a typed command reaches a shell prompt. The broker now explains unconfirmed startup with `shell_not_ready`. It does not automatically bypass or answer startup questions. The four other fixes are implemented.

All live control used our own `lugarn-test-<random>` sessions, scratch homes and explicit `LUGARN_COMMS_HOME`. Every server we started was stopped and its session deleted. No default server, `~/.lugarn`, credential file, global harness configuration, remote machine or real harness turn was used. Harness version probes ran; bootstrap probes used `/usr/bin/true`. Windows runtime verification remains for Nova.

**What Herdr 0.9.5 offers.** The complete commands and exact installed-binary output are preserved in [herdr-help.raw.json](../internal/broker/testdata/pane-start/herdr-help.raw.json) as a JSON transcript string. The [readable log](../internal/broker/testdata/pane-start/herdr-help.log) removes trailing whitespace from blank lines. Selected output:

```text
$ herdr --version
herdr 0.9.5

$ herdr workspace create --help
Usage: herdr workspace create [OPTIONS]

$ herdr tab create --help
Usage: herdr tab create [OPTIONS]

$ herdr pane split --help
Usage: herdr pane split [OPTIONS] [PANE_ID]

$ herdr pane run --help
Usage: herdr pane run <PANE_ID> <COMMAND>...
```

The creation options are cwd, environment, label/topology and focus; none accepts a command or argv. `herdr api schema --json` confirms that `WorkspaceCreateParams`, `TabCreateParams` and `PaneSplitParams` have no command field. Selected complete definitions are preserved in the help log. `pane run` types text and Enter; successful admission does not establish execution. This agrees with the [0.9.5 socket API documentation](https://raw.githubusercontent.com/herdrdev/herdr/v0.9.5/docs/next/website/src/content/docs/socket-api.mdx).

`herdr pane get`, `pane process-info`, `agent get`, `agent explain` and `agent wait` expose no authoritative shell-prompt readiness signal. `interactive_ready` describes a detected agent, not a plain shell. Exact probe output is in [herdr-state.log](../internal/broker/testdata/pane-start/herdr-state.log). For example, while zsh was blocked in `.zshrc`'s `read`:

```text
$ herdr --session lugarn-test-b097811de21c agent get w1:p1
{"error":{"code":"agent_not_found","message":"agent target w1:p1 not found"},"id":"cli:agent:get"}
```

`pane process-info` reported zsh itself as the only foreground process, with `shell_pid` equal to `foreground_process_group_id`. The before transcript shows the same process identity after `read` returns to the prompt. The [0.9.5 available-shell check](https://raw.githubusercontent.com/herdrdev/herdr/v0.9.5/src/platform/mod.rs#L484) underlying `agent_pane_busy` checks that process group and process identity; it does not distinguish a shell builtin reading a startup answer. Therefore neither option (a) nor a reliable option (b) is available. Guessing from terminal text or waiting for a foreground shell would not establish readiness.

Herdr would need an exact-argv spawn surface on workspace/tab/pane creation, preserving cwd and environment, or an authoritative shell-ready signal coupled with guarded command submission. Direct argv creation would be preferable because a foreground-process check alone cannot recognize shell builtin questions.

**Reproduction before and after.** Both runner runs used a private server, `SHELL=/usr/bin/zsh`, a short scratch `HOME`, and private Herdr configuration selecting zsh. The scratch `.zshrc` was:

```zsh
print -r -- "STARTUP_QUESTION: answer ready"
read -r answer
print -r -- "STARTUP_ANSWER=$answer"
PS1="SHELL_READY> "
```

The original broker was built with `go build -o /tmp/lugarn-pane-before ./cmd/lugarn-comms` before editing; the changed broker used `/tmp/lugarn-pane-after`. The retained [runner reproduction script](../internal/broker/testdata/pane-start/reproduce-runner.py) accepts `<binary> before|after`. Commands used:

```text
python3 /tmp/lugarn-pane-repro.py /tmp/lugarn-pane-before before
python3 /tmp/lugarn-pane-repro.py /tmp/lugarn-pane-after after
```

Before: [full transcript](../internal/broker/testdata/pane-start/reproduction-before.log). The typed command was consumed as `STARTUP_ANSWER=/tmp/lugarn-pane-before run --team pane-repro`. `start-team` failed after **15.123 s**, exit 32, with:

```json
{"code":"runner_unavailable","message":"runner could not start","details":{"failure":{"code":"runner_unavailable","message":"runner did not heartbeat within 15 seconds","details":{}}}}
```

After: [full transcript](../internal/broker/testdata/pane-start/reproduction-after.log). The same question still consumes the command, as expected for fallback (c). After **15.079 s**, exit 32, the error is now:

```json
{"code":"runner_unavailable","message":"pane startup was not confirmed; the shell may be asking a startup question. Open a Herdr pane, answer or resolve the shell's startup questions, then retry","details":{"cause":"shell_not_ready"}}
```

The team was stopped and the failed pane cleaned up. After removing the scratch startup question and retrying, `start-team` succeeded with a live runner in the state directory. Selected `/proc/<runner pid>/environ` values confirmed `LUGARN_COMMS_HOME`, `LUGARN_COMMS_POINTER_HOME` and explicit `PATH`; `pane process-info` confirmed cwd. `stop-team` succeeded and the runner pane subsequently returned `pane_not_found`.

[Bootstrap probes](../internal/broker/testdata/pane-start/bootstrap-live.log) reproduce swallowing of both `exec <broker> pane-shell` and `<broker> pane-launch <launch id>` in fresh zsh panes. Neither marker existed after the swallowed command. A manual retry at the prompt produced `ready` in each marker. The native wrapper probe ran `/usr/bin/true`, with no model call; this exercises Windows' wrapper entry point on Linux, not a Windows console. The [bootstrap script](../internal/broker/testdata/pane-start/reproduce-bootstrap.py) accepts the broker binary. Broker tests separately verify the resulting `launch_failed/shell_not_ready` errors and durable failure events for POSIX and Windows flows.

**Changes per brief point.** Paths below include source line numbers.

| Point | Implementation and evidence |
| --- | --- |
| 1. Pane startup | `internal/broker/integration_launcher.go:235`: bounded 15-second heartbeat confirmation with a top-level `runner_unavailable/shell_not_ready`. `internal/broker/integration_shell.go:121`: explanatory error and bounded 20-second marker confirmation. `internal/broker/integration_pane_launch.go:155` and `integration_lifecycle.go:281`: Windows bootstrap acknowledgement before detection, preserving `agent_not_detected` for a bootstrap that ran but produced no detected agent. Existing owned-pane cleanup, placement, environment, cwd and takeover paths remain in use. Tests: `internal/broker/integration_startup_test.go:16`, `:69`, `:90`, plus the existing takeover/teardown tests and extended native-wrapper test. Spec: `docs/protocol.md:358`, `:967`, `:989`. |
| 2. Member transition | `internal/broker/runner_launch.go:49`: enter `starting` before headless worktree/host/binary/workdir checks, retaining runner/member fencing and wake eligibility checks. Failed checks now emit actual `stopped → starting → stopped` transitions; the failure event is `starting → stopped`, `reason: launch_failed`. `internal/broker/launch_failure_test.go:105` covers worktree, binary and workdir failure. |
| 3. Schema examples | `schema/examples/app-ops/members.response.json:58`: pane `w1:p2`, workspace id `w1`, and `lead-frxn4a`, calculated from SHA-256/Crockford base32 for team `fmvq3a1x7k2` and member `lead`. All examples were searched; this was the stale Herdr identity block. |
| 4. List order | `docs/protocol.md:1302`: members by creation time, then id, both ascending, including human and requested removed rows. Also documented get-team spec/grid order (`:1266`), status/round counters (`:1282`), worktrees and removal results (`:1362`). Corrected the store comment at `internal/store/members.go:207`. `internal/broker/members_order_test.go:14` tests reverse insertion of equal timestamps, creation-time precedence, removed filtering and human inclusion. |
| 5. Socket path | `internal/herdr/server.go:95` computes and validates before even invoking Herdr status. `internal/herdr/server_socket_unix.go:16` follows release 0.9.5 HOME/XDG/default/named-session rules without cleaning path components; `:48` checks both sockets against platform `sun_path` capacity, reserving the terminating NUL and counting bytes. Windows uses a no-op check for its named pipes. `server_socket_unix_test.go:17`, `:49`, `:77`, `:105` cover paths, HOME/XDG precedence, no invocation/spawn, UTF-8, boundaries and the longer client socket. Spec: `docs/protocol.md:1397`. |

Herdr's socket base is `$XDG_CONFIG_HOME/herdr` when that variable is set, otherwise `$HOME/.config/herdr`; named sessions append `sessions/<name>`. Sockets are `herdr.sock` and `herdr-client.sock`. The [0.9.5 path implementation](https://raw.githubusercontent.com/herdrdev/herdr/v0.9.5/src/session.rs#L163) and [configuration directory rules](https://raw.githubusercontent.com/herdrdev/herdr/v0.9.5/src/config/io.rs#L30) were checked. `default` has no session subdirectory. Herdr status itself fails when its API path is too long, which is why a status-only preflight was insufficient; [probe evidence](../internal/broker/testdata/pane-start/socket-status-limitation.log) records this discovery.

The final [live socket checks](../internal/broker/testdata/pane-start/socket-live.log) returned `herdr_unavailable`, exit 33, `details.reason: socket_path_too_long`, with path/length/limit, in **64 ms** for a 166-byte API path and **57 ms** when the API path fit but the 111-byte client path did not. No server was started. The [socket script](../internal/broker/testdata/pane-start/reproduce-socket.py) accepts the broker binary.

**Verification.** All commands exited 0. Counts include subtests; parent test counts are supplied separately. Existing conditional, platform and subprocess-helper skips were retained; no test was removed or newly disabled. The manual private-session reproductions above supplement the opt-in live suites, which were not enabled because they include real harness turns.

| Command | Output/count |
| --- | --- |
| `go test -json ./...` | [Output](../internal/broker/testdata/pane-start/test.log): 16 packages passed; 2 have no test files. 1,310 test cases passed, including 387 top-level tests; 13 existing conditional/helper skips; 0 failures. |
| `go test -race -json ./internal/broker ./internal/herdr ./internal/store` | [Output](../internal/broker/testdata/pane-start/race.log): 3 packages, 332 cases passed, including 201 top-level tests; 10 existing conditional/helper skips; 0 failures or race reports. |
| `go vet ./...` | Linux/amd64: 18 packages, no output. [Log](../internal/broker/testdata/pane-start/vet-linux.log). |
| `GOOS=windows GOARCH=amd64 go vet ./...` | 18 packages, no output. [Log](../internal/broker/testdata/pane-start/vet-windows.log). |
| `GOOS=darwin GOARCH=arm64 go vet ./...` | 18 packages, no output. [Log](../internal/broker/testdata/pane-start/vet-darwin.log). |
| `GOOS=windows GOARCH=amd64 go test -c` for broker and herdr | Both test binaries compiled, 0 errors. Outputs: `/tmp/lugarn-pane-broker.test.exe`, `/tmp/lugarn-pane-herdr.test.exe`. |
| `gofmt -l .` | [Empty output](../internal/broker/testdata/pane-start/gofmt.log): 0 unformatted files. |
| `python3 scripts/validate-schemas.py` | [Output](../internal/broker/testdata/pane-start/schemas.log): 4 schemas, 61 examples, 46 documented enums; 0 failures. |
| `python3 scripts/check-migrations.py` | [Output](../internal/broker/testdata/pane-start/migrations.log): 1 migration, 33 SQL enum columns match; smoke, integrity, rejection and query-plan checks passed; 0 failures. |
| `git diff --check` | No output. |

**Spec findings.** The promised member failure transition was correct; implementation timing was wrong. The Windows busy-pane wording overstated prompt readiness, and the earlier general pane-launch paragraph still claimed a replacement `PowerShell -NoProfile` despite the later native-capsule flow. Both descriptions now match the implementation. Startup timeouts describe unconfirmed bootstrap, not certainty that a shell question caused the failure. No other spec contradiction was found in this scope.

Implementation commits: `1455eab` (state), `6d87f11` and `bd25ea3` (socket checks), `425e1c8` (schema example), `513d4ce` (ordering), `669d78d` (pane startup diagnostics). All end with the requested Codex coauthor trailer. No push was performed.
