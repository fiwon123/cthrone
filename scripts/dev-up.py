#!/usr/bin/env python3
"""Cross-platform dev-sandbox launcher

Why this exists
---------------
The Makefile is a great interface on Linux, macOS and WSL2, but it cannot work
on native Windows: its preflight and recipes use POSIX ``test -x``/``stat``/
``mkdir -p``/``id -u``, and ``make`` there runs under cmd.exe/PowerShell rather
than a POSIX shell. Windows users therefore had no supported way to start the
sandbox with anything other than a bare ``docker compose up``.

This script reproduces the Makefile's developer targets in Python, using only
the standard library, so the same commands run on Linux, macOS, WSL2 and native
Windows (PowerShell/cmd). The Makefile remains the recommended path on POSIX
hosts; both delegate to the same docker-compose files.

It also preserves a correctness property rather than just porting commands: the
in-container AI agent requires a *Linux* opencode binary. Demanding
``~/.opencode/bin/opencode`` for every sandbox start would (a) make Windows and
macOS users install a tool that can never run inside the Linux container and
(b) block ``dev-up`` for people who only wanted the application. Here the agent
is optional: the stack starts either way, and only agent-specific commands
require it.

Usage
-----
    python scripts/dev-up.py <command> [options]

Commands:
    dev-up        Build/start the sandbox (foreground)
    dev-down      Stop the sandbox (volumes kept)
    dev-restart   down + up --build in one step
    dev-log       Tail the dev container logs
    dev-exec      Shell into the running dev container
    opencode      Run the AI coding agent inside the dev container
    shell         Interactive shell in the dev container (agent ready)

Run ``python scripts/dev-up.py help`` for the full text.

Demo users do not need this script at all: ``docker compose up`` with no
arguments starts the project. This script is for development.
"""

from __future__ import annotations

import hashlib
import os
import re
import shutil
import subprocess
import sys
import time
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parents[1]

BASE_COMPOSE = ["-f", str(REPO_ROOT / "docker-compose.yml")]
AGENT_COMPOSE = [
    *BASE_COMPOSE,
    "-f",
    str(REPO_ROOT / "docker-compose.agent.yml"),
]

DEFAULT_WAIT_TIMEOUT = 180
DEV_SERVICE = "dev"

IS_WINDOWS = os.name == "nt"
IS_POSIX = not IS_WINDOWS


# ---------------------------------------------------------------------------
# Output helpers
# ---------------------------------------------------------------------------


def log(message: str) -> None:
    print(f"[sandbox] {message}", file=sys.stderr, flush=True)


def warn(message: str) -> None:
    print(f"WARN: {message}", file=sys.stderr, flush=True)


def error(message: str) -> None:
    print(f"ERROR: {message}", file=sys.stderr, flush=True)


# ---------------------------------------------------------------------------
# Host discovery - every value Compose needs, derived without a POSIX shell
# ---------------------------------------------------------------------------


def host_home() -> Path:
    # Path.home() honours HOME on POSIX and USERPROFILE on Windows.
    return Path.home()


def host_uid() -> str:
    if IS_WINDOWS:
        # Docker Desktop virtualises bind-mount ownership; the image's default
        # devuser is 1000 and the value is not observable from the container.
        return os.environ.get("HOST_UID") or "1000"
    return os.environ.get("HOST_UID") or str(os.getuid())


def host_gid() -> str:
    if IS_WINDOWS:
        return os.environ.get("HOST_GID") or "1000"
    return os.environ.get("HOST_GID") or str(os.getgid())


def host_project_dir() -> str:
    # Respect an inherited value first: invoking this from inside the dev
    # container must still send the HOST path to the host Docker daemon.
    return os.environ.get("HOST_PROJECT_DIR") or str(REPO_ROOT)


def compose_path(value: Path | str) -> str:
    """Render a host path the way Docker Compose expects on this platform.

    Windows short-syntax volumes with ``C:\\...`` are ambiguous to the volume
    parser; forward slashes (``C:/...``) are accepted everywhere and are what
    Compose itself normalises to. POSIX paths are unchanged.
    """
    return Path(value).as_posix() if IS_WINDOWS else str(value)


