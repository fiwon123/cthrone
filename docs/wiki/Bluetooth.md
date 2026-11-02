# Bluetooth

## ⚡ In a hurry

```bash
cthrone host --bluetooth              # advertise the service
cthrone scan --bluetooth              # what advertises it nearby
cthrone connect --bluetooth laptop    # dial by name or address
```

No address, no port, no network. One device at a time on a host.

The transport for when the network itself is the problem: no Wi-Fi, no router,
no broker.

```bash
cthrone host --bluetooth                 # advertise the service
cthrone connect --bluetooth laptop       # scan and dial by name or address
cthrone scan --bluetooth                 # list what advertises the service
```

## What the radio imposes

- **One device at a time on a host.** BlueZ does not say which central wrote to a
  characteristic, so two devices cannot be told apart and there is no per-device
  channel to answer on. The slot frees when the keepalive notices the device has
  gone quiet.
- **Nothing is announced.** Discovery is a BLE scan, not a beacon, so the scan
  lists devices in range rather than hosts on a network.
- **Text, not files, in practice.** A packet carries 20 bytes.
- **An address is an address however it is spelled**: `AA:BB:CC:DD:EE:FF`,
  `AA-BB-CC-DD-EE-FF` and `aabbccddeeff` all name the same device.

## Platforms

| Platform | State |
|---|---|
| Linux | works — BlueZ over D-Bus |
| Windows | **compiles, never run** — WinRT backend ([#224](https://github.com/fiwon123/cthrone/issues/224)) |
| Android | not bound yet ([#206](https://github.com/fiwon123/cthrone/issues/206)) |
| macOS | not supported by the library this uses |

The security model does not change on a radio: the same device-key handshake, the
same first-contact code, the same pin in `peers.json`.

## Android: the scope decision

`tinygo.org/x/bluetooth` has no Android backend — its stacks are BlueZ,
CoreBluetooth, WinRT and embedded chips — so the binding has to be written
against Android's Java Bluetooth APIs, here. Decided before the code, as
[#216](https://github.com/fiwon123/cthrone/issues/216) asks:

**Central first.** The case the transport exists for on a phone is a phone with
no network joining a machine that hosts — that is the central role: scan,
connect, discover characteristics, subscribe, write. It is the half `Dial`
already needs, and it stays away from the GATT server and advertiser surface
(`BluetoothLeAdvertiser`, several centrals, lifecycle) that a host adds. A host
on a phone is a one-device room on a small screen, which is worth less than the
phone that can join someone else's.

The host role is a follow-up, not a different design: the same `Listener` seam,
over the same framing and keepalive. Nothing in the wire protocol, the security
model or the keepalive changes.

One consequence. `bluetoothcore.Supported()` answers a single boolean today, so
the window either offers the transport or does not. A central-only phone build
must **connect but not host**, so the platform fact becomes a per-role
capability — can connect, can host — rather than one flag. #206 is where that
shape lands.

---

<sub>🎞️ **Previous:** [Video in the app](Video-in-the-app) · 🏠 [Home](Home) · **Next:** [NATS](NATS) 🛰️</sub>
