# Commands and flags

## Commands

| Command | What it does |
|---|---|
| `cthrone host` | host the room: every device that joins sees the messages of the others |
| `cthrone connect <ip>` | connect to a device's chat server and chat; with `--bluetooth`, the argument is a device name or address |
| `cthrone scan` | scan the default route's `/24` for chat servers; `--subnet <cidr>` adds another segment, `--all-subnets` walks every local interface (shows device names when discovered); `--bluetooth` lists the devices in radio range instead |
| `cthrone peers` | list the devices and host certificates this one has pinned, or `cthrone peers forget <name\|fingerprint\|address>` one |
| `cthrone ui` | same as `cthrone_gui` when built with `-tags gui`; the release CLI binaries only print a message — use the GUI archive's `cthrone_gui` |
| `cthrone recv` | opt-in remote command receiver with allowlist and audit logging |
| `cthrone exec <device> -- <cmd>` | run a command via an opt-in receiver (allowlist + logging enforced) |
| `cthrone update --check` | ask the public release list whether a newer version exists; names the archive for this platform and exits 0 either way — non-zero only when the check could not be made |
| `cthrone update --install [--to <tag>]` | replace this binary with the one from the newest published release, or the one named by `--to`; the download is verified against the release's `checksums.txt` first, and the binary it replaces is kept as `<name>.previous` |
| `cthrone update --revert` | put the previous binary back, without a network call |

## Flags

| Flag | What it does |
|---|---|
| `-p`, `--port` | the port to use (default `8080`) |
| `--subnet` | `cthrone scan`: an IPv4 CIDR to walk as well, e.g. `192.168.1.0/24`; repeat for several segments |
| `--all-subnets` | `cthrone scan`: walk the `/24` of every local network interface |
| `--nats` | use the NATS transport instead of the websocket one |
| `--nats-url` | the NATS broker to connect to (default `nats://127.0.0.1:4222`); a `tls://` URL asks for an encrypted connection |
| `--bluetooth` | use the Bluetooth transport instead of the websocket one (Linux and Windows; `host` serves one device at a time) |
| `--token` | a shared secret to use instead of pairing by device key — **visible in the process list**, so prefer the environment or the file |
| `--token-file` | the file the shared secret is read from — no file is read unless it is named here |
| `--host` | the interface `cthrone host` binds to (default: every interface) |
| `--name` | device name shown to the peer in an encrypted session; the host also announces it on the network |
| `--no-tls` | serve and dial plaintext `ws://` instead of the default `wss://`; the payload stays sealed end to end |
| `--tls` | encrypt the transport — **the default**; kept so a v0.6.0 command line still runs |
| `--cert-fingerprint` | pin the certificate by hand instead of on first contact; a `tls://` broker needs one (or `--insecure`) |
| `--insecure` | accept any certificate; the traffic is still encrypted, but the host is not checked |
| `-v`, `--version` | print the version |

## Environment

| Variable | What it does |
|---|---|
| `CTHRONE_TOKEN` | the shared secret, for the whole session; nothing is read from a file unless `--token-file` names one |
| `NATS_USER`, `NATS_PASSWORD` | the broker's credentials, not cthrone's |

`--token` on the command line is visible in the process list; the environment or
a file is the safer place for it.