# ---------------------------------------------------------------------------
# The in-container agent: optional, and Linux-only by nature
# ---------------------------------------------------------------------------


# The command line each agent starts with, plain and in auto mode. Mirrors the
# Makefile's *_CMD variables — see the comment there for what each mode allows.
# Codex's own sandbox (bubblewrap) cannot create a namespace inside an
# unprivileged container, so the container is its sandbox in both modes.
AGENTS: dict[str, dict[str, list[str]]] = {
    "opencode": {"plain": ["opencode"], "auto": ["opencode", "--auto"]},
    "claude": {"plain": ["claude"], "auto": ["claude", "--dangerously-skip-permissions"]},
    "codex": {
        "plain": ["codex", "--sandbox", "danger-full-access"],
        "auto": ["codex", "--dangerously-bypass-approvals-and-sandbox"],
    },
}


def compose_environment(*, with_github: bool) -> dict[str, str]:
    env = os.environ.copy()
    env["HOST_UID"] = host_uid()
    env["HOST_GID"] = host_gid()
    env["HOST_HOME"] = compose_path(host_home())
    env["HOST_PROJECT_DIR"] = compose_path(host_project_dir())
    if with_github:
        token, _source = resolve_gh_token()
        if token:
            env["GH_TOKEN"] = token
    return env


def prepare_agent_mounts() -> None:
    """Create the read-only mount sources the agent overlay requires.

    A missing bind-mount source is fatal on Windows and creates root-owned
    directories on Linux, so these are made before Compose starts.
    """
    home = host_home()
    (home / ".config" / "gh").mkdir(parents=True, exist_ok=True)
    gitconfig = home / ".gitconfig"
    if not gitconfig.exists():
        gitconfig.touch()


# ---------------------------------------------------------------------------
# GitHub credential passthrough (parity with scripts/resolve-gh-token.sh)
# ---------------------------------------------------------------------------


def resolve_gh_token() -> tuple[str, str]:
    """Return ``(token, source)``; token is empty when none is available."""
    for name in ("GH_TOKEN", "GITHUB_TOKEN"):
        value = os.environ.get(name)
        if value:
            return value, name
    gh = shutil.which("gh")
    if not gh:
        return "", "missing gh"
    env = {k: v for k, v in os.environ.items() if k not in ("GH_TOKEN", "GITHUB_TOKEN")}
    try:
        result = subprocess.run(  # noqa: S603 - fixed argv, no shell
            [gh, "auth", "token"],
            capture_output=True,
            text=True,
            timeout=15,
            env=env,
            check=False,
        )
    except (OSError, subprocess.SubprocessError):
        return "", "gh auth token"
    token = result.stdout.strip() if result.returncode == 0 else ""
    return token, "gh auth token"


