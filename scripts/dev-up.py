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


def opencode_binary() -> Path:
    override = os.environ.get("OPENCODE_BIN")
    return Path(override) if override else host_home() / ".opencode" / "bin" / "opencode"


def is_linux_binary(path: Path) -> bool:
    """True when *path* is an ELF executable.

    The dev container is Linux. A Windows ``.exe``/``.cmd`` or a macOS Mach-O
    binary cannot execute there, so mounting one would only fail later with
    ``exec format error``. The ELF magic check is what makes "the agent is
    available" mean something true on every platform (including WSL2, whose
    host filesystem carries the Linux binary).
    """
    try:
        with path.open("rb") as handle:
            return handle.read(4) == b"\x7fELF"
    except OSError:
        return False


def agent_available() -> bool:
    return is_linux_binary(opencode_binary())


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
    """Create the read-only mount sources Compose requires.

    A missing bind-mount source is fatal on Windows and creates root-owned
    directories on Linux, so these are made before Compose starts. No-op when
    the agent is unavailable.
    """
    if not agent_available():
        return
    home = host_home()
    for directory in (home / ".config" / "opencode", home / ".config" / "gh"):
        directory.mkdir(parents=True, exist_ok=True)
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
        files, ["ps", "--all", "--format", f"{{{{{field}}}}}", service], capture=True
    )
    if result.returncode != 0:
        return ""
    lines = (result.stdout or "").strip().splitlines()
    return lines[0].strip() if lines else ""


def ensure_sandbox_running(*, agent: bool) -> None:
    """Bring the stack up without gratuitously rebuilding an existing image.

    Mirrors the Makefile's intent: build only when the sandbox was never
    initialized; otherwise reconcile and wait for readiness. A running stack is
    left completely untouched so a live `opencode` session is not clobbered.
    """
    files = AGENT_COMPOSE if agent else BASE_COMPOSE
    state = _ps_field(files, DEV_SERVICE, "State")

    if not state:
        log("dev is not initialized yet - building and starting the stack (this runs once).")
        run_compose(files, ["up", "-d", "--build", DEV_SERVICE])
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
# Commands
# ---------------------------------------------------------------------------


def preflight(*, require_agent: bool) -> None:
    if IS_POSIX and os.getuid() == 0:
        error(
            "refusing to run the dev sandbox as UID 0; repair workspace ownership "
            "or set HOST_UID to the host user."
        )
        raise SystemExit(1)

    require_docker()

    if not check_gh_token():
        raise SystemExit(1)

    if require_agent and not agent_available():
        error(f"the in-container AI agent needs a Linux opencode binary at {opencode_binary()}.")
        if IS_WINDOWS:
            error("  A Windows opencode install cannot execute inside the Linux container.")
            error("  Recommended: use WSL2, where the host binary is Linux.")
        elif sys.platform == "darwin":
            error("  A macOS opencode binary cannot execute inside the Linux container.")
        error("  Alternative: run 'opencode' on the host against the running stack.")
        raise SystemExit(1)

    prepare_agent_mounts()


def agent_note() -> None:
    if agent_available():
        log("sandboxed agent available - merging docker-compose.agent.yml.")
    else:
        log(f"in-container agent unavailable (no Linux binary at {opencode_binary()}).")
        log("  Starting the application without it; run opencode on the host if needed.")


def cmd_dev_up() -> int:
    preflight(require_agent=False)
    agent_note()
    files = AGENT_COMPOSE if agent_available() else BASE_COMPOSE
    result = run_compose(files, ["up", "--build", DEV_SERVICE], with_github=True)
    return result.returncode


def cmd_dev_down() -> int:
    require_docker()
    return run_compose(BASE_COMPOSE, ["down"]).returncode


def cmd_dev_restart() -> int:
    preflight(require_agent=False)
    agent_note()
    files = AGENT_COMPOSE if agent_available() else BASE_COMPOSE
    down = run_compose(BASE_COMPOSE, ["down"])
    if down.returncode != 0:
        return down.returncode
    result = run_compose(files, ["up", "--build", DEV_SERVICE], with_github=True)
    return result.returncode


def cmd_dev_log() -> int:
    require_docker()
    return run_compose(BASE_COMPOSE, ["logs", "-f", DEV_SERVICE]).returncode


def cmd_dev_exec() -> int:
    preflight(require_agent=False)
    ensure_sandbox_running(agent=agent_available())
    return run_compose(
        AGENT_COMPOSE if agent_available() else BASE_COMPOSE,
        ["exec", DEV_SERVICE, "zsh"],
        with_github=True,
    ).returncode


def _opencode_args(extra: list[str]) -> list[str]:
    # Default matches `make opencode`: auto-approve prompts (trusted sandbox).
    return extra if extra else ["--auto"]


def cmd_opencode(extra: list[str]) -> int:
    preflight(require_agent=True)
    ensure_sandbox_running(agent=True)

    # Shell-quote so values such as `-m provider/model` survive the zsh -lc layer.
    quoted = " ".join(_shell_quote(part) for part in _opencode_args(extra))
    remote = f"cd /workspace && opencode {quoted}".strip()

    interactive = sys.stdin.isatty()
    exec_flag = "-it" if interactive else "-T"
    return run_compose(
        AGENT_COMPOSE,
        ["exec", exec_flag, DEV_SERVICE, "zsh", "-lc", remote],
        with_github=True,
    ).returncode


def cmd_shell() -> int:
    preflight(require_agent=True)
    ensure_sandbox_running(agent=True)
    log("Dev sandbox shell - run the AI coding agent with:")
    log("  cd /workspace && opencode")
    return run_compose(AGENT_COMPOSE, ["exec", DEV_SERVICE, "zsh"], with_github=True).returncode


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
  opencode [args...]     Run the AI agent inside the dev container
                         (default args: --auto)
  shell                  Interactive shell in the dev container (agent ready)
  help                   Show this message

Environment:
  HOST_UID / HOST_GID    Override the container identity (default: your user,
                         or 1000 on Windows where Docker Desktop virtualises it)
  HOST_HOME              Host home path (default: your home directory)
  HOST_PROJECT_DIR       Host repository path (default: this checkout)
  OPENCODE_BIN           Path to a Linux opencode binary
  GH_TOKEN/GITHUB_TOKEN  GitHub credential forwarded into the sandbox

Demo users do not need this script - run `docker compose up`.
The in-container AI agent needs a Linux opencode binary (Linux/WSL2); on
macOS and native Windows, run `opencode` on the host instead.
"""

COMMANDS = {
    "dev-up": cmd_dev_up,
    "dev-down": cmd_dev_down,
    "dev-restart": cmd_dev_restart,
    "dev-log": cmd_dev_log,
    "dev-exec": cmd_dev_exec,
    "opencode": cmd_opencode,
    "shell": cmd_shell,
}


def main(argv: list[str]) -> int:
    if not argv or argv[0] in ("help", "-h", "--help"):
        print(HELP, end="")
        return 0

    command, *rest = argv
    if command not in COMMANDS:
        error(f"unknown command '{command}'.")
        print(HELP, end="")
        return 2

    handler = COMMANDS[command]
    if command == "opencode":
        return handler(rest)
    if rest:
        error(f"'{command}' takes no arguments (got: {' '.join(rest)}).")
        return 2
    return handler()


if __name__ == "__main__":
    raise SystemExit(main(sys.argv[1:]))
