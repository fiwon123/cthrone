# Project Context

> Structured identity for this project. `AGENTS.md` is the working document an
> agent reads first; this file holds the facts behind it. Update both when the
> project changes.

---

## Identity

- **Project Name**: cthrone
- **One-liner**: A chat tool that carries messages from one device to another, over a websocket or a NATS server — from a terminal or from a touch-friendly app on a phone.
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
- **GUI**: Fyne 2.8.2, behind the `gui` build tag — one GUI for the desktop and for the Android app
- **Packaging**: `fyne` CLI `v1.26.1` for the APK; `make release` for the CLI archives
- **Configuration**: no configuration file — settings come from flags and the environment (`--port`, `--token`, `--token-file`, `--peers-file`, `--tls`, `--cert-fingerprint`, `--insecure`, and per command `--nats`, `--nats-url`, `--host`, `--name`; `CTHRONE_TOKEN`, `NATS_USER`, `NATS_PASSWORD`). What the tool writes for itself lives in the platform's user configuration directory — `~/.config/cthrone` on Linux, `~/Library/Application Support/cthrone` on macOS, `%AppData%\cthrone` on Windows — for the CLI and the desktop GUI, and in the app's private storage on Android: this device's key and the pins it trusts, the TLS material, and — only when one is configured — the shared secret
- **Dev environment**: a local Go toolchain and `make`, or the dev sandbox — a pinned Go 1.26.10 container. `docker-compose.yml` carries both the sandbox and a local NATS server for experiments; nothing in the build or the gate needs a container. The APK is the exception: it needs the Android SDK, an NDK and the `fyne` CLI, so it is built in CI

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
internal/e2e/               the end-to-end layer every session is wrapped in
  ↓
internal/chat/ + ui/        the terminal loop and the GUI, over one session.Session
```

`internal/core` decides *what* to do (`connect`, `host`, `scan`); the handlers
decide *how* the transport does it. `scan` walks the local subnet and reports
the addresses that answer on the chat port. `internal/pairing` decides *which
credential* a session speaks — the shared secret when one is configured, this
device's key otherwise — so the transports cannot disagree.

The GUI is not a fourth transport: `ui/` reads and writes through the same
`internal/session.Session` the terminal loops use, and `mobile/` is a second
`main` that starts that GUI on a phone. So the terminal and the app share the
transports, the credential and the TLS rules, and differ only in how they read
and write.

## Design Decisions

- **Two interchangeable transports behind one command set** — the user picks
  the transport with a flag, and the chat experience is the same either way.
  (2026-01)
- **The websocket transport is the default** — it needs nothing but the two
  binaries, while NATS needs a broker. (2026-01)
- **No configuration file** — settings come from flags and the environment, so
  there is no state to migrate. What the tool writes for itself — in the
  platform user configuration directory (`identity.key`, `peers.json`,
  `cert.pem`/`key.pem`, and a `token` only when a secret is configured) — is
  generated on first use and is device state or secret material, not
  configuration. (2026-01, corrected 2026-10)
- **The `ct` short alias ships next to the `cthrone` binary** — a name typed
  many times a day should be short. (2026-01)
- **The release archive carries the README and the licenses** — a download has
  to be usable and lawful without the repository. (2026-01)
- **An Android arm64 binary ships alongside the desktop archives** —
  the tool is meant to run on a phone. (2026-01)
- **The phone app is Fyne, not a Kotlin rewrite** — one Go codebase builds the
  desktop GUI and the Android APK, and the same code can become an iOS build;
  a second UI in a second language would have duplicated the transports and the
  security model. (2026-10, issue #84; supersedes issue #9)
- **Encryption is built in, not opt-in** — every session is sealed on both
  transports and a peer that cannot authenticate is refused rather than
  downgraded to plaintext. There is no anonymous mode. (2026-10, issue
  #11/#109, kept by #125)
- **A device pairs by its own key, with a shared secret as the alternative** —
  the default is a per-device ed25519 key, pinned by the peer on first contact
  and compared by fingerprint on both screens; a different key under a pinned
  name is refused, and `cthrone peers forget` is the way back. The pin is
  recorded only after the handshake has proved the peer holds the key — a
  hello alone records nothing, so a stranger cannot claim a name from a frame
  it never proved. The shared secret stays for one release so a v0.6.0 peer
  still connects, and the handshake version gates the two models. Pairing by
  key is what makes the tool usable from a phone, which has no terminal and no
  clipboard that crosses devices. (2026-10, issue #125; supersedes the "the
  secret is the identity" rule of #11/#109)

## Topology

- kind: cli, path: ., purpose: the single binary — commands, transports, and the chat loop
- kind: library, path: internal/, purpose: the transport packages and the supporting ones (auth, data, discovery, pairing, exec, session, shutdown)
- kind: desktop, path: ui/, purpose: the Fyne GUI behind the `gui` build tag, shared with the phone app
- kind: mobile, path: mobile/, purpose: a second `main` — the Android/iOS entry point that starts `ui`

There is one module: the repository root *is* the Go module. Without the `gui`
tag, `ui.Run` prints a message and exits, so the CLI needs no GUI toolkit.

## Directory Layout

```text
cmd/                 cobra commands: connect, host, scan, peers, ui, recv, exec (+ the assets package)
internal/core/       one package per transport (websocket, nats)
internal/handlers/   one package per transport (websocket, nats)
internal/data/app/   the in-memory configuration
internal/pairing/    the credential a session uses: the shared secret, or this device's key
internal/auth/       the shared secret and the TLS material
internal/chat/       the terminal loop both transports run a session on
internal/e2e/        the end-to-end layer every session is wrapped in
internal/wire/       the CTH1 envelope and the file chunk arithmetic
internal/discovery/  the device identity kept on disk and the pins trusted on first contact
internal/exec/       the allowlist behind `recv`/`exec`
internal/session/    the message hub the GUI uses
internal/shutdown/   graceful stop and terminal restore
ui/                  the Fyne GUI, behind the `gui` build tag
mobile/              the Android/iOS entry point: a second `main` that starts `ui`
assets/              the app icon the APK carries
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
  `make release-gui` builds the GUI archive a Linux host can produce;
  `make gui-android` builds the debug-signed APK (needs the Android SDK, an NDK
  and the `fyne` CLI). The release workflow (`.github/workflows/release.yml`)
  runs all three in CI and opens a draft for a human to publish.