def check_gh_token() -> bool:
    """Validate the credential, matching the shell helper's contract.

    Rejected -> fatal (a rejected token is worse than none: gh looks
    authenticated inside the container and 401s on every call, and it breaks
    gh/git on the host too).
    Absent / offline / unverifiable -> warning, never blocks a local sandbox.
    The token itself is never printed.
    """
    token, _source = resolve_gh_token()
    if not token:
        warn("no GitHub credential found - 'gh' will not be authenticated in the dev sandbox.")
        warn("  Fix: gh auth login   (or pass GH_TOKEN=<pat> to the command)")
        return True

    gh = shutil.which("gh")
    if not gh:
        warn("gh is not installed on this host; skipping credential validation.")
        return True

    env = {k: v for k, v in os.environ.items() if k != "GITHUB_TOKEN"}
    env["GH_TOKEN"] = token
    try:
        result = subprocess.run(  # noqa: S603 - fixed argv, token via env
            [gh, "api", "user"],
            capture_output=True,
            text=True,
            timeout=25,
            env=env,
            check=False,
        )
    except subprocess.TimeoutExpired:
        warn("could not reach GitHub to verify the credential (offline?); continuing.")
        return True
    except (OSError, subprocess.SubprocessError):
        warn("could not run 'gh' to verify the credential; continuing.")
        return True

    output = f"{result.stdout}\n{result.stderr}"
    if re.search(r"bad credentials|HTTP 401|requires authentication|invalid token", output, re.I):
        error("the GitHub token available to this shell is rejected by GitHub.")
        error("  A GH_TOKEN/GITHUB_TOKEN in the environment takes precedence over your")
        error("  keyring login, so a stale one shadows a working credential and would be")
        error("  injected into the dev container (it also breaks 'gh' and 'git' on the host).")
        error("  Fix: unset GH_TOKEN GITHUB_TOKEN, or refresh the token, then retry.")
        return False
    if re.search(
        r"error connecting|dial tcp|no such host|i/o timeout|connection reset|"
        r"connection refused|could not resolve|network is unreachable|TLS handshake",
        output,
        re.I,
    ):
        warn("could not reach GitHub to verify the credential (offline?); continuing.")
        return True
    if '"login"' not in output:
        warn("unexpected response while verifying the GitHub credential; continuing.")
    return True


# ---------------------------------------------------------------------------
# Docker Compose plumbing
# ---------------------------------------------------------------------------


def docker_compose_argv(files: list[str], args: list[str]) -> list[str]:
    return ["docker", "compose", *files, *args]


def require_docker() -> None:
    if not shutil.which("docker"):
        error("docker was not found on PATH.")
        error("  Install Docker Desktop (Windows/macOS) or Docker Engine (Linux), then retry.")
        raise SystemExit(1)


def run_compose(
    files: list[str],
    args: list[str],
    *,
    with_github: bool = False,
    capture: bool = False,
) -> subprocess.CompletedProcess[str]:
    require_docker()
    argv = docker_compose_argv(files, args)
    try:
        return subprocess.run(  # noqa: S603 - argv built here, no shell
            argv,
            cwd=REPO_ROOT,
            env=compose_environment(with_github=with_github),
            capture_output=capture,
            text=True,
            check=False,
        )
    except KeyboardInterrupt:
        # Ctrl+C during a foreground `up` is the normal way to stop it.
        return subprocess.CompletedProcess(argv, 130, "", "")


def _ps_field(files: list[str], service: str, field: str) -> str:
    result = run_compose(
        # A Go template: `{{.State}}`. Without the dot Compose rejects it
        # ("function "State" not defined"), every lookup reads as "not running",
        # and each agent start waits the full timeout first.
        files, ["ps", "--all", "--format", f"{{{{.{field}}}}}", service], capture=True
    )
    if result.returncode != 0:
        return ""
    lines = (result.stdout or "").strip().splitlines()
    return lines[0].strip() if lines else ""


def ensure_sandbox_running() -> None:
    """Bring the stack up without gratuitously rebuilding an existing image.

    Mirrors the Makefile's intent: build only when the sandbox was never
    initialized; otherwise reconcile and wait for readiness. A running stack is
    left completely untouched so a live `opencode` session is not clobbered.
    """
    files = AGENT_COMPOSE
    state = _ps_field(files, DEV_SERVICE, "State")

    if not state:
        log("dev is not initialized yet - building and starting the stack (this runs once).")
        run_compose(files, ["up", "-d", DEV_SERVICE])
    elif state == "running":
        log("dev is already running - skipping image build.")
    else:
        log(f"dev is '{state}' - starting the existing container without rebuilding.")
        run_compose(files, ["up", "-d", DEV_SERVICE])

    if not wait_for_dev(files):
        log("Continuing anyway - the container may still be initializing.")


def wait_for_dev(files: list[str], timeout: int = DEFAULT_WAIT_TIMEOUT) -> bool:
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        state = _ps_field(files, DEV_SERVICE, "State")
        if state == "running":
            health = _ps_field(files, DEV_SERVICE, "Health")
            if not health or health == "healthy":
                return True
            if health == "unhealthy":
                log("dev reported unhealthy - continuing to wait (it may recover).")
        time.sleep(2)

    log(f"Timed out after {timeout}s waiting for '{DEV_SERVICE}'. Recent status:")
    run_compose(files, ["ps", DEV_SERVICE])
    log("Follow the logs with: dev-log")
    return False


