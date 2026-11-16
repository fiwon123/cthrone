#!/usr/bin/env python3
"""Show and bump the AI agents' pinned versions in the Dockerfile.

The dev image installs opencode, Claude Code and Codex at the versions pinned
in the Dockerfile (``ARG OPENCODE_VERSION=…`` and friends). A pin is what keeps
every developer on the same agent; this script is what keeps the pin from
going stale, because looking up three vendors' latest releases by hand is the
step nobody does.

Usage:
    python scripts/agents-pin.py show
    python scripts/agents-pin.py bump [opencode] [claude] [codex]
    python scripts/agents-pin.py bump claude --version 2.1.300

``show`` prints each pin next to the latest release. ``bump`` rewrites the pins
of the named agents (all of them when none is named) to the latest release, or
to ``--version`` for exactly one agent. It prints ``old -> new`` and exits 0
when nothing changed. It never touches the image: ``make agents-upgrade`` (or
``dev-up.py agents-upgrade``) runs it and then rebuilds.

Each latest version comes from the vendor's own release source — the same one
its installer reads. Standard library only, so it runs wherever Python does.
"""

from __future__ import annotations

import argparse
import json
import re
import sys
import urllib.request
from pathlib import Path

DOCKERFILE = Path(__file__).resolve().parents[1] / "Dockerfile"

# agent -> (Dockerfile ARG, where its latest release is published)
AGENTS = {
    "opencode": ("OPENCODE_VERSION", "https://opencode.ai/update/api/latest/cli/npm"),
    "claude": ("CLAUDE_CODE_VERSION", "https://downloads.claude.ai/claude-code-releases/latest"),
    "codex": ("CODEX_VERSION", "https://api.github.com/repos/openai/codex/releases/latest"),
}

VERSION_RE = re.compile(r"\d+\.\d+\.\d+")


def fetch(url: str) -> str:
    request = urllib.request.Request(url, headers={"User-Agent": "dev-sandbox-agents-pin"})
    with urllib.request.urlopen(request, timeout=20) as response:  # noqa: S310 - fixed https URLs
        return response.read().decode("utf-8").strip()


def latest(agent: str) -> str:
    """The latest release of *agent*, validated as a plain X.Y.Z version."""
    body = fetch(AGENTS[agent][1])
    if agent == "opencode":
        version = json.loads(body)["version"]
    elif agent == "codex":
        version = json.loads(body)["tag_name"].removeprefix("rust-v")
    else:
        version = body
    if not VERSION_RE.fullmatch(version):
        raise ValueError(f"{agent}: the release source answered {version!r}, not a version")
    return version


def pinned(text: str, arg: str) -> str | None:
    match = re.search(rf"^ARG {arg}=(\S+)\s*$", text, re.M)
    return match.group(1) if match else None


def cmd_show() -> int:
    text = DOCKERFILE.read_text()
    status = 0
    for agent, (arg, _url) in AGENTS.items():
        pin = pinned(text, arg) or "not pinned"
        try:
            newest = latest(agent)
        except (OSError, ValueError, KeyError) as exc:
            newest, status = f"unknown ({exc})", 1
        note = "" if newest == pin else "  <- make " + agent + "-upgrade"
        print(f"{agent:<9} pinned {pin:<10} latest {newest}{note}")
    return status


def cmd_bump(agents: list[str], version: str | None) -> int:
    if version is not None:
        if len(agents) != 1:
            print("ERROR: --version pins exactly one agent: bump <agent> --version X.Y.Z", file=sys.stderr)
            return 2
        if not VERSION_RE.fullmatch(version):
            print(f"ERROR: {version!r} is not a version (expected X.Y.Z)", file=sys.stderr)
            return 2
    text = DOCKERFILE.read_text()
    for agent in agents or list(AGENTS):
        arg = AGENTS[agent][0]
        old = pinned(text, arg)
        if old is None:
            print(f"ERROR: {DOCKERFILE.name} has no `ARG {arg}=` line to bump", file=sys.stderr)
            return 1
        try:
            new = version or latest(agent)
        except (OSError, ValueError, KeyError) as exc:
            print(f"ERROR: could not resolve the latest {agent}: {exc}", file=sys.stderr)
            return 1
        text = re.sub(rf"^ARG {arg}=\S+", f"ARG {arg}={new}", text, count=1, flags=re.M)
        print(f"{agent:<9} {old} -> {new}" + ("" if old != new else " (already current)"))
    DOCKERFILE.write_text(text)
    return 0


def main(argv: list[str]) -> int:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    sub = parser.add_subparsers(dest="command", required=True)
    sub.add_parser("show", help="print each pin next to the latest release")
    bump = sub.add_parser("bump", help="rewrite pins to the latest release (or --version)")
    bump.add_argument("agents", nargs="*", metavar="agent", help=" | ".join(AGENTS))
    bump.add_argument("--version", help="pin exactly this version (one agent only)")
    args = parser.parse_args(argv)
    if args.command == "show":
        return cmd_show()
    unknown = [a for a in args.agents if a not in AGENTS]
    if unknown:
        parser.error(f"unknown agent {unknown[0]!r} (choose from {', '.join(AGENTS)})")
    return cmd_bump(args.agents, args.version)


if __name__ == "__main__":
    raise SystemExit(main(sys.argv[1:]))
