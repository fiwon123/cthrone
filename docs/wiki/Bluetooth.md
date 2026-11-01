# Bluetooth

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