# ---------------------------------------------------------------------------
# Is the dev image current? (mirrors the Makefile's check-image)
# ---------------------------------------------------------------------------

# The Dockerfile and every file it COPYs — the SAME list, in the same order, as
# IMAGE_INPUTS in the Makefile. `make check` compares the two hashes.
IMAGE_INPUTS = ["Dockerfile", "scripts/dev-sandbox-rc.sh"]


def image_inputs_hash() -> str:
    """First 16 hex digits of SHA-256 over IMAGE_INPUTS, concatenated in order."""
    digest = hashlib.sha256()
    for rel in IMAGE_INPUTS:
        path = REPO_ROOT / rel
        if path.exists():
            digest.update(path.read_bytes())
    return digest.hexdigest()[:16]


def dev_image_name() -> str:
    """Compose names a built image `<project>-<service>`; the project is `name:`."""
    compose = REPO_ROOT / "docker-compose.yml"
    text = compose.read_text(encoding="utf-8") if compose.exists() else ""
    # Without `name:`, Compose names the project after the directory.
    match = re.search(r"^name:\s*(\S+)", text, re.M)
    return f"{match.group(1) if match else REPO_ROOT.name}-{DEV_SERVICE}"


def image_label() -> str:
    """The image's `dev.image-inputs` label; empty when the image does not exist."""
    require_docker()
    result = subprocess.run(  # noqa: S603 - fixed argv, no shell
        ["docker", "image", "inspect", "--format",
         '{{ index .Config.Labels "dev.image-inputs" }}', dev_image_name()],
        capture_output=True, text=True, check=False,
    )
    return result.stdout.strip() if result.returncode == 0 else ""


def check_image() -> None:
    """Build a missing image; refuse a stale one by name (dev-up never rebuilds)."""
    have = image_label()
    if not have:
        log("no dev image yet - building it (this runs once).")
        if cmd_dev_build() != 0:
            raise SystemExit(1)
    elif have != image_inputs_hash():
        error("the dev image is out of date: it was not built from the current "
              + " ".join(IMAGE_INPUTS) + ".")
        error("  (an edited Dockerfile, a bumped agent or toolchain pin, or an image built")
        error("  outside dev-build) Rebuild it: dev-build - agent logins are kept in volumes.")
        raise SystemExit(1)


# ---------------------------------------------------------------------------
# Commands
# ---------------------------------------------------------------------------


def preflight() -> None:
    if IS_POSIX and os.getuid() == 0:
        error(
            "refusing to run the dev sandbox as UID 0; repair workspace ownership "
            "or set HOST_UID to the host user."
        )
        raise SystemExit(1)

    require_docker()

    if not check_gh_token():
        raise SystemExit(1)


    prepare_agent_mounts()


def cmd_dev_up() -> int:
    preflight()
    check_image()
    files = AGENT_COMPOSE
    result = run_compose(files, ["up", DEV_SERVICE], with_github=True)
    return result.returncode


def cmd_dev_down() -> int:
    require_docker()
    return run_compose(BASE_COMPOSE, ["down"]).returncode


def cmd_dev_restart() -> int:
    preflight()
    files = AGENT_COMPOSE
    down = run_compose(BASE_COMPOSE, ["down"])
    if down.returncode != 0:
        return down.returncode
    result = run_compose(files, ["up", DEV_SERVICE], with_github=True)
    return result.returncode


def cmd_dev_build() -> int:
    """Rebuild the dev image - the only path that does, mirroring `dev-build`."""
    preflight()
    files = AGENT_COMPOSE
    return run_compose(
        files, ["build", "--build-arg", f"DEV_IMAGE_INPUTS={image_inputs_hash()}", DEV_SERVICE]
    ).returncode


