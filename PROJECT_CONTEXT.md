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

- **Language**: Go 1.25.5 (see `go.mod`)
- **CLI framework**: spf13/cobra
- **Transports**: gorilla/websocket (default) and nats-io/nats.go (`--nats`)
- **Terminal handling**: golang.org/x/term (raw mode, cursor control)
- **Configuration**: none on disk — the port is the only setting, and it comes from the command line
- **Dev environment**: a local Go toolchain and `make`, or the dev sandbox — a pinned Go 1.25.5 container. `docker-compose.yml` carries both the sandbox and a local NATS server for experiments; nothing in the build or the gate needs a container.

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
internal/data/app/          the in-memory configuration (the port)
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
- **No configuration file** — the only setting is the port, so there is no
  state to migrate, no file to protect, and nothing to write in the user's
  home directory. (2026-01)
- **The `ct` short alias ships next to the `cthrone` binary** — a name typed
  many times a day should be short. (2026-01)
- **The release archive carries the README and the licenses** — a download has
  to be usable and lawful without the repository. (2026-01)
- **An Android arm64 binary ships alongside the Windows and Linux archives** —
  the tool is meant to run on a phone. (2026-01)

## Topology

- kind: cli, path: ., purpose: the single binary — commands, transports, and the chat loop
- kind: library, path: internal/, purpose: the transport packages

There is one component: the repository root *is* the Go module.

## Directory Layout

```text
cmd/                 cobra commands: connect, host, scan
internal/core/       one package per transport (websocket, nats)
internal/handlers/   one package per transport (websocket, nats)
internal/data/app/   the in-memory configuration
scripts/             add_to_path installers + the dev sandbox runtime
docker-compose.yml   the dev sandbox and a local NATS server
```

## Key Commands

- **Build**: `go build ./...`
- **Run locally**: `go run ./ --help`
- **Tests**: `go test ./...`
- **Format check**: `gofmt -l .`
- **Vet**: `go vet ./...`
- **Full gate**: `make check`
- **Release archives**: `make release` (writes to `build/`)

## Status

- **Done**: the websocket transport (host, connect, scan), the NATS transport, the terminal chat loop, the `ct` short alias, the version flag, release archives for Windows and Linux plus an Android arm64 binary.
- **In progress**: the tool has no authentication and no encryption on either transport, and the websocket server binds every interface (issue #11).

## Backlog

1. Decide and implement the transport security model — authentication, `wss://`, and a loopback default (issue #11).
2. Add the first tests. The repository has no test file at all, so `make check` currently proves only that the code is formatted, vetted, and buildable.
3. Delete the leftover `feature/*` branches — the branch gate rejects their names.
4. Release `v0.2.0` to `main`, which is 11 commits behind `dev`.

## Notes

- The `ct` alias is the same binary under a second name; both are shipped in
  the release archive.
- The websocket server listens on `:8080` — every interface — and the client
  dials plain `ws://`. Messages travel in plaintext and any client that can
  reach the port gets a session. See issue #11.
- The `docker-compose.yml` broker runs with `-js` (JetStream), so it also
  persists the messages it carries.
- `docker-compose.yml` is a convenience for local experiments, not a
  dependency: the build, the gate, and the websocket transport need nothing
  but Go.
- Windows uses `cmd` to execute commands, Linux uses `sh`; the `add_to_path`
  installers exist for both.
- There is no configuration file, so there is nothing in the user's home
  directory to protect and nothing to keep out of the repository.
