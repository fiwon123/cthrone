# Troubleshooting

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
