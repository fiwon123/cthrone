# cthrone

Two devices that need to exchange a message — a phone and a laptop, a sensor and
a workstation — normally need an account, a hosted server, or a GUI. cthrone
needs none of those: start it on one device, connect from the other, and type.

- **Nothing to install in the middle.** One binary is the server and the client.
- **Encrypted and authenticated by default.** No anonymous mode, no plaintext.
- **A room, not a pair.** Every device that joins a host sees the others, with
  the sender named.
- **Terminal and app.** The same code builds a CLI and a Fyne GUI for the
  desktop and for Android.

## The three transports

| Transport | What it needs | Where it works |
|---|---|---|
| **websocket** (default) | nothing but the two binaries | the same network |
| **NATS** | a broker both can reach | across networks, if a broker exists |
| **Bluetooth** | a radio, no network at all | Linux, and Windows (compiled, never run — [#224](https://github.com/fiwon123/cthrone/issues/224)) |

## Start here

- [Host and connect](Host-and-connect) — the first two commands.
- [Pairing and peers](Pairing-and-peers) — the six-digit code, and how to undo a
  pairing.
- [Send a file](Send-a-file) — what the other side receives.
- [Troubleshooting](Troubleshooting) — the failures people actually hit.

## What is not proven

This project says what it verified. Three claims currently rest on hardware
nobody has run:

- the **Windows** Bluetooth transport ([#224](https://github.com/fiwon123/cthrone/issues/224));
- the **Android app** on a device, and its radio ([#155](https://github.com/fiwon123/cthrone/issues/155), [#206](https://github.com/fiwon123/cthrone/issues/206));
- a **rendered GUI frame** under the native driver.

If you can run any of those, [CONTRIBUTING](https://github.com/fiwon123/cthrone/blob/dev/CONTRIBUTING.md)
explains what would help most.
