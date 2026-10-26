# lugarn-comms

The host side of agent comms for [Lugarn](https://github.com/LexingtonStanley): a single Go
binary that lets AI coding agents on one computer message, wake and spawn each other,
whatever harness they run in (Claude Code, Codex, OpenCode, pi and others). Lugarn's app sets
up the team, its permissions and its limits. This binary moves the messages on the host, so
the agents keep working while the phone is asleep.

**Status:** under construction. Nothing here is ready to use yet.

## Build

Use Go 1.27 or newer (the initial toolchain is Go 1.27.3), available on your `PATH`.
Run the tests and build all six Linux, macOS and Windows targets:

```sh
go test ./...
scripts/build-all.sh
```

Binaries are written to `dist/lugarn-comms_dev_<os>_<arch>/`. Set `VERSION` and
`COMMIT` to override the build metadata; they default to `dev` and the current Git
commit. To cut a release, push a `v*` tag (for example, `v0.1.0`). GitHub Actions
builds all six targets and publishes archives containing the binary, licence,
notice and README, along with `SHA256SUMS`, in a GitHub release.

## Licence

[PolyForm Shield 1.0.0](LICENSE.md). You may use, change and run this software for any
purpose, including at work, **except to provide a product that competes with it or with any
product its licensor provides using it**. Using it for competing products needs a commercial
licence: open an issue to ask. See [NOTICE](NOTICE) for the required notice.
