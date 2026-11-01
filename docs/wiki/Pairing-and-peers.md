# Pairing and peers

Every session is authenticated by exactly one credential: **this device's
ed25519 key**, unless you name a shared secret.

## The device key

- Generated on first use, in the platform's user configuration directory:
  `~/.config/cthrone/identity.key` on Linux (mode `0600`),
  `~/Library/Application Support/cthrone` on macOS, `%AppData%\cthrone` on
  Windows, and the app's private storage on Android.
- The peer **pins** it under your device name on first contact — but only after
  the handshake has proved you hold the key. A hello alone records nothing.
- A different key under a pinned name is **refused**, never re-pinned.

## What `cthrone peers` shows

```bash
cthrone peers                      # what this device trusts, and when it was pinned
cthrone peers forget bob           # by name
cthrone peers forget 192.168.1.20  # a host whose certificate changed
```

The list also carries the **host certificates** pinned on first contact, keyed
by the dialed `host:port`. A different certificate at a pinned address is
refused; forgetting the address is the way back.

## When to forget

- a device reinstalled the app and generated a new key;
- a host's certificate changed (a new install, or a different machine at the same
  address);
- you paired with the wrong device.

Forgetting is the only way back: the pin is a promise, and the program will not
break it silently.

## The shared secret

If you name a secret — `--token`, `CTHRONE_TOKEN`, or `--token-file` — that is
the credential instead, which is the v0.6.0 compatibility path. The two models
cannot be mixed: a peer speaking the other one is refused, never downgraded.

A `token` file left by v0.6.0 in the configuration directory is **not** read on
its own. The app tells you it is there and offers to delete it.
