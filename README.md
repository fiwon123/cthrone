<div align="center">

<img src="assets/logo-512.png" width="96" alt="cthrone">

# cthrone

**Chat that needs no account, no server and no cloud.**
Two devices, one command, encrypted end to end — from a terminal or from the app.

[![Latest release](https://img.shields.io/github/v/release/fiwon123/cthrone?color=4c8bf5&label=release)](https://github.com/fiwon123/cthrone/releases)
[![License: MIT](https://img.shields.io/badge/license-MIT-green)](LICENSE)
[![Platforms](https://img.shields.io/badge/platforms-Linux%20%7C%20Windows%20%7C%20macOS%20%7C%20Android-8250df)](https://github.com/fiwon123/cthrone/releases)
[![CI](https://github.com/fiwon123/cthrone/actions/workflows/ci.go.yml/badge.svg)](https://github.com/fiwon123/cthrone/actions/workflows/ci.go.yml)

</div>

---

## ✨ What it is

Two devices that need to exchange a message — a phone and a laptop, a sensor and
a workstation — normally need an account, a hosted server, or a GUI. cthrone
needs none of those.

- **Nothing in the middle.** One binary is the server and the client. No sign-up,
  no cloud, no relay you did not choose.
- **Encrypted and authenticated by default.** Every session is sealed end to end
  and a peer that cannot prove its key is refused. There is no plaintext mode.
- **A room, not a pair.** Every device that joins a host sees the messages of the
  others, with the sender named.
- **Terminal and app, one codebase.** A CLI everywhere, and a Fyne GUI for Linux,
  Windows, macOS and Android.

## 🚀 Quick start

```bash
# on the device that hosts
cthrone host

# on the other device — the address the host prints
cthrone connect 192.168.1.20
```

Both screens show a **six-digit code** on first contact. Match it, accept, and
the device is pinned by its own key. That is the whole pairing.

Scanning instead of typing? `cthrone scan` finds the hosts on your network.

Every flag is in `cthrone --help` — the binary carries its own documentation, so
an archive with no wiki at hand still answers — and in
[Commands and flags](https://github.com/fiwon123/cthrone/wiki/Commands-and-flags).

## 🧭 Transports

| | What it needs | Where it works |
|---|---|---|
| 🌐 **websocket** *(default)* | nothing but the two binaries | the same network |
| 📡 **NATS** | a broker both can reach | across networks, if a broker exists |
| 🔵 **Bluetooth** | a radio, no network at all | Linux; Windows compiles it, never run ([#224](https://github.com/fiwon123/cthrone/issues/224)) |

Details: [Host and connect](https://github.com/fiwon123/cthrone/wiki/Host-and-connect) ·
[NATS](https://github.com/fiwon123/cthrone/wiki/NATS) ·
[Bluetooth](https://github.com/fiwon123/cthrone/wiki/Bluetooth)

## 📦 Download

| Platform | CLI | App |
|---|---|---|
| Linux amd64 | `cthrone_<tag>_linux.tar.gz` | `…_linux_gui_amd64.tar.gz` |
| Windows amd64 | `cthrone_<tag>_windows.zip` | `…_windows_gui_amd64.zip` |
| macOS | `…_darwin_arm64.tar.gz`, `…_darwin_amd64.tar.gz` | `…_darwin_gui_arm64.tar.gz` |
| Android arm64 | `…_android.zip` *(Termux)* | `…_android_gui_arm64.apk` |

Every archive carries `cthrone` and the short alias `ct`; run
`./add_to_path.sh` (`add_to_path.bat` on Windows) from the unpacked folder.

> The **APK is debug-signed**: tap to install, no store and no account. It has
> never been installed on a device by this project — see
> [#155](https://github.com/fiwon123/cthrone/issues/155).

Verify a download against `checksums.txt` from the release page — or let the
tool do it: `cthrone update --check`, then `--install` (verify, atomic swap,
`--revert` to go back).

## 🔒 Security in four lines

1. **One credential** authenticates a session: this device's ed25519 key, pinned
   by the peer on first contact — or a shared secret you name (`--token`,
   `CTHRONE_TOKEN`, `--token-file`), the v0.6.0 path.
2. **X25519 + HKDF + AES-256-GCM** on every payload, on every transport, with a
   sequence number that fails a replayed frame.
3. **First contact is the only moment to insert a machine**, which is why both
   screens show the same six-digit code and both fingerprints.
4. The websocket is **`wss://` by default**; the host's certificate is pinned on
   first contact. What stays visible is the shape — addresses, subjects, timing.

Full model, with the issues that decided it:
[Security model](https://github.com/fiwon123/cthrone/wiki/Security-model).

## 📚 Documentation

| I want to… | Read |
|---|---|
| host, connect, and find a device | [Host and connect](https://github.com/fiwon123/cthrone/wiki/Host-and-connect) |
| understand the six-digit code | [Pairing and peers](https://github.com/fiwon123/cthrone/wiki/Pairing-and-peers) |
| send a file | [Send a file](https://github.com/fiwon123/cthrone/wiki/Send-a-file) |
| run it without any network | [Bluetooth](https://github.com/fiwon123/cthrone/wiki/Bluetooth) |
| cross two networks | [NATS](https://github.com/fiwon123/cthrone/wiki/NATS) |
| look up a flag | [Commands and flags](https://github.com/fiwon123/cthrone/wiki/Commands-and-flags) |
| fix something | [Troubleshooting](https://github.com/fiwon123/cthrone/wiki/Troubleshooting) |
| build or release it | [Development](https://github.com/fiwon123/cthrone/wiki/Development) |

## ⚠️ What is not proven

This project says what it verified.

- **The Windows Bluetooth transport compiles and has never run.**
- **The Android app has never been installed**, and its radio is not written.
- The GUI is tested **headless**; no native rendered frame has been checked.

Running the app and reporting what happened is the most useful contribution
right now — [CONTRIBUTING](CONTRIBUTING.md) says how.

## 🤝 Contributing

Issues, ideas, questions and — most of all — running the unproven parts on real
hardware: see [CONTRIBUTING.md](CONTRIBUTING.md). The project is developed with
an AI agent, and every pull request says what was verified and what was not.

## 📄 License

MIT — see [LICENSE](LICENSE). Copyright © 2025 Felipe Inoue.
