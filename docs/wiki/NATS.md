# NATS

Both devices talk to a broker, so they do not need to see each other directly.
This is the transport that crosses subnets when a broker is reachable.

```bash
cthrone host --nats --nats-url nats://broker.internal:4222
cthrone connect --nats --nats-url nats://broker.internal:4222
```

## What the broker can and cannot see

- **It carries ciphertext.** Every payload is sealed end to end before it is
  published, so the broker reads the subject and the timing, nothing more.
- **It needs credentials** of its own: the broker's, from the environment
  (`NATS_USER`, `NATS_PASSWORD`), not cthrone's.
- **A `tls://` URL** asks for an encrypted connection to the broker, with the
  same pinning rules as a websocket host.

## The local broker

`docker-compose.yml` runs one for experiments:

```bash
docker compose up -d nats     # nats://127.0.0.1:4222
```

It requires the credentials in `nats.conf` and runs **without** JetStream, so a
conversation does not outlive the session it happened in.

## What is different from a websocket

There is no host to ask on a first contact: a subject has no single person
behind it. A first contact on NATS is therefore pinned without a prompt, and the
handshake version still gates the two credential models.
