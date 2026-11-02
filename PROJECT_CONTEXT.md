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
- **Audio**: `github.com/ebitengine/oto/v3` for the sound device (pure Go on
  Linux, Windows and macOS, cgo against the NDK's oboe on Android) and
  `github.com/hajimehoshi/go-mp3` for MP3; WAV is decoded by the standard
  library
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

- **Done**: the websocket transport (host, connect, scan), the NATS transport, the terminal chat loop, the `ct` short alias, the version flag, device identity/discovery, a session hub, an allowlist and audit log for remote execution whose transport is not written yet (#275), end-to-end encryption of every session (`internal/e2e`), transport encryption by default with the host certificate pinned on first contact, CLI release archives for Windows, Linux, Android and macOS, a Fyne GUI archive for the three desktop platforms, a debug-signed Android APK built from the same GUI, a draft-release workflow that builds and attaches them all, pairing by device key with a six-digit first-contact code and a pin on first contact (`cthrone peers`), buttons and tab labels that copy on a long press, a send order that is serialized end to end and a websocket keepalive that notices a silent link instead of waiting for the next write (#192, #193), and the device name shown beside its address in the Devices list, in chat and in `cthrone scan` (#194), a host that is a room rather than a pair: every device that joins sees every other device's messages and files, relayed with the sender named (#199), `cthrone scan --subnet`/`--all-subnets`, which walks a segment the default route does not cover when the router routes between them (#200), and a Bluetooth transport (`--bluetooth`) that carries the same sealed session over a BLE link, one device at a time, with framing, reassembly and a keepalive tested without a radio (#202). The Linux GATT layer speaks BlueZ and the Windows one WinRT, which has never been run (#224); the Android binding is separate work (#206), and a phone build refuses the transport and leaves the option out of the GUI. `v0.11.0` brought a messenger that shows what it
sends: a sent file draws the same preview as a received one, a text file is drawn
in its row, a PDF or an audio clip opens in the device's own app, and a transfer
in flight can be cancelled from either side — the peer is told, so it drops its
half file at once instead of waiting for a timeout (#237, #238). Discovery now
reaches a second segment: a host announces itself by unicast to every address
this device has pinned, which is what a broadcast cannot do (#239).

It also carries the hardening that came out of asking what a peer can make this
program do: a command's output is bounded and its whole process tree dies at the
deadline (#269), a received file's name can no longer escape the download folder
(#271), a beacon flood can no longer grow the device list without bound (#273),
and `recv`/`exec` — which were stubs, one of them running commands **locally**
while its help said remote — now refuse and say so (#275). The suite runs under
the race detector and a fuzzer in CI (#262, #264).

`v0.16.0` is a presentation release. The **wiki** is rebuilt to be read: a
landing page with badges and a diagram of terminal, window and phone into one
sealed session and one room; a quick start on every page; a capability matrix
per transport and per file kind; a proven/unproven table with the issue behind
every ❌; diagrams for the host flow and the handshake; and Prev · Home · Next
footers so it reads in order (#450). The **chat header shows its connection
state as one colour glyph** — ⚪ offline, 🟡 starting, 🟢 hosting, 🔵 connected,
🟠 reconnecting, 🔴 lost — so the line beside it is free for what happened, and
the state is visible without opening Setup (#453). No transport, no wire format
and no security rule changed.

`v0.15.0` is the second report from a user, and the fixes it asked for. A
message a host relays is **attributed to the device that typed it**: the sender
travels in the frame header now, and a peer that may predate the field is given
the prefix it has always read (#431). The chat line carries **only what
happened** — no connection state standing in it, no "Discovered" chatter — and
hides when it has nothing to say (#433). The transcript **follows a new message
only when you were at the bottom**, and offers a button back down when you were
not (#434). The **waveform is prepared when the row is painted**, so a point can
be chosen before Play, and the clip is decoded once for both (#435). The wiki is
rebuilt around a quick start per page and signposts rather than paragraphs
(#436), and the roadmap is a GitHub project with one item per issue (#437).
With `CTHRONE_PERF` set, the window reports its message path's cost, its
goroutines and its heap to the failure log, because the lag report could not be
reproduced on a machine with no renderer (#432 — still open until the numbers
say what the cost is). Android hosting in the background (#438) and the radio on
a phone (#206) need a service, an SDK and a device; neither is in this
environment.

`v0.14.1` puts the package count right. `gui/` — the window binary's own entry
point, added in v0.12.2 — has its test, and the coverage claim in the working
instructions still said 32 packages and did not list it. It says 33 and names
the package (#426). No code changed.

`v0.14.0` plays an animated picture. A GIF in the conversation moves: Play and
Stop, a clock, and a seek bar that jumps through it, decoded with the standard
library on every platform — the first half of the video request, and the player
an MP4 decoder would feed (#421). Frames are composited the way the sender's
viewer drew them, a still GIF keeps its picture, and nothing beyond 16 MiB, 1200
frames or 64 MiB of decoded pixels is decoded at all. What a phone records is
still H.264 in MP4, and that needs a codec this build does not carry (#389). The
gate grew too: `make fuzz` now drives **every parser a peer can reach** — the
discovery beacon anyone on the segment can send (#414), the handshake a peer
sends before proving anything (#416), and the Bluetooth reassembler's packet
stream (#418) — beside the envelope and the chunk arithmetic it already covered.

`v0.13.2` keeps the playhead. **Stop** no longer throws away where the sound
had got to: the row shows the point, and **Play resumes** from there, while a
clip that played to its end starts over and a tap on the waveform still wins
(#409). The release page also says what was missing from it: **video does not
play in the app**, it opens with the device's player, and the plan for changing
that is on the wiki (#411).

`v0.13.1` is a documentation patch: the video decision is now a plan. The wiki
page **Video in the app** records why a pure-Go decoder is not an option, what
cgo FFmpeg would cost on each platform (including the mingw cross-build and the
NDK one), the licence rule that keeps this project MIT, where a decoder plugs in
beside the audio player, and the tests that would prove it — split into what a
fake decoder can prove and what needs a real one and a screen. It says plainly
that nothing is implemented. No code changed (#404, #389).

`v0.13.0` adds the shape of the sound. An audio row now carries a **waveform**
under its Play button — one peak per column, from the decode the player already
did — and the waveform is the **seek control**: tap or drag it, and the clip
continues from there, or starts there if it was idle. The clock follows, because
the device is told to re-anchor it, and the peaks are cached per path so a
repaint draws from memory rather than decoding again (#399). No dependency was
added. **Video is still not drawn**: the report asked for it with a seek bar,
and that needs a codec this build does not carry — the decision and its cost are
in #389, which stays open. The four hardware claims are unchanged: the Windows
radio (#224), the Android app and its radio (#155, #216, #206).

`v0.12.2` is a patch written from a report against Windows 11 and a phone. The
window binary is its own entry point now, so a double-click opens it — the
command tree used to sit between the click and the window (#382). The chat
header is one row: the status, **Stop** and **Files**, with the connection state
as the line's fallback rather than a second line above it (#383). A host's
**Stop disconnects every client**: a websocket is a hijacked connection, and the
HTTP server does not close those, so the room is closed explicitly before the
listener goes (#385). A tap outside a picture dismisses the viewer (#387). The
file picker opens in the last folder used, else Downloads, else home, instead of
the toolkit's list of places — the likely reason Attach took seconds on Windows
(#386). And the About tab gains a **Restart** button after an install, and
**Install previous release** for a build that arrived by unpacking the archive,
where Revert had nothing to revert to (#384). No transport, no wire format and
no security rule changed. What is not proven: the double-click and the picker's
timing need a Windows machine, and neither was measured on one.

`v0.12.1` is a documentation patch on that: **how to prove what v0.12.0 could
not**. A new page, *Verifying on real hardware*, turns each unproven claim into
steps — the Windows radio (#224), the Android app and its radio (#155, #216,
#206), a rendered window, and the in-app player — with the exact commands, what
to record, and the issue the result belongs on. The Bluetooth page now carries
the **Android scope decision** #216 asked for: the central role first, because a
phone that joins a host is the case the transport exists for, and the platform
fact therefore becomes a per-role capability rather than one boolean. No
behaviour changes, and no test is claimed: this release is instructions.

`v0.12.0` adds the two things a chat window was still missing. A **dropped
client asks for its link again**: a GUI connection that closes without the user
pressing Stop retries with a backoff from one second to a minute, says
`reconnecting…` in the header, stops when the user does, and never retries a
refusal — a peer that answered no is an answer, not a drop (#326). And a
**sound plays where it was sent**: a received or sent WAV or MP3 clip plays on
its own row, with Play and Stop on one button, a position clock, one clip at a
time, the decode done beside the UI and refused past 32 MiB, and the sound
device opened only on the first Play (#306). Other formats keep the row's
Open/Save actions, and the player is in every GUI build — including the APK,
where nothing has heard it yet (#155). No transport and no security rule
changes in this release; the Bluetooth radio on Windows, and the phone build
and the APK on a device, are still unproven (#224, #216, #206, #155).

`v0.11.7` is a patch on that, and the rest of the same report. Enter is
deterministic on a phone: it always breaks the line, and the Send button or the
keyboard's action key sends — the append rule #323 inferred could send or break the
same key depending on a flag nothing on a phone sets (#362). A desktop keeps Enter
sends and Shift+Enter newline. The About version is part of the name's own line
(`cthrone  v<tag>`), the Chat tab's status line exists only while it has something
to say, and the connection mark is a character in the same text as its word, so
they share a baseline (#364). A transfer failure writes nothing about the file to
the log — a name and a path are the user's — and a phone's update check names the
release on the button that opens the release page (#366).

`v0.11.6` is a patch on that, and the rest of the same report. A row draws the
sender on a line of its own, bold, in a colour the name decides — the same device
in the same colour everywhere (#346). Enter sends and Shift+Enter breaks the line,
the way a chat app a desktop user knows behaves; a phone's enter key sends too
(#323). With the keyboard up, a tap on the transcript puts it away instead of
copying a message (#325). A mobile build keeps the stacked layout whatever the
width, so a rotation cannot bring the two-column desktop layout onto a handset —
the manifest still asks for portrait, and the request is what one device ignores
(#302). And a transfer that fails now writes the reason to the log as well as the
status line (#327).

It does not claim the backspace repeat (#290), the sensor lock (#302), the
phone-to-phone transfer report (#327), or audio in the app (#306).

`v0.11.5` is a patch on that. The receiver now belongs to the room rather than to
one device in it: when the newest device left a host, the assembler closed with it
and every later file from the others was refused as "a file arrived while no
session was receiving" (#342). The Android app reports the tag it was built from —
the build writes it in, where before the APK fell back to packager metadata — and
the version sits beside the name instead of at the screen's edge (#343). The
composer has no placeholder (#344), and the chat header says, with a mark and a
word, whether this window is offline, connecting, hosting, or connected to a named
device — never the app's own name (#345).

It does not claim colours per sender (#346) or Enter-to-send (#323), and the rest
of that report is still open: backspace repeat (#290), the portrait lock on a phone
that still rotates (#302), the two phone-only reports (#325, #327), and audio in
the app (#306).

`v0.11.4` is a patch on that. A host's own message and file now reach the whole
room instead of the last device that joined — a host relays its clients but spoke
through the one session it happened to hold (#320). The transcript stops decoding
the same picture and re-reading the same text file on every repaint, which is what
made the app slower the longer a conversation ran (#321). The update check reads
the release page's redirect instead of the rate-limited API, names a 403 as a rate
limit, and reuses an answer for ten minutes (#322). The composer starts at one
line, grows to five and then scrolls, with a paperclip and a primary Send
(#324); the About tab carries the logo beside the name with the version at the
right (#329); and the release page is a product page with an icon per platform,
the changelog folded away (#328).

It does not claim **Enter sends** (#323: still Shift+Enter, as Fyne's multiline
entry defines it), **reconnect after a drop** (#326), the two phone-only reports
(#325: Send copying a row with the keyboard up, and #327: a PDF or audio between
two phones), or audio in the app (#306).

`v0.11.3` is a patch on that, written from a second field report on a phone. A
picture sent from a phone now shows itself: the row keeps a thumbnail, because a
picker hands over a `content://` URI with no path to read later, and the type is
read from the bytes because the picker names the file with a document id and no
extension (#303). A file row says what kind of file it is — audio, video, image
or text, the generic document for a PDF — instead of being a filename alone
(#304). A phone no longer offers **Open**, which could never work there, and the
row says how to get the file out instead (#305). Every field carries a **×** that
empties it in one press, because holding backspace does not repeat (#290). And
the app stays upright: `make gui-android` renders a manifest that locks the
activity to portrait, with the release's version substituted (#302).

It does not claim **audio playing inside the app** (#306) and does not claim the
backspace repeat is fixed — that is Fyne's Android bridge, still tracked upstream
— and nothing in it was run on a phone (#155).

`v0.11.2` is a patch on that, written from the first field report on a phone. A
received file is no longer called failed when it arrived intact and only the save
could not happen — the row keeps the staged file and offers *Save as…* — and a
folder is accepted only after the app has written a byte into it, because a
phone's picker returns a path that is absolute and is not a directory (#286). A
phone is no longer offered an update it cannot install: the package, not a file,
is replaced by the release page (#285). A device row is a button, because that is
what it does (#287); Stop moved out of the composer row, away from Send (#288);
Copy all is gone, since a tap on a row is the precise gesture (#289); and Setup
asks for the device, the address and the carrier before the security switches
(#291).

It does not claim a fix for holding backspace on a phone keyboard: that repeat
stops in Fyne's Android bridge, every field in every app is affected, and the
finding is recorded and tracked upstream (#290). Nothing in this patch was run on
a phone (#155).

`v0.11.1` was the patch before it: a row that takes a tap is the object the driver
finds on a desktop too, so the Devices list and the transcript respond to a click
again — the selectable label's own selection child had been swallowing it (#280).

`v0.10.1` was the release before it: the README as a landing page with a wiki
behind it (#242, #241), CONTRIBUTING (#244), one licence — MIT (#243), the window
shown before the slow work (#240), and the first two slices of the file work.
`v0.10.0` carried the multi-line composer (#211), the transport select reading
one platform fact (#218), the update check with install and revert (#212, #214),
long-press selection (#213, #231) and the Bluetooth transport building for
Windows (#215).

What this release does not claim: **audio does not play inside the app** (#306)
and PDFs are not drawn — both open with another app, because this toolkit ships
no audio player or decoder and there is no pure-Go PDF renderer; **`recv`/`exec`
are not implemented** (#275); **backspace does not repeat on a phone** (#290);
and the **Windows radio**, the **phone build** and the **APK** are still unrun
(#224, #206, #155).

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
- **Roadmap**: every open issue is one item on the
  [project board](https://github.com/users/fiwon123/projects/7), with its
  milestone and its status (Todo, In Progress, Done). The issues are the source
  of truth — the board is a view of them, kept by hand as work moves — and the
  board's own description says so.
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