def cmd_dev_log() -> int:
    require_docker()
    return run_compose(BASE_COMPOSE, ["logs", "-f", DEV_SERVICE]).returncode


def cmd_dev_exec() -> int:
    preflight()
    ensure_sandbox_running()
    return run_compose(
        AGENT_COMPOSE,
        ["exec", DEV_SERVICE, "zsh"],
        with_github=True,
    ).returncode


def cmd_agent(name: str, mode: str, extra: list[str]) -> int:
    """Run one agent inside the sandbox, starting the sandbox if needed."""
    preflight()
    check_image()
    ensure_sandbox_running()

    argv = [*AGENTS[name][mode], *extra]
    # Shell-quote so values such as `--model provider/model` survive the zsh -lc layer.
    quoted = " ".join(_shell_quote(part) for part in argv)
    binary = AGENTS[name][mode][0]
    remote = (
        f"cd /workspace && if ! command -v {binary} >/dev/null; then "
        f"echo 'ERROR: {binary} is not installed in this image - add it to "
        f"SANDBOX_AGENTS in the Dockerfile, then run dev-build.' >&2; exit 127; fi; "
        f"exec {quoted}"
    )

    interactive = sys.stdin.isatty()
    exec_flag = "-it" if interactive else "-T"
    return run_compose(
        AGENT_COMPOSE,
        ["exec", exec_flag, DEV_SERVICE, "zsh", "-lc", remote],
        with_github=True,
    ).returncode


def cmd_shell() -> int:
    preflight()
    check_image()
    ensure_sandbox_running()
    log("Dev sandbox shell - run an agent with: opencode | claude | codex")
    return run_compose(AGENT_COMPOSE, ["exec", DEV_SERVICE, "zsh"], with_github=True).returncode


def _sandbox_running() -> bool:
    return _ps_field(AGENT_COMPOSE, DEV_SERVICE, "State") == "running"


def cmd_agents_version() -> int:
    """Each agent's version and login in the sandbox, then the latest releases."""
    require_docker()
    if not _sandbox_running():
        error("the dev sandbox is not running - start it with: dev-up")
        return 1
    status = run_compose(
        AGENT_COMPOSE, ["exec", "-T", DEV_SERVICE, "bash", "/workspace/scripts/agents-status.sh"]
    ).returncode
    print()
    pin = subprocess.run([sys.executable, str(REPO_ROOT / "scripts" / "agents-pin.py"), "show"], check=False)
    return status or pin.returncode


def cmd_agents_upgrade(args: list[str]) -> int:
    """Bump the pins (latest, or --version X.Y.Z for one agent), rebuild, re-create."""
    preflight()
    bump = subprocess.run(  # noqa: S603 - fixed script, validated arguments
        [sys.executable, str(REPO_ROOT / "scripts" / "agents-pin.py"), "bump", *args], check=False
    )
    if bump.returncode != 0:
        return bump.returncode
    built = cmd_dev_build()
    if built != 0:
        return built
    if _sandbox_running():
        log("re-creating the running sandbox on the new image (logins are kept)...")
        up = run_compose(AGENT_COMPOSE, ["up", "-d", "--force-recreate", DEV_SERVICE], with_github=True)
        if up.returncode != 0:
            return up.returncode
        cmd_agents_version()
    log("commit the Dockerfile pin change so the whole project upgrades with you.")
    return 0


def cmd_doctor() -> int:
    """Docker, the image, the token, the sandbox and each agent's login - one look."""
    status = 0

    def report(level: str, text: str) -> None:
        print(f"  {level:<5} {text}")

    docker_ok = bool(shutil.which("docker")) and subprocess.run(
        ["docker", "info"], capture_output=True, check=False
    ).returncode == 0
    if docker_ok:
        report("ok", "Docker is running")
    else:
        report("FAIL", "Docker is not reachable - start Docker Desktop or the docker service")
        return 1
    have = image_label()
    if not have:
        report("--", "no dev image yet - dev-up builds it")
    elif have == image_inputs_hash():
        report("ok", "dev image is current")
    else:
        report("FAIL", "dev image is out of date - dev-build")
        status = 1
    token, _source = resolve_gh_token()
    report("ok" if token else "--", "GitHub token available" if token else "no GitHub token - gh auth login")
    if _sandbox_running():
        report("ok", "sandbox is running")
        status |= run_compose(
            AGENT_COMPOSE, ["exec", "-T", DEV_SERVICE, "bash", "/workspace/scripts/agents-status.sh"]
        ).returncode
    else:
        report("--", "sandbox is not running - dev-up (agent logins are checked while it runs)")
    return status


