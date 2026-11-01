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
- **Transports**: gorilla/websocket (default), nats-io/nats.go (`--nats`), and
  tinygo.org/x/bluetooth (`--bluetooth`: BlueZ over D-Bus on Linux, WinRT on
  Windows — the Windows path is compiled and never run)
- **Terminal handling**: golang.org/x/term (raw mode, cursor control)
- **GUI**: Fyne 2.8.2, behind the `gui` build tag — one GUI for the desktop and for the Android app
- **Packaging**: `fyne` CLI `v1.26.1` for the APK; `make release` for the CLI archives
- **Configuration**: no configuration file — settings come from flags and the environment (`--port`, `--token`, `--token-file`, `--peers-file`, `--no-tls`, `--tls`, `--cert-fingerprint`, `--insecure`, and per command `--nats`, `--nats-url`, `--host`, `--name`; `CTHRONE_TOKEN`, `NATS_USER`, `NATS_PASSWORD`). The websocket transport is encrypted by default, `--no-tls` is the opt-out, and `--tls` is accepted as a no-op so a v0.6.0 command line still runs. What the tool writes for itself lives in the platform's user configuration directory — `~/.config/cthrone` on Linux, `~/Library/Application Support/cthrone` on macOS, `%AppData%\cthrone` on Windows — for the CLI and the desktop GUI, and in the app's private storage on Android: this device's key, the pins it trusts (device keys and host certificates), and the TLS material. A shared secret is used only when it is named — `--token`, `CTHRONE_TOKEN` or `--token-file` — and the tool writes no `token` file; one left by v0.6.0 is reported, never read on its own (issue #163)
- **Dev environment**: a local Go toolchain and `make`, or the dev sandbox — a pinned Go 1.26.10 container. `docker-compose.yml` carries both the sandbox and a local NATS server for experiments; nothing in the build or the gate needs a container. The APK is the exception: it needs the Android SDK, an NDK and the `fyne` CLI, so it is built in CI

## Architecture

A cobra command is a thin shell over a *transport* package. The transport is
selected at the command level — `--nats` chooses the NATS path, `--bluetooth`
the radio, and neither chooses the websocket path — and all three meet at the
same chat loop over the same sealed session.

```text
cmd/<verb>/                 cobra command: flags and argument parsing
  ↓
internal/core/<transport>/  the operation: connect, host, scan
  ↓
internal/handlers/<transport>/  the rules: the websocket and NATS sessions
  ↓
internal/e2e/               the end-to-end layer every session is wrapped in
  ↓
internal/chat/ + ui/        the terminal loop and the GUI, over one session.Session and one hub
```

`internal/core` decides *what* to do (`connect`, `host`, `scan`); the handlers
decide *how* the transport does it. `scan` walks the `/24` the default route
uses — not the first private address an adapter happens to list, which on a
machine with a docker bridge or a VPN is often the wrong one — and reports the
addresses that answer on the chat port, never this device's own, so a host does
not offer itself as a peer. `--subnet <cidr>` adds a segment the default route
does not cover (repeatable), and `--all-subnets` walks the `/24` of every local
interface; that reaches a second Wi-Fi, a VLAN or a wired segment **only when
the router routes between them**, because the beacon's broadcast does not cross
a subnet, and a scan cannot cross a NAT or client isolation. The walk is bounded
by a worker pool and a hard cap on the address count, so a mistyped `/16` is
refused rather than dialled. `internal/pairing` decides *which credential* a
session speaks — the shared secret when the user named one, this device's key
otherwise — so the transports cannot disagree.

A host is a **room**, not a set of two-party conversations: `session.Hub` holds
every authenticated session, relays each message to all the others with the
sender named, and reports joins and departures. The terminal console
(`chat.Room`) is one reader of that room — one stdin, so two devices never
compete for the keyboard — and the GUI is another. Relay attribution is
in-band: text is prefixed with the sender's label, and a file frame carries the
label in an optional `wire.Header.Sender` field, which an older peer ignores.

The radio is the exception to "the transports are interchangeable", and the
exception belongs to Bluetooth rather than to a shortcut: a BlueZ peripheral
cannot tell which central wrote to a characteristic, so a host serves **one
device at a time** and the slot frees when the keepalive notices the device has
gone quiet; discovery is a BLE scan rather than the UDP beacon, so `cthrone scan
--bluetooth` lists the devices in range; and a 20-byte packet makes text the
only practical payload. The GATT layer is built for Linux and Windows
(`//go:build (linux || windows) && !android` — Android satisfies the `linux`
tag and has no BlueZ, so it is excluded explicitly), and
`internal/core/bluetooth/unsupported.go` answers everywhere else. The Windows
path compiles and has never been run. The framing, the session and the
keepalive are portable and tested on an in-memory link; no gate opens a radio.

