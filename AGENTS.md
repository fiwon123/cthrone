# AGENTS.md

> Working instructions for agents in this repository. `PROJECT_CONTEXT.md` holds
> the structured project identity behind this document; keep the two in step.

### Product

## Project Objective

cthrone — a terminal chat tool that carries messages from one device to
another, over a websocket or a NATS server, with no GUI.

### Problem

Two devices that need to exchange a message — a phone and a laptop, a sensor
and a workstation — normally need an account, a server, or a GUI. For a short
internal exchange, that is more setup than the message is worth.

### Goal

A user can start a chat server on one device, connect from another with one
command, and exchange messages in the terminal — over the local network or
through a NATS broker.

### Main Technology

- **Language**: Go 1.25.5 (pinned in `go.mod`)
- **CLI framework**: spf13/cobra
- **Transports**: gorilla/websocket (default) and nats-io/nats.go (`--nats`)
- **Terminal handling**: golang.org/x/term
- **Development**: a local Go toolchain and `make`, or the dev sandbox — a pinned Go 1.25.5 container that runs the same on Linux, macOS, WSL2 and Windows. The NATS broker lives in `docker-compose.yml` next to the sandbox; nothing in the build or the test gate needs a container.

### Core Architecture

```text
cmd/<verb>/                 cobra command: flags and argument parsing
  ↓
internal/core/<transport>/  the operation: connect, host, scan
  ↓
internal/handlers/<transport>/  the rules: the websocket and NATS sessions
  ↓
internal/data/app/          the in-memory configuration (the port, the bind address)
  ↓
internal/auth/              the shared secret and the TLS material: where they come from, how they are compared
```

The transport is chosen at the command level: `--nats` selects the NATS path,
and its absence selects the websocket path. Both paths meet at the same chat
loop, so the two transports are interchangeable from the user's point of view.

## Project Structure

```text
cmd/                 cobra commands: connect, host, scan
internal/core/       one package per transport (websocket, nats)
internal/handlers/   one package per transport (websocket, nats)
internal/data/app/   the in-memory configuration
internal/auth/       the shared secret and the TLS material of the websocket transport
scripts/             add_to_path installers + the dev sandbox runtime
docker-compose.yml   the dev sandbox and a local NATS server
```

## Development Commands

```bash
make check           # the local gate: gofmt, vet, test, build
make release         # build the Windows and Linux archives and the Android binary
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

The container pins Go 1.25.5 — the version in `go.mod` — so a build inside the
sandbox is the build CI runs. The sandbox is **optional**: a host toolchain
still works, and `make check` does not require Docker.

The image creates the directories the compose file mounts into, owned by the
runtime user: Docker creates a mount destination the image does not contain as
**root**, and the container runs as `${HOST_UID}`, so a sandbox whose image
omits them cannot write its own caches. A sandbox created before that fix
keeps its old root-owned volumes — Docker does not re-initialise a volume that
already exists — so it needs `make reset` once.

### The current state of this suite — read this before claiming coverage

**The coverage is partial.** Both transports, the two commands that use them
and the `host`/`connect` command flags have tests; the scanner and the `scan`
command do not. `go test ./...` prints `? package [no test files]` for those,
and exits 0. That is not a pass: it means nothing was executed for them.

So a green `make check` proves:

- the code is `gofmt`-clean,
- `go vet` found nothing,
- the code compiles,
- the tested packages passed — currently both transports and the two
  commands that use them.

It does **not** prove the behaviour of the scanner works. A chat tool's
behaviour — a message that arrives, a host that binds the right port, a scan
that finds the server — is partly untested here. Report honestly: "gofmt, vet,
and build are clean; the transports are covered, the scanner is not", not
"tests pass".

`make check` prints the list of packages with no test file after the run, so
the gap is visible in the output rather than only in this document.

## Runtime Environment

The AI agent (opencode) runs at the repository root. There are two modes, and
they are not equivalent — say which one a claim was made in.

### Host mode (the default)

The agent runs directly on the host, against the tools installed on the
machine. This is what `make check` and `git` operate on.

### Sandbox mode (optional)

`make dev-opencode` runs the agent inside the dev container, where the
toolchain is pinned to Go 1.25.5 — the version in `go.mod`, the same one CI
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
- **Ports**: 8080 by default, for both the websocket and the NATS path (`-p`).
- **No authentication**: the transports are currently open. See "Security" below.

### Security — read this before changing the transport

Both transports authenticate their peers, and both can encrypt the traffic.
The model is the one issue #11 decided:

- the **websocket** transport requires a **shared secret** — a client that
  does not know it is refused — and `--tls` encrypts the connection, with
  the host's certificate pinned by its fingerprint;
- the **NATS** transport sends the broker's credentials from the environment
  (`NATS_USER`, `NATS_PASSWORD`), and a `tls://` broker URL asks for an
  encrypted connection with the same pinning;
- the `docker-compose.yml` broker asks for credentials and **persists
  nothing**: JetStream is off, so a conversation does not outlive the
  session it happened in.

The rules that come with it:

- do not describe this tool as encrypted or authenticated **by default**:
  the websocket transport needs a shared secret, and `--tls` on top of it;
  the NATS path needs a broker that asks for credentials;
- do not add a credential to a **command-line flag** — a flag value is visible
  in the process list. Use an environment variable or a file;
- the broker reads its password through its own configuration parser, so a
  password with a `#` or a quote in it does not survive the trip;
- if you touch the transport, read issue #11 first: the security model is a
  design decision, not an implementation detail, and it changes the CLI
  surface.

## Environment and Secret-File Rules

- The application holds no configuration file in the repository — the port is
  the only setting, and it comes from the command line.
- The user's configuration directory **does** hold two files: the shared
  secret (`~/.config/cthrone/token`, `0600`) and the TLS material
  (`~/.config/cthrone/cert.pem` and `key.pem`, the key `0600`). They are
  generated on first use, they are never committed, and the secret is the
  one thing that must not be shared.
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

> `Closes #N` does not close the issue on a `dev` merge. GitHub auto-closes a
> linked issue only when the pull request merges into the repository's *default*
> branch, which is `main`. A feature pull request merges into `dev`, so
> `Closes #N` gives you the cross-reference and nothing more. Close the issue
> explicitly with `gh issue close` after the merge to `dev`.

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

### The legacy `feature/*` branches

The repository still carries branches named `feature/<number>-<slug>` from
before the convention changed. The branch gate rejects those names, so a pull
request opened from one of them fails.

None of them needs renaming — they are leftovers, not work in progress:

| Branch | State |
|---|---|
| `feature/2-implement-nats` | already merged (as a squash) |
| `feature/4-implement-security-on-websocket` | already merged, and identical to `dev` — the security work was never done |
| `feature/5-implement-chat-same-terminal-websocket` | already merged (as a squash) |
| `feature/1-implement-bluetooth-connection` | **abandoned** — the zeroconf dependency was removed on purpose |

Deleting them is a separate decision and is not part of any pull request.
Until they are gone, do not open a pull request from one.

### Branch gate

`.github/workflows/branch-gate.yml` fails a pull request whose branch does not
follow the model above, so the author finds out before a reviewer does. It is a
**guard rail, not a lock**: an administrator can override a failed required
check, and a check nobody blocks on is noise. Branch protection (below) is the
control; the gate is the feedback.

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
- milestone and issue conventions;
- development commands — `make check`, `make release`, `make clean`;
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
