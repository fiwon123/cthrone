# AGENTS.md

> Working instructions for agents in this repository. `PROJECT_CONTEXT.md` holds
> the structured project identity behind this document; keep the two in step.

### Product

## Project Objective

cthrone — a chat tool that carries messages from one device to another, over a
websocket or a NATS server, from a terminal or from a touch-friendly app on a
phone.

### Problem

Two devices that need to exchange a message — a phone and a laptop, a sensor
and a workstation — normally need an account, a server, or a GUI. For a short
internal exchange, that is more setup than the message is worth.

### Goal

A user can start a chat server on one device, connect from another with one
command, and exchange messages in the terminal — over the local network or
through a NATS broker.

### Main Technology

- **Language**: Go 1.26.10 (pinned in `go.mod`)
- **CLI framework**: spf13/cobra
- **Transports**: gorilla/websocket (default), nats-io/nats.go (`--nats`), and
  tinygo.org/x/bluetooth over BlueZ/D-Bus (`--bluetooth`, Linux only)
- **Terminal handling**: golang.org/x/term
- **Development**: a local Go toolchain and `make`, or the dev sandbox — a pinned Go 1.26.10 container that runs the same on Linux, macOS, WSL2 and Windows. The NATS broker lives in `docker-compose.yml` next to the sandbox; nothing in the build or the test gate needs a container.

### Core Architecture

```text
cmd/<verb>/                 cobra command: flags and argument parsing
  ↓
internal/core/<transport>/  the operation: connect, host, scan
  ↓
internal/handlers/<transport>/  the rules: the websocket, NATS and Bluetooth sessions
  ↓
internal/e2e/               the end-to-end layer every session is wrapped in
  ↓
internal/chat/ + ui/        the terminal loop and the GUI, over one session.Session

beside that stack:
  internal/data/app/        the in-memory configuration (the port, the bind address)
  internal/pairing/         the credential: the shared secret, or this device's key
  internal/auth/            the shared secret and the TLS material
  internal/discovery/       the device identity on disk, and the pins trusted on first contact
```

The transport is chosen at the command level: `--nats` selects the NATS path,
`--bluetooth` the radio, and neither selects the websocket path. All three meet
at the same `internal/chat` loop over the same sealed `session.Session`, so they
are interchangeable from the user's point of view — with one limit the radio
imposes, below.

A host is a room rather than a pair: `internal/session.Hub` holds every
authenticated session and relays what one sends to all the others with the
sender named, and `internal/chat`'s `Room` is the terminal's single console for
that room — one stdin, so two devices never compete for the keyboard. The GUI
reads the same hub. Attribution is in-band: text is prefixed with the sender's
label, and a file frame carries it in an optional `wire.Header.Sender` field
that an older peer ignores.

The GUI sits beside the commands rather than inside them: `ui/` builds on the
same `session.Session` the terminal loops use, and `mobile/` is a second `main`
that starts that GUI on Android (and, later, iOS). The terminal and the GUI
therefore share the transports, the secrets and the TLS rules, and differ only
in how they read and write.

The Bluetooth transport is the exception to "the transports are
interchangeable", and the exception is the radio's, not a shortcut:

- **one device at a time on a host.** BlueZ does not say which central wrote to
  a peripheral's characteristic, so two devices cannot be told apart and there
  is no per-device channel to answer on. The slot frees when the keepalive
  notices the device has gone quiet;
- **nothing is announced.** There is no beacon and no port: discovery is a BLE
  scan for the service, so `cthrone scan --bluetooth` lists the devices in
  radio range rather than the hosts on a network;
- **text, not files in practice.** A packet carries 20 bytes, so a 256 KiB
  file chunk is thousands of notifications. Nothing refuses a file frame, and
  nothing offers one on a radio host: the terminal has no file support, and the
  GUI's transfer would arrive eventually, not usefully;