def _shell_quote(value: str) -> str:
    """POSIX-safe quoting for a value embedded in a `zsh -lc` string."""
    if not value:
        return "''"
    if re.fullmatch(r"[A-Za-z0-9_./:@%+=,-]+", value):
        return value
    return "'" + value.replace("'", "'\"'\"'") + "'"


# ---------------------------------------------------------------------------
# CLI
# ---------------------------------------------------------------------------

HELP = """\
Cross-platform dev sandbox launcher

Usage:
  python scripts/dev-up.py <command> [options]

Commands:
  dev-up                 Build/start the sandbox in the foreground
  dev-down               Stop the sandbox (volumes kept)
  dev-restart            Stop, then up --build
  dev-log                Tail dev container logs
  dev-exec               Shell into the running dev container
  opencode | claude | codex [args...]
                         Run that agent inside the dev container
  <agent>-auto [args...] The same agent in auto mode
  agents-version         Each agent's version and login, and the latest releases
  agents-upgrade [agent...] [--version X.Y.Z]
                         Move the pins to the latest release and rebuild (logins kept)
  doctor                 Check Docker, the image, the token, and every login
  dev-build              Rebuild the dev image (the only command that builds)
  shell                  Interactive shell in the dev container (agent ready)
  help                   Show this message

Environment:
  HOST_UID / HOST_GID    Override the container identity (default: your user,
                         or 1000 on Windows where Docker Desktop virtualises it)
  HOST_HOME              Host home path (default: your home directory)
  HOST_PROJECT_DIR       Host repository path (default: this checkout)
  GH_TOKEN/GITHUB_TOKEN  GitHub credential forwarded into the sandbox

Demo users do not need this script - run `docker compose up`.
The agents are installed in the image and run from every host OS.
"""

COMMANDS = {
    "dev-up": cmd_dev_up,
    "dev-down": cmd_dev_down,
    "dev-restart": cmd_dev_restart,
    "dev-build": cmd_dev_build,
    "dev-log": cmd_dev_log,
    "dev-exec": cmd_dev_exec,
    "shell": cmd_shell,
    "agents-version": cmd_agents_version,
    "doctor": cmd_doctor,
}

# `opencode`, `opencode-auto`, `claude`, ... — every agent, in both modes.
AGENT_COMMANDS = {
    **{name: (name, "plain") for name in AGENTS},
    **{f"{name}-auto": (name, "auto") for name in AGENTS},
}


def main(argv: list[str]) -> int:
    if not argv or argv[0] in ("help", "-h", "--help"):
        print(HELP, end="")
        return 0

    command, *rest = argv
    if command in AGENT_COMMANDS:
        name, mode = AGENT_COMMANDS[command]
        return cmd_agent(name, mode, rest)
    if command == "agents-upgrade":
        return cmd_agents_upgrade(rest)
    if command.endswith("-upgrade") and command[: -len("-upgrade")] in AGENTS:
        return cmd_agents_upgrade([command[: -len("-upgrade")], *rest])
    if command == "image-inputs-hash":
        print(image_inputs_hash())
        return 0
    if command not in COMMANDS:
        error(f"unknown command '{command}'.")
        print(HELP, end="")
        return 2

    handler = COMMANDS[command]
    if rest:
        error(f"'{command}' takes no arguments (got: {' '.join(rest)}).")
        return 2
    return handler()


if __name__ == "__main__":
    raise SystemExit(main(sys.argv[1:]))
