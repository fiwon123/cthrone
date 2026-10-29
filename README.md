<p align="center">
  <img src="assets/logo-128.png" alt="cthrone logo" width="128" height="128">
</p>

<h1 align="center">
  cthrone
</h1>

<p align="center">A terminal chat tool that carries messages from one device to another, over a websocket or a NATS server.</p>

<p align="center">
  <a href="https://github.com/fiwon123/cthrone/actions/workflows/ci.go.yml"><img src="https://github.com/fiwon123/cthrone/actions/workflows/ci.go.yml/badge.svg?branch=main" alt="CI"></a>
  <img src="https://img.shields.io/badge/go-1.26.10-informational" alt="Go 1.26.10">
  <a href="https://github.com/fiwon123/cthrone/blob/main/LICENSE"><img src="https://img.shields.io/badge/license-MIT-blue" alt="License"></a>
</p>

---

## Table of contents

- [About](#about)
- [Security](#security)
- [How to use](#how-to-use)
- [Install](#install)
- [Commands](#commands)
- [Architecture](#architecture)
- [Repository](#repository)
- [How to develop](#how-to-develop)
- [Troubleshooting](#troubleshooting)
- [Tech stack](#tech-stack)
- [License](#license)

## About

cthrone is a chat tool for the terminal. One device hosts a chat server, another
connects to it, and the two exchange messages in the terminal — no account, no
GUI, no configuration file.

There are two transports, and the command set is the same either way:

- **websocket** (the default) — one device runs the server, the other connects
  to it over the local network. Nothing else needs to be installed.
- **NATS** — both sides talk to a broker, so the two devices do not need to
  see each other directly. Useful when a broker is already running.

There is nothing to configure: the only setting is the port, and it comes from
the command line. The `ct` short alias ships next to the `cthrone` binary,
because a name you type many times a day should be short.

**Supported platforms**: Windows, Linux, macOS, and Android (arm64). On Android
there are two downloads: a **command-line program** that runs in a terminal app
on the phone, and an **APK** — the same chat with a touch-friendly GUI, which
installs like any other app. See [Android](#android) below.

> **Security**: every session is **end-to-end encrypted** with the shared
> secret — a device that does not hold it cannot join, and cannot read what
> passes between the ones that do, including a broker in the middle. The
> secret is required; there is no anonymous mode. `--tls` adds transport
> encryption on top and pins the host's certificate. See
> [Security](#security) for how it is set up, and
> [issue #11](https://github.com/fiwon123/cthrone/issues/11) for the model.

> **Status**: actively used, pre-1.0, and not yet covered by tests — see
> [How to develop](#how-to-develop).

## Security

Both transports need a **shared secret**: a websocket client that does not
present it is refused with a `401` before any session starts, and the NATS
transport reads the same secret. It does two jobs: it is the password that
lets a device in, and it is the material both devices derive their encryption
keys from. A device that presents the wrong one cannot complete the handshake,
and the session ends with a message saying why. There is no anonymous mode.

The secret is read from the first of these that has a value:

| Source | How |
|---|---|
| `--token` | the flag — **visible in the process list** to every other process on the machine, so it is for a quick manual run, not for daily use |
| `CTHRONE_TOKEN` | the environment — the one to use in a container or a script |
| the token file | `--token-file <path>`, or `~/.config/cthrone/token` by default |

The host **generates** a secret the first time it runs and writes it to the
token file with `0600` — a secret every process on the machine can read is
not a secret. It is never replaced afterwards, so two devices that share the
file keep agreeing.

One flag changes the shape of it:

- `--host <address>` binds the server to one interface instead of every
  interface. The default is every interface, because the tool is a chat
  between two devices on the same network.

### Encryption

Every session is encrypted end to end with the shared secret, whatever the
transport. Each pair of devices runs a handshake when they meet — a X25519 key
exchange, folded into a per-direction key with HKDF — and every message after
it travels as AES-256-GCM ciphertext with a sequence number that makes a
replayed or reordered frame fail. The handshake itself is authenticated by the
secret, so a device without it cannot make a session look encrypted and cannot
read one.

That is what makes the NATS transport private: **the broker carries
ciphertext**. It sees that two devices are talking on a subject and nothing
about what they say. Over a websocket the host is your correspondent rather
than a relay, so the end-to-end layer protects the wire, not the peer.

`--tls` adds **transport** encryption: the host serves `https`, and the client
connects over `wss://`.

The host's certificate is **self-signed** — there is no certificate
authority for a chat between two devices on a LAN. So the client does not
check it against an authority; it checks it against the **fingerprint** the
host prints:

```bash
cthrone host --tls
# Its fingerprint is c56e6a600a2398c7f13f14501e85508b9fe42ba050d5f0282c3653ed411821d9
```

Give that fingerprint to the other device:

```bash
cthrone connect 192.168.0.42 --tls --cert-fingerprint c56e6a600a2398c7f13f14501e85508b9fe42ba050d5f0282c3653ed411821d9
```

A certificate that does not match is refused, and the client says what it
got. The fingerprint is accepted with or without colons in it.

The host generates the certificate the first time it runs and keeps it —
`~/.config/cthrone/cert.pem` and `~/.config/cthrone/key.pem` (the key is
`0600`) — so the fingerprint does not change under a client that pinned it.

`--insecure` makes the client accept **any** certificate. The traffic is
still encrypted; the host is simply not checked, which means anyone who can
intercept the connection can present their own. It is for a trusted network,
and it has to be asked for.

Without `--tls` the connection itself is not encrypted: what is said is
sealed, but who is talking to whom, and how much, is visible on the wire.

## How to use

1. Install the binary (see [Install](#install)), or run it from source with
   `go run ./ --help`.

2. On the device that receives the messages, start the chat server:

   ```bash
   cthrone host
   ```

   It listens on port 8080 and prints a line when a client connects. The
   first time it runs, it also **generates a shared secret** and writes it to
   `~/.config/cthrone/token` (0600) — the path is printed. Keep it: the
   other device needs the same secret.

3. On the other device, connect to it:

   ```bash
   cthrone connect 192.168.0.42
   ```

   The IP is the address of the machine running `host`. Everything you type is
   sent; everything the other side types is printed.

   The client needs the same secret. Copy the file from the host device, or
   put the value in the environment:

   ```bash
   CTHRONE_TOKEN="$(cat ~/.config/cthrone/token)" cthrone connect 192.168.0.42
   ```

   On a network you do not trust, add `--tls` on both sides and give the
   client the fingerprint the host printed — see [Security](#security).

4. Do not know the address? Scan the local subnet for a chat server:

   ```bash
   cthrone scan
   ```

   It walks your /24 and prints the addresses that answer on the chat port.

5. Prefer a broker? Add `--nats` to either side, and run a NATS server
   locally — the repository ships one in `docker-compose.yml` for that:

   ```bash
   docker compose up -d nats
   cthrone host --nats
   ```

   The broker is published on `nats://127.0.0.1:4222` on the host, so a
   client running on the host reaches it. If the dev sandbox is already
   running, the broker is up with it — the sandbox declares the broker as
   a dependency, so `make dev-up` starts both.

   On the other side, `cthrone connect --nats` publishes to the broker.

   Note that on the NATS path the roles are inverted: `host` **subscribes** to
   the subject and `connect` **publishes** to it. The subject is the
   positional argument and defaults to `chat`:

   ```bash
   cthrone host --nats room-42
   cthrone connect --nats room-42
   ```

   The broker is the one the `--nats-url` flag names, and it defaults to
   `nats://127.0.0.1:4222` — so a remote broker is reachable:

   ```bash
   cthrone host --nats --nats-url nats://broker.internal:4222
   ```

   **The broker asks for credentials.** They come from the environment —
   `NATS_USER` and `NATS_PASSWORD` — and not from a flag, because a flag is
   visible in the process list:

   ```bash
   export NATS_USER=cthrone NATS_PASSWORD=cthrone-dev
   cthrone connect --nats
   ```

   The broker that ships with the dev sandbox uses the same names, with
   `cthrone` and `cthrone-dev` as the defaults. Override them for both
   sides when you want a different password:

   ```bash
   NATS_USER=cthrone NATS_PASSWORD=another-one docker compose up -d nats
   ```

   A broker that asks for no credentials is still reachable: without
   `NATS_USER` the client offers none.

   **The broker keeps nothing.** JetStream is off, so a conversation does
   not outlive the session it happened in, and the broker has no messages
   to clean up.

   **The broker cannot read the messages.** Every payload is sealed with the
   shared secret before it is published, so the broker carries ciphertext:
   it can tell that something was sent on a subject, not what it said. The
   `--token`, `--token-file` and `CTHRONE_TOKEN` values are what it needs to
   be private, and both devices need the same one.

   **The connection to the broker can be encrypted** the same way the
   websocket one is: give the URL the `tls://` scheme and pin the
   broker's certificate.

   ```bash
   cthrone connect --nats --nats-url tls://broker.internal:4222 \
     --cert-fingerprint 1f2e3d...
   ```

## Install

Download the archive for your platform from the
[releases page](https://github.com/fiwon123/cthrone/releases), then run the
installer it ships with — Linux, macOS and Windows carry one; the Android
archive does not. Every release also carries a `checksums.txt` — verify a
download before running it.

| Platform | Archive | Install |
|---|---|---|
| Linux (amd64) | `*_linux.tar.gz` | `tar -xzf <archive> && cd <folder> && ./add_to_path.sh` |
| macOS (Apple silicon) | `*_darwin_arm64.tar.gz` | `tar -xzf <archive> && cd <folder> && ./add_to_path.sh` |
| macOS (Intel) | `*_darwin_amd64.tar.gz` | same as above |
| Windows (amd64) | `*_windows.zip` | `Expand-Archive <archive>` then `.\add_to_path.bat` |
| Android (arm64), app | `*_android_gui_arm64.apk` | copy it to the phone and tap it, or `adb install <file>` — see [Android app](#android-app-arm64) |
| Android (arm64), CLI | `*_android.zip` / `.tar.gz` | **CLI, not an app** — runs in Termux; see [Android CLI](#android-cli-arm64-termux) |

The desktop GUI (`cthrone_gui`) ships as its own archive, so a terminal-only
user does not download a window toolkit:

| Platform | Archive | Notes |
|---|---|---|
| Linux (amd64) | `*_linux_gui_amd64.tar.gz` | needs the GTK3/GL/X11 runtime libraries, listed in the release notes |
| Windows (amd64) | `*_windows_gui_amd64.zip` | no extra runtime libraries |
| macOS (Apple silicon) | `*_darwin_gui_arm64.tar.gz` | no extra runtime libraries |

Each archive carries both `cthrone` and the short alias `ct`, so `ct host`
works the moment the folder is on your PATH.

The GUI archive adds `cthrone_gui` — the binary that opens the window — and, on
Linux, a menu entry the installer registers with a launcher icon it writes into
the XDG directories itself. No archive carries an icon file: the window icon is
embedded in `cthrone_gui`, and the launcher copy only exists once it is
installed.

### Android

Android gets two downloads from the same source tree: an **app** you tap, and
the **CLI** for a terminal app. Pick the app unless you want a terminal.

#### Android app (arm64)

`cthrone_<tag>_android_gui_arm64.apk` is the Fyne GUI compiled for a 64-bit ARM
phone or tablet — the same chat, with buttons instead of a prompt. It is
**debug-signed**, which is what a sideload accepts: there is no store listing
and no update channel, so a new release is a new APK installed over the old
one. Android asks permission to install from your browser or file manager the
first time.

```bash
adb install cthrone_<tag>_android_gui_arm64.apk
# or copy the file to the device and tap it
```

The app has three tabs:

- **Chat** — the conversation, a send field and a stop button. Tap a message to
  copy it; **Copy all** copies the whole conversation.
- **Setup** — the port, the shared secret, the transport (NATS or websocket)
  and the TLS options, with **Host** and **Connect**.
- **Devices** — **Scan network** walks the local subnet for a host on the chat
  port; tap a result to fill in its address.

A phone that hosts generates a shared secret and offers it in a dialog, copied
to the clipboard — type or paste it on the other device, or the connection is
refused. The app keeps that secret and its TLS material in its own private data
directory, separate from the CLI's `~/.config/cthrone`.

The app is an **Android** build today. The same Go code is the source for an
iOS build, which needs macOS and Xcode and has not been done yet
([issue #84](https://github.com/fiwon123/cthrone/issues/84)).

#### Android CLI (arm64, Termux)

The Android archive holds the same command-line program, compiled for Android's
CPU. Android has no terminal of its own, so it runs inside one —
[Termux](https://termux.dev) is the one it is written for.

```bash
# inside Termux
cd ~                       # not /sdcard — that mount is noexec, and running
                           # the binary from there fails with "Permission denied"
curl -LO https://github.com/fiwon123/cthrone/releases/download/<tag>/cthrone_<tag>_android.tar.gz
tar -xzf cthrone_<tag>_android.tar.gz
chmod +x cthrone ct
./cthrone connect <ip>     # or ./cthrone host — same commands as on a desktop
```

The shared secret works the same way, with one extra step: the first
`cthrone host` run creates `~/.config/cthrone/token`, and the device that
connects has to hold the same value or the connection is refused. Whichever
device hosts, copy the file across before connecting.

What the Android CLI does and does not give you:

- **arm64, `v8.0` only** — a 32-bit phone and an x86_64 emulator cannot run it.
- the archive carries **no installer script** (Linux, macOS and Windows ship
  one); to type `cthrone` from any directory, add the folder to `PATH` yourself,
  e.g. `echo 'export PATH="$HOME/cthrone:$PATH"' >> ~/.bashrc` in Termux.
- the desktop GUI ships as `cthrone_gui` in its own archive; the APK above is
  the phone equivalent, and `cthrone ui` in this archive only prints a message.
- **neither Android download is verified on a device.** Both are cross-compiled
  and published; nothing in this project's CI installs the APK or runs the
  binary on a phone or an emulator, so "built for Android" is a build claim, not
  an observed result. If it fails on your device,
  that is a bug worth
  [reporting](https://github.com/fiwon123/cthrone/issues) rather than a
  surprise.

## Commands

| Command | What it does |
|---|---|
| `cthrone host` | host the chat server and wait for a client |
| `cthrone connect <ip>` | connect to a device's chat server and chat |
| `cthrone scan` | scan the local subnet for chat servers (shows device names when discovered) |
| `cthrone ui` | same as `cthrone_gui` when built with `-tags gui`; the release CLI binaries only print a message — use the GUI archive's `cthrone_gui` |
| `cthrone recv` | opt-in remote command receiver with allowlist and audit logging |
| `cthrone exec <device> -- <cmd>` | run a command via an opt-in receiver (allowlist + logging enforced) |

| Flag | What it does |
|---|---|
| `-p`, `--port` | the port to use (default `8080`) |
| `--nats` | use the NATS transport instead of the websocket one |
| `--nats-url` | the NATS broker to connect to (default `nats://127.0.0.1:4222`); a `tls://` URL asks for an encrypted connection |
| `--token` | the shared secret — **visible in the process list**, so prefer the environment or the file |
| `--token-file` | the file the shared secret is read from (default `~/.config/cthrone/token`) |
| `--host` | the interface `cthrone host` binds to (default: every interface) |
| `--name` | device name shown to the peer in an encrypted session; the host also announces it on the network |
| `--tls` | encrypt the transport: the host serves https, the client expects it |
| `--cert-fingerprint` | the fingerprint of the certificate to pin, with `--tls` |
| `--insecure` | accept any certificate, with `--tls` |
| `-v`, `--version` | print the version |

`cthrone connect` needs the address of the machine running `cthrone host`; it
exits with an error when it is not given one. Run `cthrone <command> --help`
for the full flag list.

## Architecture

The transport is chosen at the command level, and both transports meet at the
same chat loop, so they are interchangeable from your side.

The shared secret lives in `internal/auth` — it is not part of either
transport, because the credential is a property of the tool rather than of
the wire it uses.

```mermaid
flowchart LR
  CLI["cmd/ (cobra)<br/>connect · host · scan"] --> Core["internal/core/&lt;transport&gt;/<br/>the operation"]
  Core --> Handlers["internal/handlers/&lt;transport&gt;/<br/>the session"]
  Handlers --> App["internal/data/app<br/>the port"]
```

`internal/core` decides *what* to do — connect, host, scan. The handlers
decide *how* the transport does it.

## Repository

- **Source**: https://github.com/fiwon123/cthrone
- **Releases**: https://github.com/fiwon123/cthrone/releases
- **Issues & discussions**: https://github.com/fiwon123/cthrone/issues
- **Wiki**: https://github.com/fiwon123/cthrone/wiki

## How to develop

### Prerequisites

Either of these works:

- **Host toolchain** — Go 1.26.10 or newer (the version is pinned in `go.mod`)
  and `make`. Fastest for a quick edit.
- **Dev sandbox** — Docker, and nothing else. The container pins Go 1.26.10, so
  your build is the build CI runs.

OpenCode is optional — see `AGENTS.md` for how the agent layer is configured.

### Quick start

```bash
git clone https://github.com/fiwon123/cthrone.git
cd cthrone
go run ./ --help
```

### Commands

| Command | What it does |
|---|---|
| `make check` | gofmt, vet, test, build — the local gate |
| `make release` | build every CLI archive (Windows, Linux, Android, macOS) into `build/` |
| `make release-gui` | build the GUI archives a Linux host can produce (Linux, Windows) |
| `make gui-android` | build the Android APK (needs the Android SDK, the NDK and the `fyne` CLI) |
| `make clean` | remove `build/` |
| `go test ./...` | tests only |
| `gofmt -l .` | list files that are not formatted |
| `make help` | every target, with a one-line description |

### Releasing

`.github/workflows/release.yml` builds the release and creates a **draft** for
a human to publish. It runs on manual dispatch, from `main` only:

```bash
gh workflow run release.yml --ref main -f version=v0.2.0
```

It runs `make check` first, then builds the CLI for Windows, Linux, Android and
macOS, the Fyne GUI for Linux, Windows and macOS, and the Android APK, writes
`checksums.txt`, and attaches everything to a draft release whose description
carries the install matrix and the generated changelog. **The workflow never
publishes and never pushes a tag** — publishing the draft by hand is what
creates the tag.

The GUI targets need CGO and platform headers, so they run on the GitHub
runners and not in the dev sandbox: `make release` stays buildable anywhere,
`make release-gui` needs the GTK3/X11/GL headers, `make gui-darwin` only runs
on macOS, and `make gui-android` needs the Android SDK, an NDK and the `fyne`
CLI. The APK is built **without** `fyne package --release` on purpose: that
flag emits an `.aab` for a store, while a sideload installs a debug-signed
`.apk`.

### Dev sandbox

A container with the pinned toolchain, so the build is the same on a Linux
box, a Mac, a WSL2 shell and a Windows machine. Docker is the only
prerequisite — no Make, no Python, no host setup:

```bash
docker compose up           # the sandbox, with the broker it talks to
docker compose up -d nats   # just the broker, on nats://127.0.0.1:4222
```

The sandbox declares the broker as a dependency, so every way of starting
it — `docker compose up`, `make dev-up`, `scripts/dev-up.py dev-up` —
brings the broker up with it. The second command is for running only the
broker, e.g. against a host-side client.

With `make` (Linux, macOS, WSL2):

| Command | What it does |
|---|---|
| `make dev-up` | start the sandbox in the foreground |
| `make dev-shell` | open a shell inside it |
| `make dev-opencode` | run the AI agent inside it (Linux/WSL2 only) |
| `make dev-down` | stop it |
| `make dev-build` | rebuild the image after a `Dockerfile` change |
| `make reset` | stop everything and wipe the volumes |

On native Windows, where `make` does not exist:

```powershell
python scripts\dev-up.py dev-up
```

From inside the sandbox the broker is at `nats://nats:4222` (the compose
service name); from the host it is `nats://127.0.0.1:4222`. The broker's host
ports are overridable — `NATS_PORT=4223 docker compose up -d nats` — so two
sandboxes can run at once.

The agent overlay (`docker-compose.agent.yml`) is merged automatically when the
host has a **Linux** opencode binary — the container is Linux, so a macOS or
Windows binary cannot execute inside it. The overlay mounts the gh config, the
opencode binary and `~/.gitconfig` read-only. No Docker socket is mounted: the
agent inside the sandbox cannot manage containers.

### A note on what `make check` proves

`make check` runs gofmt, vet, test, and build. Every one of the module's 21
packages now has a test file (#85/#86 closed the last gap), covering the
transports, the scanner, the CLI commands, the shared secret/TLS handling, the
remote-exec allowlist, the session hub and the mobile entry point.

A green run therefore proves that the code is formatted, that it vets clean,
that it compiles, and that every package's tests passed. It does **not** prove
the Fyne widgets, which are only type-checked under `-tags "gui ci"` in CI, and
it does not make the thin spots deep: several of the newer test files hold a
single function. `make check` prints the list of packages with no test file
after the run, so an empty list means every package was exercised.

## Troubleshooting

### `cthrone: command not found`

The installer's job is to add the extracted folder to your PATH. On Linux it
appends to `~/.bashrc`; start a new shell, or source the file by hand:

```bash
source ~/.bashrc
```

### `connect` says nothing and returns immediately

The websocket client reports what went wrong when it cannot reach the server.
Check that `cthrone host` is running on the other device, that the address is
right, and that the port matches — `cthrone scan` on the same network lists
the servers it can find.

An address that already carries a port is used as it is:
`cthrone connect 192.168.0.42:9000` talks to port 9000 and not to the
`--port` flag.

### The client says the host refused the connection

- **"it needs the shared secret"** — the two devices do not have the same
  secret. Copy `~/.config/cthrone/token` from the host, or set
  `CTHRONE_TOKEN` to the same value on both sides.
- **"e2e: unusable shared secret"** — the secret on one side is shorter than
  16 bytes, so it is not used as a key. Point both devices at the generated
  token file or give them the same long value.
- **"e2e: handshake MAC mismatch"** — the handshake reached a device with a
  different secret. The session is refused rather than falling back to
  plaintext; make the two secrets match.
- **"it is serving TLS, add --tls"** — the host was started with `--tls` and
  the client was not. Add `--tls` to the client, and give it the
  `--cert-fingerprint` the host printed.
- **"the certificate of the host does not match the fingerprint"** — the
  client pinned a different certificate. The host generates one the first
  time it runs and keeps it, so this means the client has an old
  fingerprint, or it is talking to a different host.

### `scan` finds nothing

`scan` walks your own /24 and dials the chat port on every address. It finds
nothing when the server is on a different subnet, when a firewall blocks the
port, or when the server is on the other side of a NAT.

### The broker says "Authorization Violation"

The broker asks for credentials and the client offered none, or the wrong
ones. Set them on both sides — the client reads `NATS_USER` and
`NATS_PASSWORD` from the environment:

```bash
export NATS_USER=cthrone NATS_PASSWORD=cthrone-dev
```

The dev broker uses the same names, with `cthrone` and `cthrone-dev` as
the defaults, so the two sides agree without anyone exporting anything —
as long as the password has no `#` or quote in it, which the broker's
configuration parser does not carry through.

### The NATS path cannot reach a remote broker

The broker is the one `--nats-url` names, and it defaults to
`nats://127.0.0.1:4222`. If a remote broker is not reachable, check the URL
first: the flag takes the whole address, including the scheme and the port.

### The sandbox dies with `set: pipefail: invalid option name`

The bind mount serves your working tree as-is, and a checkout that predates the
`.gitattributes` rule keeps CRLF line endings — which `git status` does not
show, because git compares normalized content. Re-check the tree out with LF
endings:

```bash
git rm --cached -r . >/dev/null && git reset --hard
```

### The sandbox refuses to start as UID 0

`make` refuses to run the container as root, because a root-owned bind mount
makes every file in the repository unwritable by your own user. Repair the
ownership of the checkout, or point the sandbox at your own identity:

```bash
HOST_UID="$(id -u)" HOST_GID="$(id -g)" make dev-up
```

### `make dev-opencode` says the agent needs a Linux binary

The container is Linux, so a macOS or Windows opencode install cannot execute
inside it. Use WSL2 — where the host binary is Linux — or run opencode on the
host against the running stack.

### The sandbox fails with `EACCES: permission denied, mkdir '/home/devuser/.cache/opencode'`, or `mkdir /go/pkg/mod/cache: permission denied`

The sandbox's caches are Docker volumes, and Docker creates a mount destination
the image does not contain **as root** — while the container runs as your UID.
The image now creates those directories itself, so a sandbox built from the
current `Dockerfile` is fine.

A sandbox created **before** that fix keeps its old, root-owned volumes:
Docker does not re-initialise a volume that already exists. Recreate them
once:

```bash
make reset        # docker compose down -v
make dev-up
```

### The broker port is already taken

Another NATS server (or another sandbox) already owns port 4222. Move this one:

```bash
NATS_PORT=4223 NATS_MONITOR_PORT=8223 docker compose up -d nats
```

## Tech stack

| Layer | Technology |
|---|---|
| Language | Go 1.26.10 |
| CLI | spf13/cobra |
| Transports | gorilla/websocket, nats-io/nats.go |
| Terminal | golang.org/x/term |

## License

MIT — see [LICENSE](LICENSE). The Apache-2.0 dependencies are attributed in
[NOTICE](NOTICE), and their licence text ships in
[LICENSE-APACHE](LICENSE-APACHE).