- **Linux only, for now** — the GATT layer speaks BlueZ over D-Bus, and
  `internal/core/bluetooth/unsupported.go` refuses everywhere else. The Android
  binding is a separate piece of work (issue #206): a phone build has no
  `--bluetooth` transport and the GUI does not offer the option.

The security model does not change: `internal/e2e` seals every session with the
device key before a payload crosses, and a peer that cannot prove its key is
refused, exactly as on a websocket.

## Project Structure

```text
cmd/                 cobra commands: connect, host, scan, peers, ui, recv, exec
internal/core/       one package per transport (websocket, nats, bluetooth)
internal/handlers/   one package per transport (websocket, nats, bluetooth)
internal/data/app/   the in-memory configuration
internal/pairing/    the credential a session uses: the shared secret, or this device's key
internal/auth/       the shared secret and the TLS material: where they come from, how they are compared
internal/discovery/  the device identity on disk, the beacon, and the pins trusted on first contact
internal/chat/       the terminal loop, and the console a host runs its room on
internal/e2e/        the end-to-end layer every session is wrapped in
internal/wire/       the CTH1 envelope and the file chunk arithmetic
internal/session/    the transport-agnostic session and hub the CLI and the GUI share
ui/                  the Fyne GUI, behind the `gui` build tag
mobile/              the Android/iOS entry point: a second `main` that starts `ui`
scripts/             add_to_path installers + the dev sandbox runtime
.github/workflows/   CI, the branch gate, and the draft-release workflow
docker-compose.yml   the dev sandbox and a local NATS server
```

## Development Commands

```bash
make check           # the local gate: gofmt, vet, test, build
make release         # build every CLI archive: Windows, Linux, Android, macOS
make release-gui     # the GUI archives a Linux host can produce (Linux, Windows)
make gui-android     # the Android APK: needs the Android SDK + NDK and the fyne CLI
make clean           # remove build/
go run ./ --help     # run from source
go test ./...        # tests only
```

`make check` is the gate every agent runs before committing. It fails when the
toolchain is missing — a gate that cannot run reports nothing, and reporting
nothing must not look like reporting success.

### Dev sandbox — the same toolchain on every machine

```bash
make dev-up          # start the sandbox (foreground; Ctrl+C stops it)
make dev-shell       # a shell inside the sandbox
make dev-opencode    # run the agent inside the sandbox (Linux/WSL2 only)
make dev-down        # stop the sandbox
make help            # every target, with a one-line description
```

Without `make`, on any OS with Docker:

```bash
docker compose up         # the sandbox
docker compose up -d nats # just the broker, on nats://127.0.0.1:4222
```

On native Windows, where `make` does not exist: `python scripts/dev-up.py dev-up`.

The container pins Go 1.26.10 — the version in `go.mod` — so a build inside the
sandbox is the build CI runs. The sandbox is **optional**: a host toolchain
still works, and `make check` does not require Docker.

The image creates the directories the compose file mounts into, owned by the
runtime user: Docker creates a mount destination the image does not contain as
**root**, and the container runs as `${HOST_UID}`, so a sandbox whose image
omits them cannot write its own caches. A sandbox created before that fix
keeps its old root-owned volumes — Docker does not re-initialise a volume that
already exists — so it needs `make reset` once.

### The current state of this suite — read this before claiming coverage

**Every package has a test file.** #85/#86 closed the last gap: all 29 packages
in the module now carry tests, so `go test ./...` prints no `? package [no test
files]` line and `make check` has no gap left to report.

- **Covered (29 packages)**: `main` (`github.com/fiwon123/cthrone`), `cmd`
  (root), `cmd/assets`, `cmd/connect`, `cmd/exec`, `cmd/host`, `cmd/peers`,
  `cmd/recv`, `cmd/scan`, `cmd/ui`, `internal/auth`, `internal/chat`,
  `internal/core/bluetooth`, `internal/core/nats`, `internal/core/websocket`
  (the scanner included), `internal/data/app`, `internal/discovery`,
  `internal/e2e`, `internal/exec`, `internal/handlers/bluetooth` (framing,
  reassembly and keepalive — portable, so tested without a radio),
  `internal/handlers/nats`, `internal/handlers/websocket`, `internal/pairing`,
  `internal/session`, `internal/shutdown`, `internal/wire`, `mobile`, `ui` (its
  non-GUI settings logic), `ui/widgets` (the selectable-text helpers, the
  copyable button and tab bar), and the gui-tagged widget tree (headless — see
  below).

So a green `make check` proves:

- the code is `gofmt`-clean,
- `go vet` found nothing,
- the code compiles,
- every package's tests passed,
- the gui-tagged widget tree's tests passed under Fyne's software driver.

**A test file is not depth.** The newest ones are small — `cmd/assets`,
`cmd/ui`, `internal/data/app` and `mobile` have one test function each. The
gui-tagged widget tree is now **executed**, not just type-checked:
`make check` and the `CI (Go)` job run
`CGO_ENABLED=0 go test -tags "gui ci" ./ui/... ./cmd/ui/...`, which swaps
Fyne's GL driver for the software one (`fyne.io/fyne/v2/app/app_software.go`)
and needs no GTK/X11/GL. What that still does not prove is a rendered frame
under the native driver, or the APK on a device (#155). The Bluetooth transport
is the same kind of limit: its framing, session and keepalive are exercised on
an in-memory link, and no gate here opens a radio — a claim that Bluetooth
works needs two devices (#202, #206). Report what executed, not just that the
package list is complete.

`make check` prints the list of packages with no test file after the run, so a
green run with no such list means every package was exercised.

## Runtime Environment

The AI agent (opencode) runs at the repository root. There are two modes, and
they are not equivalent — say which one a claim was made in.

### Host mode (the default)

The agent runs directly on the host, against the tools installed on the
machine. This is what `make check` and `git` operate on.

### Sandbox mode (optional)

`make dev-opencode` runs the agent inside the dev container, where the
toolchain is pinned to Go 1.26.10 — the version in `go.mod`, the same one CI
uses. The workspace is bind-mounted at `/workspace`; edits made inside are the
same files on the host.

The container has **no Docker socket**: the agent inside it cannot manage
containers, and that is deliberate. `docker` commands fail there, and a claim
about the sandbox made from inside it cannot be about the host's Docker.

The in-container agent needs a **Linux** opencode binary (the container is
Linux). On macOS and native Windows, `make dev-opencode` refuses with a clear
message and the agent stays on the host; use WSL2, where the host binary *is*
Linux.

### What the agent CAN do

- Run Go commands (`go build`, `go test`, `go vet`, `go run`, `gofmt`)
- Run `make` targets
- Run git and GitHub CLI commands
- Read and edit any file in the repository except the secret files listed below
- Start and use the dev sandbox (`make dev-up`, `make dev-shell`) from the host

### What the agent CANNOT do

- Access `.env`, `.env.*`, `.envrc`, or any other secret file without explicit permission
- Run commands that dump environment values (`printenv`, `env`)
- Run `docker` from **inside** the sandbox (no socket is mounted)
- Push directly to `dev` or `main`
- Merge a pull request without explicit instruction

## Key Conventions

- **Assignee**: `fiwon123` — `workflow.md` tells agents to read `ASSIGNEE` from here before assigning issues and PRs
- **GitHub credentials**: `GH_TOKEN` or `GITHUB_TOKEN` — either name works, and neither is defined here. The values belong in the host environment; nothing is written into the repository.
- **Model assignments**: `opencode.json` assigns `opencode/big-pickle` to `build` and `opencode/mimo-v2.6-flash-free` to `plan`. That is a starting point, not a recommendation — no prompt depends on which model an agent runs. Verify a new id with `opencode models` first: a catalog entry on models.dev does not guarantee the installed build offers it, and an unresolvable id fails at spawn rather than at config load.
- **Repository link**: the README links to the GitHub repository, and the release page is linked from the README — a reader is always one click from the source and from the download.
- **Formatting**: `gofmt` — `make check` fails on an unformatted file
- **Testing**: `go test` — standard library only, no assertion framework
- **CI**: GitHub Actions runs gofmt, vet, test, and build on `dev` → `main` pull requests only — never on a feature-branch pull request
- **Local testing**: run `make check` before pushing a feature branch. CI is not the gate for feature work; the local gate is.
- **Version**: the binary reports the tag it was built from, injected with `-X` into `github.com/fiwon123/cthrone/cmd.Version`. A build without that variable reports `vx.x.x`.
- **Ports**: 8080 by default, for both network transports — the websocket and
  the NATS path (`-p`). The radio has no port: a Bluetooth link has no address
  to type.
- **No anonymous mode**: every session is authenticated and encrypted by one of
  two credentials. See "Security" below.

### Security — read this before changing the transport

Every session is authenticated and encrypted end to end, on every transport;
the websocket connection is encrypted as well (`wss://`) unless `--no-tls` is
given. The model is the one issue #125 decided, superseding #11/#109:

- **exactly one credential authenticates a session**, and `internal/pairing`
  chooses it once so the transports cannot disagree:
  - **the device key**, when no secret is configured — the default. Each
    device generates an ed25519 key (in the platform user configuration
    directory: `~/.config/cthrone/identity.key` on Linux, `0600`), signs its
    handshake hello with it, and the peer pins the key under the device's name
    on first contact (`peers.json`, beside it). A different key under a pinned
    name is **refused**, never re-pinned; `cthrone peers forget` is the way
    back. **The pin is written only after the confirm authenticates the
    peer** — a hello alone records nothing, or anyone could take a name from a
    frame it never proved. On a **websocket** and on **Bluetooth** the host also
    shows a six-digit **first-contact code** on both screens and asks before the
    device joins; it **withholds its confirm** until the answer, so a peer
    that has not been accepted is neither authenticated nor pinned, an
    unanswered question expires into a refusal (silence is not a yes), and a
    name already pinned is never asked again. Only a host asks, and only where
    a host is: a NATS subject has no host and no single person to ask, so a
    first contact there is pinned without a prompt, and a terminal host with no
    tty accepts and pins and prints why;
  - **the shared secret**, when the user names one: `--token`, `CTHRONE_TOKEN`,
    or a file passed to `--token-file` — the compatibility path a v0.6.0 peer
    speaks. Nothing generates one any more: a minted token file would force
    every later run on that machine into secret mode, and a fresh device
    pairing by key could not join it. A file in the configuration directory is
    **not** read on its own (issue #163): a `token` left by v0.6.0 used to move
    the device into secret mode in silence, and the peer was then asked for a
    value nobody chose. The run names that file, offers to delete it, and never
    opens it;
- the handshake **version gates the models**: v1 is the shared secret, v2 is
  the device key, and a peer that speaks the other one is refused rather than
  downgraded. `internal/e2e` wraps every session either way: X25519, HKDF per
  direction, AES-256-GCM, and a sequence number that fails a replayed frame.
  In v2 the confirm repeats the device's key and signature, so a peer that
  joins a NATS subject after the hello was broadcast is still authenticated;
- the **websocket** transport is encrypted by default: the host serves
  `https` and the client dials `wss://`, and `--no-tls` is the opt-out kept for
  a v0.6.0 peer and for a network the user trusts. The host's certificate is
  self-signed, so the client **pins it on first contact** in `peers.json`,
  beside the device pins, keyed by the dialed `host:port`; a different
  certificate at a pinned address is **refused**, never trusted, and
  `cthrone peers forget <address>` is the way back. `--cert-fingerprint` pins
  by hand instead (and records nothing), `--insecure` skips the check. With a
  secret configured the handler also compares that secret's header and answers
  `401`; without one there is no header to compare, and the handshake is the
  gate;
- the **NATS** transport sends the broker's credentials from the environment
  (`NATS_USER`, `NATS_PASSWORD`), and a `tls://` broker URL asks for an
  encrypted connection with the same pinning. Even without it, the broker
  carries ciphertext: it cannot read the payloads, only the subject and the
  timing;
- the **Bluetooth** transport adds no transport-level encryption of its own and
  does not need one: the radio link is short-range and paired at the operating
  system's level, and the end-to-end layer seals every payload before it is
  fragmented into 20-byte packets. What stays visible is the shape — packet
  counts and timing — exactly as on NATS. The device-key handshake is the same
  one the websocket runs, so a peer on the radio must prove its key too, and the
  host asks the same first-contact question; a Bluetooth peer is pinned in the
  same `peers.json`;
- the `docker-compose.yml` broker asks for credentials and **persists
  nothing**: JetStream is off, so a conversation does not outlive the
  session it happened in.

The rules that come with it:

- "encrypted by default" means the **payload** is sealed, and the websocket
  connection is encrypted too. It does not mean the conversation is hidden: on
  NATS the subject and the timing stay visible, on the radio the packet count
  and the timing do, and `--no-tls` puts the websocket's shape back on the
  wire. On a websocket the host is the correspondent, not a relay, so the
  transport encryption protects the wire and the end-to-end layer protects the
  peer on NATS and on Bluetooth;
- **first contact is the only moment a machine in the middle can insert
  itself**, and the fingerprint comparison is what closes it. Say that plainly
  wherever pairing is described; a pin is only as good as the comparison. Each
  screen therefore prints **both** fingerprints — the peer's and its own — so
  the two values that have to agree are on both screens at once, and a
  websocket first contact is confirmed by a short code both screens show
  rather than by a forty-character hash; the code is derived from the two
  ephemeral keys, so a spliced session shows two different ones. The
  certificate pin follows the same rule: the first certificate at an address
  is recorded, and the device-key pin behind it is the stronger gate;
- do not add a credential to a **command-line flag** — a flag value is visible
  in the process list. Use an environment variable or a file;
- the secret and the device key are keys, not passwords: never log them, print
  them, or put them in a status line;
- the broker reads its password through its own configuration parser, so a
  password with a `#` or a quote in it does not survive the trip;
- if you touch the transport, read issues #11, #109, #125 and #202 first: the
  security model is a design decision, not an implementation detail, and it
  changes the CLI surface. #202 added a carrier and changed no rule.

## Environment and Secret-File Rules

- The application holds no configuration file in the repository — the port is
  the only setting, and it comes from the command line.
- The user's configuration directory holds what the tool writes for itself:
  this device's key (`identity.key`, `0600`), the pins it trusts
  (`peers.json`, `0600` — public keys and certificate fingerprints, but who you
  talk to is nobody else's business), and the TLS material (`cert.pem` and
  `key.pem`, the key `0600`). They are written on first use and never
  committed. The tool writes **no** `token` file: a v0.6.0 installation may
  still have one there, and nothing reads it unless a run passes
  `--token-file`. The secret and the device key are the two things that must
  not be shared; `peers.json` is the record that makes the second one
  meaningful, and the certificate pins in it are what make `wss://` the default
  without a value to copy.
- That directory is the one the **platform** provides, not a hard-coded
  `~/.config`: `internal/discovery.ConfigDir` asks `os.UserConfigDir` and
  appends `cthrone`, so it is `~/.config/cthrone` on Linux,
  `~/Library/Application Support/cthrone` on macOS and `%AppData%\cthrone` on
  Windows. The CLI and the desktop GUI both use it, so one machine is one
  device; the phone app uses its own private storage, because a phone has no
  user configuration directory to rely on. Tests isolate it with
  `XDG_CONFIG_HOME`, which `os.UserConfigDir` honours — setting only `HOME` is
  not isolation.
- Never access `.env`, `.env.*`, `.envrc`, or any other environment/secret file without explicit permission.
- Never open, read, print, summarize, quote, or send the contents of those files. `.env.example` is the one exception: it holds placeholders and is safe to read.
- Read `.env.example` with the **Read tool**, not with `cat`. Bash denies cover every `.env*` path on purpose.
- Never run commands that reveal environment values — `printenv`, `env`. `docker compose config` is on the deny list for the same reason: it renders the *interpolated* values.
- Never display, repeat, log, store, or include secret values in responses, code changes, or commits.
- To confirm a variable is set, check for presence, never print it: `test -n "$VAR" && echo set`.

### Enforcement and its limits

`opencode.json` enforces the rules above at the tool level. The top-level
`permission` block denies `read` and `edit` of `*.env`, `*.env.*`, and
`*.envrc` while allowing `*.env.example`, and denies `bash` commands matching
those paths or dumping the environment. Every agent inherits it, and each agent
repeats the deny list locally so the project stays guarded when one file is
edited in isolation.

Treat it as a backstop, not as the permission. The rules match **command
strings and file paths**, not intent, so they have known gaps you must not rely
on your way past:

- An allowed interpreter can still reach a file. `go run ./script.go` matches
  no deny pattern, because the pattern is matched against the command string,
  not what the command does.
- A repo-wide `grep -rn SECRET .` ignores `.gitignore`, so it will read a
  `.env` that is correctly excluded from version control. Prefer the Grep
  tool, which is gitignore-aware.
- Rules are evaluated with the **last matching pattern winning**, and
  agent-level rules take precedence over global ones. A broad allow placed
  after a narrow deny silently reinstates the leak — which is why every agent
  that allows `grep *` also denies `grep *.env`, `grep *.env.*`, and
  `grep *.envrc` explicitly, positioned *after* the allow.

## Development Workflow

### Git Workflow

- Branch from `dev` (not `main`)
- Never push directly to `dev` or `main`
- Branch naming: `<type>/<issue-number>-<slug>` (e.g. `feat/12-nats-auth`)
- Branch types: `feat/`, `fix/`, `refactor/`, `docs/`, `test/`, `chore/`, `ci/`
- Use conventional commits: `feat:`, `fix:`, `refactor:`, `docs:`, `test:`, `chore:`, `ci:`
- All testing is local for feature branches — CI only runs on `dev` → `main` pull requests
- Do NOT merge pull requests unless explicitly instructed
- Do NOT automatically create release pull requests or merge to `main` — the user must explicitly request it
- Always return to `dev` after completing any merge
- Every change is tracked on GitHub: **issue → branch → PR → merge**

### Linking

- Every branch includes the issue number: `feat/12-nats-auth`
- Every pull request references its issue: `Closes #<number>`
- Every issue is assigned to a milestone before work begins
- Every pull request is assigned to a milestone when created
- No orphaned branches, pull requests, or issues

> The repository's **default branch is `dev`**, so `Closes #N` in a pull request
> body does close the issue when that pull request merges into `dev` — GitHub
> auto-closes on a merge into the *default* branch, and `dev` is it. Run
> `gh issue close` after the merge anyway: it is the verification, and it is
> expected to answer "already closed". The release pull request (`dev` → `main`)
> closes nothing, because every milestone issue is closed before it is opened.

### Branch Strategy

```text
main          ← release merges (CI runs here — dev→main PRs only)
  └── dev     ← integration branch (all feature branches merge here)
        ├── feat/11-transport-security
        └── chore/10-opencode-agent-layer
```

- Feature branches: `<type>/<issue>-<slug>` → merge to `dev`
- Release: `dev` → `main` (a pull request — CI runs there)
- Hotfixes: `fix/<issue>-<slug>` → merge to `dev`, cherry-pick to `main` if urgent

### The legacy `feature/*` branches (deleted)

Branches named `feature/<number>-<slug>` predate the convention above. The
branch gate still rejects that name, so a pull request opened from one fails.
All of them have been deleted — they were merged (as squashes) or abandoned —
and the remote carries only `dev` and `main`. Do not recreate one.

### Branch gate

`.github/workflows/branch-gate.yml` fails a pull request whose branch does not
follow the model above, so the author finds out before a reviewer does. It is a
**guard rail, not a lock**: an administrator can override a failed required
check, and a check nobody blocks on is noise. Branch protection (below) is the
control; the gate is the feedback.

**The Dependabot exemption.** Dependabot derives its head branch from the
ecosystem, the target branch and the dependency —
`dependabot/<ecosystem>/<target-branch>/<dependency>-<version>` — and offers no
way to rename it. The convention above therefore can never apply to a
dependency update, and a gate that rejects the dependency automation forever is
a check nobody can pass, which is the "noise" failure this section warns about.
So `dependabot/` is accepted **for a `dev` base only**:

| Base | Accepted head branches |
|---|---|
| `dev` | `<type>/<issue-number>-<slug>`, or `dependabot/…` |
| `main` | `dev`, or `fix/<issue-number>-<slug>` — unchanged |

The exemption stops at `dev` on purpose: a dependency bump still has to reach
`main` through the release pull request, so it cannot bypass the integration
branch. The pattern is the exact prefix `^dependabot/`, not a looser match, so
a human who wants a differently named branch still has the convention above.

A dependency pull request into `dev` gets the gate and **nothing else** — CI
runs on release pull requests only. Verification of a bump is therefore the
merger's local `make check`; a `gui`-tagged dependency (Fyne) can be tested
headless (`-tags "gui ci"`) but cannot be linked natively in this environment,
so its PR body must say what was and was not verified.

### Repository settings (configure by hand)

These live in the repository settings, not in a file, so no agent can set them.
Verify them once after a transfer.

**Branch protection** (Settings → Branches → Rules):

- `dev`: require a pull request, and require the **Branch gate** check. Do
  *not* require the CI check here — CI runs on release pull requests only, so a
  required check that never runs leaves every pull request waiting forever.
- `main`: require a pull request, and require both **CI** and **Branch gate**.
  Do not allow deletions: `dev` is the head of the release pull request, so
  deleting it breaks every future pull request's base.

**Other settings worth turning on:**

- Secret scanning and push protection (Settings → Code security).
- Tag protection for `v*`, so a published release tag cannot be moved.
- Dependabot targeting `dev` — see `.github/dependabot.yml`.

### Milestones

- Every issue MUST be assigned to a milestone before work begins
- Milestones represent releases or sprint iterations
- Use `gh issue edit <issue-number> --milestone "<milestone-name>"`
- Track milestone progress on the GitHub Milestones page

### Agents

Agent definitions live in `opencode.json` and `.opencode/agents/`:

- **build** (primary): Full development work with all tools enabled
- **plan** (primary): Analysis and planning without making changes — restricted to read-only subagents (explore, reviewer only)
- **reviewer** (subagent): Reviews code for bugs, security, and regressions

Each agent's instructions live in exactly one place: the body of
`.opencode/agents/<name>.md`. Model, mode, and permissions live in
`opencode.json`. Do not add a `prompt` key to `opencode.json` — it creates a
second prompt source that drifts from the markdown and leaves it undefined which
one wins.

An agent named in `opencode.json` without a matching markdown file has no
instructions at all. If you add an agent, add its `.md` in the same commit.

### Skills

- `github-workflow` ships in `.opencode/skills/github-workflow/SKILL.md` — the command cookbook for git, issues, and pull requests. `workflow.md` remains the policy authority; the skill is the quick reference.

### Session Memory (4-tier)

- At the START of every new chat: read `.opencode/memory/global.md` then
  `.opencode/memory/current.md` and resume from "Next action" — this is how
  long-running work survives between sessions. If the state references an
  issue/branch/PR, continue that work unless the user says otherwise.
- At meaningful milestones (issue created, branch renamed, code done, tests
  run, PR opened, merged) update `global.md`; update `current.md` after each
  step and at session end rotate the oldest archive out so only 3 remain.
- Durable, reusable lessons (a footgun, the command that works, a decision's
  rationale) go to `lessons.md`; offload them there when `global.md` approaches
  its cap rather than letting tracked work and lessons share one file.
- Keep sizes bounded: `global.md` ≤ ~200 lines, `lessons.md` ≤ ~100 lines,
  `current.md` ≤ ~100 lines, archive files ≤ ~50 lines each; never store
  secrets/tokens/credentials.
- The whole `.opencode/memory/` directory is gitignored and must never be
  committed. Full rules: `.opencode/instructions/memory.md`.

### Rules for Agents

All workflow rules that agents must follow are documented in this file:

- branch strategy and naming conventions;
- CI strategy — `.github/workflows/ci.go.yml` runs on release pull requests (`dev` → `main`) and manual dispatch, not on feature-branch pull requests, which are gated by local `make check`;
- release strategy — `.github/workflows/release.yml` builds every asset and creates a **draft** release; a human publishes it. It runs on manual dispatch, from `main` only, and never pushes a tag;
- milestone and issue conventions;
- development commands — `make check`, `make release`, `make release-gui`, `make clean`;
- environment and secret-file rules;
- the transport security gap above, and the rule that a skipped or empty run is not a pass.

### CI triggers (read this before claiming a change is verified)

CI runs on release pull requests (`dev` → `main`) and manual dispatch — never on
a feature-branch push. Two things can make a run verify nothing:

- a `paths:` filter that the change does not touch means the workflow does not
  trigger at all; and
- a job gated behind a label or `if:` condition comes back `skipped`, not
  `passed`.

**Skipped is not passed.** A triggered run whose jobs were all gated out, or a
run that never happened, is not coverage. Do not describe a change as "CI
passed" unless a run actually executed the job that covers it; if it did not,
say the change is unverified and test it locally.

The same rule applies locally: `go test` reporting `[no test files]` for a
package is not a pass for that package.

### Release workflow (read this before tagging)

`.github/workflows/release.yml` builds the release and opens a **draft**. It is
manual-dispatch, `main`-only, and it never publishes and never pushes a tag —
publishing the draft by hand creates the tag.

```bash
gh workflow run release.yml --ref main -f version=v0.2.0
```

- It runs `make check` first, so a red tree produces no draft at all.
- It builds the CLI for Windows, Linux, Android and macOS, the Fyne GUI for
  Linux, Windows and macOS, and the Android APK, writes `checksums.txt`, and
  attaches every archive to the draft.
- The GUI jobs need CGO and platform headers (GTK3/X11/GL on Linux, mingw-w64
  for Windows, a macOS runner for darwin), so they exist in CI and not in the
  dev sandbox — a GUI build cannot be verified locally. The APK is built on
  `ubuntu-latest` with the runner's Android SDK and NDK; `make gui-android`
  needs the same three things (SDK, NDK, `fyne` CLI) and refuses with a clear
  message when one is missing.
- The APK is packaged **without** `fyne package --release`: that flag emits an
  `.aab` for a store, while a sideload installs the debug-signed `.apk`.
- Re-running the same version refreshes the existing draft; a release that is
  already **published** is refused, not modified.
- The workflow itself is not covered by CI. Its first real proof is a dry run,
  and a run whose jobs never executed proves nothing.

