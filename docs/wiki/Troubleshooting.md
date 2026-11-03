# Troubleshooting

## 🔎 Find yours

Use your browser's find: the sections below are named for the symptom.
**`command not found`** · **no device found** · **it refuses to pair** ·
**the certificate changed** · **a file will not open on the phone** ·
**no sound** · **holding backspace** · **Bluetooth** · **slow start on Windows**.

## `cthrone: command not found`

The archive ships `cthrone` and the short alias `ct`. Run `./add_to_path.sh`
(Linux/macOS) or `add_to_path.bat` (Windows) from the unpacked folder, or add
the folder to `PATH` yourself.

## `connect` says nothing and returns immediately

Give it an address: `cthrone connect 192.168.1.20`. Without one it has nothing to
dial. `cthrone scan` finds the hosts that answer on the chat port.

## The client says the host refused the connection

The message names the reason. The common ones:

- **"it needs the shared secret"** — one side has a `--token` and the other does
  not. Both ends must use the same model.
- **"e2e: peer identity refused"** — the name is pinned to a different key. If
  the device was reinstalled: `cthrone peers forget <name>`.
- **"discovery: the address is pinned to another certificate"** — the host's
  certificate changed at that address: `cthrone peers forget <address>`.
- **"the certificate of the host does not match the fingerprint"** — a
  `--cert-fingerprint` that is not the host's current one.
- **"tls: first record does not look like a TLS handshake"** — one side is
  serving plaintext. The host is TLS by default; a `--no-tls` peer needs
  `--no-tls` on both.
- **"--tls and --no-tls cannot both be given"** — TLS is the default, so `--tls`
  is only kept so a v0.6.0 command line still runs.

## `scan` finds nothing

- The devices must be on the same subnet: a broadcast does not cross a router,
  a NAT, or a network that isolates its clients.
- On a machine with a docker bridge or a VPN, the default route may be the wrong
  network: name the segment with `--subnet 192.168.1.0/24`, or use
  `--all-subnets`.
- A host on the same machine is never listed — it does not offer itself.

## The broker says "Authorization Violation"

The broker has its own credentials (`nats.conf`): export `NATS_USER` and
`NATS_PASSWORD`. A password with `#` or a quote in it does not survive the
broker's configuration parser.

## Bluetooth finds nothing

- The transport is Linux only for running code today; Windows compiles it and
  has never run it ([#224](https://github.com/fiwon123/cthrone/issues/224)).
- A host serves one device at a time; the slot frees when the keepalive notices
  the device has gone quiet (a couple of seconds).
- `cthrone scan --bluetooth` lists devices in range, not hosts on a network.

## The app opens slowly on Windows

Known, and being worked on
([#240](https://github.com/fiwon123/cthrone/issues/240)): the window is built
before it is shown, so anything slow in the build is time the user spends looking
at nothing.

## A PDF or an audio file will not open on the phone

Android does not let an app pass a private `file://` path to another app, and
this build ships no `content://` provider, so **Open** is not offered on a phone
([#305](https://github.com/fiwon123/cthrone/issues/305),
[#470](https://github.com/fiwon123/cthrone/issues/470)). Press **Save as…** and
pick a folder the file manager can see, then open the file from there.

A desktop opens these with the system's own app, as before.

## A received file is not in Downloads

By default a received file waits in the app's own storage: the row says **not
saved** and offers **Save as…**. That is deliberate — an arriving picture is not
written into Downloads unless you ask ([#469](https://github.com/fiwon123/cthrone/issues/469)).
Turn on **Save received files automatically** in the **Files** dialog if you
want arrivals to land in the chosen folder on their own; the row then says
**saved to <path>**. The Files dialog also lists the files this conversation
received, and each row copies its path.

## Play does nothing, or the row says the clip cannot be played

The in-app player handles **WAV** and **MP3** only; a row in another format
(Ogg, AAC/M4A, FLAC) has no Play button, and **Open**/**Save as…** are the way to
hear it. A clip past about three minutes is refused rather than loaded, and the
status line says so.

If a WAV or MP3 has a Play button and still makes no sound, the device could not
be opened: the app asks the operating system for one on the first **Play**, and
the status line carries the reason. On Linux that means a working PulseAudio or
ALSA setup; the app keeps working, silently, without one.

## Holding backspace on a phone deletes one character

Not this app's bug, and not fixable from here: it is in the toolkit's Android
keyboard bridge (Fyne v2.8.2), so every text field in the app behaves the same
way.

The mechanism, as far as the source shows: Fyne does not mirror the field into a
real Android input widget. It keeps a hidden one-character field — literally a
single space, rewritten whenever it empties — and turns a deletion of that space
into one `KeyBackspace` for the app
(`internal/driver/mobile/app/GoNativeActivity.java`, the `TextWatcher`;
`keyboard.go`, `keyboardDelete`). Holding the key means the keyboard must keep
deleting from a buffer the app keeps putting back, and the repeat stops.

Tracked upstream: [fyne-io/fyne#6648](https://github.com/fyne-io/fyne/issues/6648)
— see [#290](https://github.com/fiwon123/cthrone/issues/290) for the report.

What works today: pressing gives one character each time; **the × inside the
field empties it in one press** (the composer and the Setup fields all carry
one); select all and cut also works. A desktop is unaffected.

---

<sub>🧰 **Previous:** [Commands and flags](Commands-and-flags) · 🏠 [Home](Home) · **Next:** [Security model](Security-model) 🔒</sub>
