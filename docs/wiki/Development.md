# Development

## Prerequisites

Either of these works:

- **Host toolchain** — Go 1.26.10 or newer (pinned in `go.mod`) and `make`.
  Fastest for a quick edit.
- **Dev sandbox** — Docker, and nothing else. The container pins Go 1.26.10, so
  your build is the build CI runs.

```bash
make dev-up        # start the sandbox (foreground; Ctrl+C stops it)
make dev-shell     # a shell inside it
make dev-opencode  # run the AI agent inside it (Linux/WSL2 only)
make dev-down      # stop it
make help          # every target, with a one-line description
```

Without `make`, on any OS with Docker: `docker compose up`, or
`docker compose up -d nats` for just the broker on `nats://127.0.0.1:4222`.
On native Windows, where `make` does not exist: `python scripts/dev-up.py dev-up`.

## The gate

```bash
make check     # gofmt, vet, go test ./..., the headless GUI suite, cross-builds
```

It fails when the toolchain is missing — a gate that cannot run reports nothing,
and reporting nothing must not look like success. `go test ./...` printing
`[no test files]` for a package is not a pass for that package.

The GUI tests run headless (`-tags "gui ci"`, Fyne's software driver), so no
GTK/X11/GL is needed; a native rendered frame still needs a machine with the
toolkit and a human look.

## Android and Windows

- The **APK** needs the Android SDK, an NDK and the `fyne` CLI:
  `make gui-android`. CI builds it; nothing in CI installs it.
- The **Windows GUI** needs mingw-w64: `make gui-windows` cross-links it.
- The **Bluetooth transport** builds for Windows and has never run there
  ([#224](https://github.com/fiwon123/cthrone/issues/224)).

## The rules

`AGENTS.md` in the repository root holds them: branching, commit types, the
gate, the release steps, and the environment rules. It is written for the AI
agent that does much of the work, but it is the bar every patch is held to.

## Releases

`.github/workflows/release.yml` builds every archive and opens a **draft**
release for a human to publish:

```bash
gh workflow run release.yml --ref main -f version=v0.10.1
```

It runs `make check` first. Publishing the draft by hand creates the tag; the
workflow never pushes one.
