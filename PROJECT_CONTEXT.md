# Project Context

> Structured identity for this project. `AGENTS.md` is the working document an
> agent reads first; this file holds the facts behind it. Update both when the
> project changes.

---

## Identity

- **Project Name**: cthrone
- **One-liner**: A terminal chat tool that carries messages from one device to another, over a websocket or a NATS server.
- **GitHub repository**: https://github.com/fiwon123/cthrone

## Problem

Two devices that need to exchange a message — a phone and a laptop, a sensor
and a workstation — normally need an account, a hosted server, or a GUI. For a
short internal exchange, that is more setup than the message is worth.

## Goal

A user can start a chat server on one device, connect from another with one
command, and exchange messages in the terminal — over the local network or
through a NATS broker.

## Stack

- **Language**: Go 1.26.10 (see `go.mod`)
- **CLI framework**: spf13/cobra
- **Transports**: gorilla/websocket (default) and nats-io/nats.go (`--nats`)
- **Terminal handling**: golang.org/x/term (raw mode, cursor control)
- **Configuration**: no configuration file — settings come from flags and the environment (`--port`, `--token`, `--token-file`, `--tls`, `--cert-fingerprint`, `--insecure`, and per command `--nats`, `--nats-url`, `--host`, `--allow-anonymous`, `--name`; `CTHRONE_TOKEN`, `NATS_USER`, `NATS_PASSWORD`). The only files written are the shared secret and the TLS material, generated in `~/.config/cthrone` on first use
- **Dev environment**: a local Go toolchain and `make`, or the dev sandbox — a pinned Go 1.26.10 container. `docker-compose.yml` carries both the sandbox and a local NATS server for experiments; nothing in the build or the gate needs a container.

## Architecture

A cobra command is a thin shell over a *transport* package. The transport is
selected at the command level — `--nats` chooses the NATS path, its absence
chooses the websocket path — and both paths meet at the same chat loop.

```text
cmd/<verb>/                 cobra command: flags and argument parsing
  ↓
internal/core/<transport>/  the operation: connect, host, scan
  ↓
internal/handlers/<transport>/  the rules: the websocket and NATS sessions
  ↓
internal/data/app/          the in-memory configuration (port, bind address, device name)
```

`internal/core` decides *what* to do (`connect`, `host`, `scan`); the handlers
decide *how* the transport does it. `scan` walks the local subnet and reports
the addresses that answer on the chat port.

## Design Decisions

- **Two interchangeable transports behind one command set** — the user picks
  the transport with a flag, and the chat experience is the same either way.
  (2026-01)
- **The websocket transport is the default** — it needs nothing but the two
  binaries, while NATS needs a broker. (2026-01)
- **No configuration file** — settings come from flags and the environment, so
  there is no state to migrate. The two files the tool writes (`~/.config/cthrone/token`,
  `cert.pem`/`key.pem`) are generated on first use and are secrets, not
  configuration. (2026-01, corrected 2026-10)
- **The `ct` short alias ships next to the `cthrone` binary** — a name typed
  many times a day should be short. (2026-01)
- **The release archive carries the README and the licenses** — a download has
  to be usable and lawful without the repository. (2026-01)
- **An Android arm64 binary ships alongside the desktop archives** —
  the tool is meant to run on a phone. (2026-01)

## Topology

- kind: cli, path: ., purpose: the single binary — commands, transports, and the chat loop
- kind: library, path: internal/, purpose: the transport packages and the supporting ones (auth, data, discovery, exec, session, shutdown)

There is one component: the repository root *is* the Go module. The Fyne GUI
lives in `ui/` behind the `gui` build tag; without it, `ui.Run` prints a message
and exits.

## Directory Layout