The GUI is not a fourth transport: `ui/` reads and writes through the same
`internal/session.Session` the terminal loops use, and `mobile/` is a second
`main` that starts that GUI on a phone. So the terminal and the app share the
transports, the credential and the TLS rules, and differ only in how they read
and write. A phone build leaves Bluetooth out of the transport select, because
the Android binding is separate work (issue #206).

## Design Decisions

- **Interchangeable transports behind one command set** — the user picks the
  transport with a flag, and the chat experience is the same either way. The
  radio keeps that promise within its own limits — one device at a time, text
  over a 20-byte packet, the OS's Bluetooth permission — and the limits are the
  radio's, stated rather than hidden. (2026-01; the radio added 2026-11, issue
  #202)
- **A host is a room, and a message reaches every device in it** — the host
  relays what one device sends to all the others with the sender named, rather
  than pairing two devices in private. Text carries the name in-band, because
  the wire has no metadata slot for an unwrapped line; a file carries it in an
  optional header field, so an older peer still joins and still reads the
  files. The terminal host has one console for the room, so two devices never
  compete for the keyboard, and the same `session.Hub` is what the GUI reads.
  (2026-10, issue #199)
- **The websocket transport is the default** — it needs nothing but the two
  binaries, while NATS needs a broker. (2026-01)
- **No configuration file** — settings come from flags and the environment, so
  there is no state to migrate. What the tool writes for itself — in the
  platform user configuration directory (`identity.key`, `peers.json`,
  `cert.pem`/`key.pem`) — is generated on first use and is device state or
  secret material, not configuration. (2026-01, corrected 2026-10)
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
- **Encryption is built in, not opt-in** — every session is sealed on every
  transport and a peer that cannot authenticate is refused rather than
  downgraded to plaintext. There is no anonymous mode. (2026-10, issue
  #11/#109, kept by #125; #202 added a carrier and changed no rule)
- **A device pairs by its own key, with a shared secret as the alternative** —
  the default is a per-device ed25519 key, pinned by the peer on first contact
  and compared by fingerprint on both screens; a different key under a pinned
  name is refused, and `cthrone peers forget` is the way back. The pin is
  recorded only after the handshake has proved the peer holds the key — a
  hello alone records nothing, so a stranger cannot claim a name from a frame
  it never proved. A websocket first contact also shows a six-digit code on
  both screens and the host asks before the device joins; the host withholds
  its confirm until the answer, so an unaccepted peer is neither authenticated
  nor pinned, and an unanswered question expires into a refusal. (2026-10,
  issue #145; a NATS subject has no host to ask, so a first contact there is
  pinned without a prompt.) The shared secret stays for one release so a
  v0.6.0 peer still connects, and the handshake version gates the two models.
  A secret counts only when it is named — `--token`, `CTHRONE_TOKEN` or
  `--token-file`; a `token` file in the configuration directory is no longer
  read on its own, because a file nobody remembered writing silently moved an
  upgraded device into the secret model and asked the peer for a value nobody
  chose (2026-10, issue #163).
  Pairing by key is what makes the tool usable from a phone, which has no
  terminal and no clipboard that crosses devices. (2026-10, issue #125;
  supersedes the "the secret is the identity" rule of #11/#109)

## Topology

- kind: cli, path: ., purpose: the single binary — commands, transports, and the chat loop
- kind: library, path: internal/, purpose: the transport packages and the supporting ones (auth, data, discovery, pairing, exec, session, shutdown)
- kind: desktop, path: ui/, purpose: the Fyne GUI behind the `gui` build tag, shared with the phone app
- kind: mobile, path: mobile/, purpose: a second `main` — the Android/iOS entry point that starts `ui`

There is one module: the repository root *is* the Go module. Without the `gui`
tag, `ui.Run` prints a message and exits, so the CLI needs no GUI toolkit.

## Directory Layout

```text
cmd/                 cobra commands: connect, host, scan, peers, ui, recv, exec, update (+ the assets package)
internal/core/       one package per transport (websocket, nats, bluetooth)
internal/handlers/   one package per transport (websocket, nats, bluetooth)
internal/data/app/   the in-memory configuration
internal/pairing/    the credential a session uses: the shared secret, or this device's key
internal/auth/       the shared secret and the TLS material
internal/chat/       the terminal loop, and the console a host runs its room on
internal/e2e/        the end-to-end layer every session is wrapped in
internal/wire/       the CTH1 envelope and the file chunk arithmetic
internal/discovery/  the device identity kept on disk and the pins trusted on first contact
internal/exec/       the allowlist behind `recv`/`exec`
internal/session/    the transport-agnostic session and hub the CLI and the GUI share
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

- **Done**: the websocket transport (host, connect, scan), the NATS transport, the terminal chat loop, the `ct` short alias, the version flag, device identity/discovery, a session hub, opt-in remote execution behind an allowlist (`recv`/`exec`), end-to-end encryption of every session (`internal/e2e`), transport encryption by default with the host certificate pinned on first contact, CLI release archives for Windows, Linux, Android and macOS, a Fyne GUI archive for the three desktop platforms, a debug-signed Android APK built from the same GUI, a draft-release workflow that builds and attaches them all, pairing by device key with a six-digit first-contact code and a pin on first contact (`cthrone peers`), buttons and tab labels that copy on a long press, a send order that is serialized end to end and a websocket keepalive that notices a silent link instead of waiting for the next write (#192, #193), and the device name shown beside its address in the Devices list, in chat and in `cthrone scan` (#194), a host that is a room rather than a pair: every device that joins sees every other device's messages and files, relayed with the sender named (#199), `cthrone scan --subnet`/`--all-subnets`, which walks a segment the default route does not cover when the router routes between them (#200), and a Bluetooth transport (`--bluetooth`) that carries the same sealed session over a BLE link, one device at a time, with framing, reassembly and a keepalive tested without a radio (#202). The Linux GATT layer speaks BlueZ; the Android binding is separate work (#206), and a phone build refuses the transport and leaves the option out of the GUI. `v0.9.0` is the published release; it also carries the v0.8.6 fixes (#192, #193, #194, #197) and the CTH1 text-frame fix (#201), which never shipped under their own version.
- **Encrypted and authenticated by default**: every session is sealed on every
  transport — a Bluetooth link carries ciphertext too, whatever the radio's own
  pairing — and exactly one credential authenticates it — this device's
  ed25519 key when no secret is configured, or the shared secret when the user
  names one — `--token`, `CTHRONE_TOKEN` or `--token-file` — which is the
  v0.6.0 path. X25519 + HKDF per direction, AES-256-GCM, a sequence
  number that fails a replay, and a handshake version that gates the two
  models: a peer speaking the other one is refused, never downgraded. There is
  no plaintext or anonymous mode. The websocket **connection** is encrypted
  too: the host serves `https`, the client dials `wss://`, and the self-signed
  certificate is pinned on first contact in `peers.json` under the dialed
  `host:port` — a different certificate at a pinned address is refused, and
  `cthrone peers forget <address>` is the way back. `--no-tls` opts out for a
  v0.6.0 peer, `--cert-fingerprint` pins by hand, `--insecure` skips the check,
  and a `tls://` NATS URL asks for the same pinning on the broker. What stays
  visible is the shape: frame sizes, timing, and the subject or address. On a
  websocket the host is the correspondent, so the transport and end-to-end
  layers protect the wire; on NATS the end-to-end layer protects the payload
  from the broker. The model is issue #125, superseding the "the secret is the
  identity" rule of #11/#109; the transport default is issue #124.
- **The GUI widget tree is executed, not just compiled**: `make check` and the
  `CI (Go)` job run `CGO_ENABLED=0 go test -tags "gui ci" ./ui/...`, which
  swaps Fyne's GL driver for the software one, so a transfer progress row, an
  inline decodable image, a long press that copies a button or tab label, and
  the two regressions that broke a shipped GUI — the blank window (#186) and
  the unclickable Setup tab (#189) — are asserted headless as painted-versus-
  hit-test pixel checks. A native rendered frame and the APK on a device are
  still unverified (#155).
- **Open**: see Backlog.

## Backlog

1. Install the APK on a device and record the result — nothing in CI runs it, so "builds for Android" is still a build claim.
2. Prove the Bluetooth transport on two real devices — Linux to Linux, and then a phone once #206 binds the Android stack; no gate in this repository opens a radio.
3. An iOS build of the same code needs macOS and Xcode; it is a follow-up to issue #84, not a rewrite.

## Notes

- The `ct` alias is the same binary under a second name; both are shipped in
  the release archive.
- The Bluetooth transport has no address and no port. `cthrone host --bluetooth`
  advertises the service and serves one device at a time; `cthrone connect
  --bluetooth [name|address]` scans and dials the first (or named) cthrone
  device; `cthrone scan --bluetooth` lists what advertises the service. The
  GATT layer is BlueZ over D-Bus on Linux (#202) and WinRT on Windows (#215,
  compiled but never run); Android is #206.
- The websocket transport defaults to port `8080` on **every interface**; the
  client dials `wss://` and the server serves https by default, and `--no-tls`
  puts both back on `ws://`. With no secret configured — the default — the host
  pairs by device key: every client completes the sealed handshake, and the
  caller receives the session only once a peer has authenticated. With a
  secret configured the handler additionally compares its header and answers
  `401`. The host's certificate is pinned on first contact; `--cert-fingerprint`
  pins it by hand, and `--insecure` keeps the encryption but skips the check.
  See issues #11, #109, #124 and #125 for the model.
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
  (`peers.json`, `0600` — device keys and host certificate fingerprints), and
  the TLS material (`cert.pem`, and `key.pem` at `0600`). None of it is ever
  committed; see `AGENTS.md` → Environment and Secret-File Rules. The tool
  writes **no** `token` file: a v0.6.0 installation may still have one there,
  and nothing reads it unless a run names it with `--token-file`.
- The Android app writes the same material into its **own private storage**,
  because a phone has no user configuration directory it can rely on. A phone
  that hosts shows its own fingerprint in a dialog and copies nothing: the
  other device connects and the two screens are compared. A shared secret is
  shown and copied to the clipboard instead, and only when the user typed one
  on the Setup tab.
- **The APK is built, not observed.** Nothing in CI installs it on a device or
  an emulator; `make gui-android` and the release job prove that it packages,
  and that it carries an `AndroidManifest.xml` and an arm64 library, and no
  more. A claim that Android support *works* needs a device.