## Status

- **Done**: the websocket transport (host, connect, scan), the NATS transport, the terminal chat loop, the `ct` short alias, the version flag, device identity/discovery, a session hub, opt-in remote execution behind an allowlist (`recv`/`exec`), end-to-end encryption of every session (`internal/e2e`), CLI release archives for Windows, Linux, Android and macOS, a Fyne GUI archive for the three desktop platforms, a debug-signed Android APK built from the same GUI, a draft-release workflow that builds and attaches them all, and pairing by device key with a pin on first contact (`cthrone peers`). `v0.6.0` is the published release.
- **Encrypted and authenticated by default**: every session is sealed on both
  transports, and exactly one credential authenticates it — this device's
  ed25519 key when no secret is configured, or the shared secret when one is
  (the v0.6.0 path). X25519 + HKDF per direction, AES-256-GCM, a sequence
  number that fails a replay, and a handshake version that gates the two
  models: a peer speaking the other one is refused, never downgraded. There is
  no plaintext or anonymous mode. `--tls` adds transport encryption (websocket
  `https`/`wss://`, a `tls://` NATS URL), pinned with `--cert-fingerprint` or
  skipped with `--insecure`. What stays visible is the shape: frame sizes,
  timing, and the subject or address. On a websocket the host is the
  correspondent, so the end-to-end layer protects the wire; on NATS it
  protects the payload from the broker. The model is issue #125, superseding
  the "the secret is the identity" rule of #11/#109.
- **Open**: see Backlog.

## Backlog

1. Publish the `v0.6.0` release when its milestone is done — the draft comes from `release.yml` and a human presses publish.
2. Delete the leftover `feature/*` branches — the branch gate rejects their names.
3. Install the APK on a device and record the result — nothing in CI runs it, so "builds for Android" is still a build claim.
4. An iOS build of the same code needs macOS and Xcode; it is a follow-up to issue #84, not a rewrite.

## Notes

- The `ct` alias is the same binary under a second name; both are shipped in
  the release archive.
- The websocket transport defaults to port `8080` on **every interface**; the
  client dials `ws://` (or `wss://` with `--tls`) and the server serves https
  under the same flag. With no secret configured — the default — the host
  pairs by device key: every client completes the sealed handshake, and the
  caller receives the session only once a peer has authenticated. With a
  secret configured the handler additionally compares its header and answers
  `401`. With `--tls` the host's certificate is pinned by
  `--cert-fingerprint`; `--insecure` keeps the encryption but skips that
  check. See issues #11, #109 and #125 for the model.
- The `docker-compose.yml` broker requires the credentials in `NATS_USER` and
  `NATS_PASSWORD` (`nats.conf`), and runs **without** JetStream, so a
  conversation does not outlive the session it happened in. It carries
  ciphertext, because every payload is sealed before it is published.
- `docker-compose.yml` is a convenience for local experiments, not a
  dependency: the build, the gate, and the websocket transport need nothing
  but Go.
- Windows uses `cmd` to execute commands, Linux uses `sh`; the `add_to_path`
  installers exist for both.
- The tool writes its device state into the platform's user configuration
  directory (`internal/discovery.ConfigDir`, i.e. `os.UserConfigDir` +
  `cthrone`): this device's key (`identity.key`, `0600`), the pins
  (`peers.json`, `0600`), the TLS material (`cert.pem`, and `key.pem` at
  `0600`), and — only when one is configured — the shared secret (`token`,
  `0600`). None of it is ever committed; see `AGENTS.md` → Environment and
  Secret-File Rules.
- The Android app writes the same material into its **own private storage**,
  because a phone has no user configuration directory it can rely on. A phone
  that hosts shows its own fingerprint in a dialog and copies nothing: the
  other device connects and the two screens are compared. A shared secret is
  shown and copied to the clipboard instead, and only when one is configured.
- **The APK is built, not observed.** Nothing in CI installs it on a device or
  an emulator; `make gui-android` and the release job prove that it packages,
  and that it carries an `AndroidManifest.xml` and an arm64 library, and no
  more. A claim that Android support *works* needs a device.
