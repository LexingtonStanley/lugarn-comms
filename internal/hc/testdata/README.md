# hc 0.1.21 fixtures

Recorded on Linux in fresh `lugarn-test-*` sessions by
`scripts/record-hc-178.py --run` and `scripts/test-hc-178.py --run`.
The scripts reject existing sockets/state and the known default core id, then
stop their private core and remove its state in `finally`.

`hello.json`, `terminal.json`, and `subscribe.json` are real API results.
`events.json` contains ordinary lifecycle/status events from the broker scenario.
`recovery-events.json` contains a real missed snapshot and heartbeat, captured by
pausing a subscriber while a private fixture program emitted status transitions.
`probe-rpc.json` retains requests/responses, including name_taken, invalid_params,
finished, and not_found. `harness-status.json` records isolated startup observations
of installed OpenCode and pi, with temporary configuration homes and no prompts.
The native test peer replays these records; sequence and identity changes in unit
tests are deliberate fault injection, never calls to an installed core.

Notification shapes were also checked against the upstream API description:
https://github.com/herdrdev/herdr-core/blob/master/schema/README.md#events
