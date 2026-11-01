# Finding devices across networks

A scan is a UDP broadcast, and a router does not forward one. Two Wi-Fis on the
same modem are two segments, so neither sees the other's beacons — and a scan
that walks a `/24` only reaches the segment it walks.

This page says what actually crosses, and what to do.

## What each approach can reach

| Approach | Crosses a segment | Needs | Where it fails |
|---|---|---|---|
| **broadcast beacon** (the default scan) | no | nothing | anything past the router |
| **unicast beacon to a known host** (recommended) | yes | one address dialed once | a router that blocks traffic between its own segments |
| **`--subnet <cidr>`** | yes, if routed | the CIDR of the other segment | a network that hides its shape, or client isolation |
| **mDNS / DNS-SD** | only if the router reflects it | nothing — but a dependency | consumer routers almost never forward multicast |
| **NATS** | yes | a broker both can reach | no broker, or one you do not control |

## The recommended way

1. **Connect once by address.** `cthrone connect 192.168.0.20` — the address of
   the device on the other Wi-Fi, typed or found on its *Setup* tab.
2. **That is the whole setup.** The host whose certificate this device pinned is
   remembered, and from then on the host also sends its beacon to that address
   **by unicast** — which routes between segments wherever ordinary IP routing
   does, unlike a broadcast.
3. The device appears in **Devices** with its name, like a device on your own
   segment, and connects without the address being typed again.

The list it uses is the certificate pin file the app already keeps: no new
configuration, and no new port — the beacon port was already there.

## When it still does not work

Some networks are built to stop exactly this:

- **client isolation** (guest Wi-Fi) — the access point drops traffic between
  clients, so nothing crosses, unicast included;
- **separate VLANs with no inter-VLAN routing** — the two segments are separate
  networks, and only something outside them (a broker) can join them;
- **a firewall on the host** — the chat port is closed to the other segment.

In those cases the answer is [NATS](NATS): both devices talk to a broker they can
reach, and the broker carries ciphertext it cannot read.

## What was rejected

**mDNS** is how printers announce themselves, and it was the first idea. It is
still link-local multicast: it crosses a segment only when the router has an
mDNS reflector, which consumer equipment almost never does. It would have added
a dependency and a second discovery mechanism to reach exactly the networks the
unicast beacon already reaches.