```text
cmd/                 cobra commands: connect, host, scan, ui, recv, exec (+ the assets package)
internal/core/       one package per transport (websocket, nats)
internal/handlers/   one package per transport (websocket, nats)
internal/data/app/   the in-memory configuration
internal/auth/       the shared secret and the TLS material
internal/discovery/  the device identity kept on disk
internal/exec/       the allowlist behind `recv`/`exec`
internal/session/    the message hub the GUI uses
internal/shutdown/   graceful stop and terminal restore
ui/                  the Fyne GUI, behind the `gui` build tag
scripts/             add_to_path installers + the dev sandbox runtime
.github/workflows/   CI, the branch gate, and the draft-release workflow
docker-compose.yml   the dev sandbox and a local NATS server
```

## Key Commands

- **Build**: `go build ./...`
- **Run locally**: `go run ./ --help`
- **Tests**: `go test ./...`
- **Format check**: `gofmt -l .`
- **Vet**: `go vet ./...`
- **Full gate**: `make check`
- **Release archives**: `make release` builds every CLI archive into `build/`;
  `make release-gui` builds the GUI archive a Linux host can produce. The
  release workflow (`.github/workflows/release.yml`) runs both in CI and opens
  a draft for a human to publish.

## Status

- **Done**: the websocket transport (host, connect, scan), the NATS transport, the terminal chat loop, the `ct` short alias, the version flag, device identity/discovery, a session hub, opt-in remote execution behind an allowlist (`recv`/`exec`), CLI release archives for Windows, Linux, Android and macOS plus a Fyne GUI archive for the three desktop platforms, and a draft-release workflow that builds and attaches them all. `v0.5.0` is the published release.
- **Defaults, not guarantees**: the websocket transport requires the shared secret unless the host passes `--allow-anonymous`; encryption is **opt-in** — `--tls` on the websocket path, a `tls://` NATS URL (with `--cert-fingerprint`, or `--insecure`) on the NATS path; the websocket host binds **every interface** unless `--host` narrows it; and a NATS deployment is only as protected as its broker. The model behind these is issue #11.
- **Open**: see Backlog.

## Backlog

1. Cut the next release to carry what `dev` holds beyond `main` — currently the Fyne 2.8.2 bump. Its release run is the first native GUI build against that version.
2. Cover the seven packages that still have no test file: `main`, `cmd/assets`, `cmd/exec`, `cmd/recv`, `cmd/ui`, `internal/data/app`, `ui`. The transports, the scanner and the CLI commands are covered.
3. Delete the leftover `feature/*` branches — the branch gate rejects their names.
4. Reconcile the coverage claims in `AGENTS.md` and `.opencode/instructions/testing.md` with the real suite (issue #81).
5. An APK is not started — the Android archive is the CLI, and a Kotlin UI is issue #9.

## Notes

- The `ct` alias is the same binary under a second name; both are shipped in
  the release archive.
- The websocket transport defaults to port `8080` on **every interface**; the
  client dials `ws://` (or `wss://` with `--tls`) and the server serves https
  under the same flag. The host generates a shared secret on its first run
  (`~/.config/cthrone/token`, mode `0600`) and refuses a client that does not
  send it, unless the host passed `--allow-anonymous`. With `--tls` the host's
  certificate is pinned by `--cert-fingerprint`; `--insecure` keeps the
  encryption but skips that check. See issue #11 for the model.
- The `docker-compose.yml` broker requires the credentials in `NATS_USER` and
  `NATS_PASSWORD` (`nats.conf`), and runs **without** JetStream, so a
  conversation does not outlive the session it happened in.
- `docker-compose.yml` is a convenience for local experiments, not a
  dependency: the build, the gate, and the websocket transport need nothing
  but Go.
- Windows uses `cmd` to execute commands, Linux uses `sh`; the `add_to_path`
  installers exist for both.
- The tool writes two things into the user's home directory, both on first use
  and both secret: the shared secret (`~/.config/cthrone/token`, `0600`) and the
  TLS material (`cert.pem`, and `key.pem` at `0600`). Neither is ever committed;
  see `AGENTS.md` → Environment and Secret-File Rules.
