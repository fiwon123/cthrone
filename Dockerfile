# syntax=docker/dockerfile:1.6
#
# Dev sandbox image for cthrone.
#
# The base image pins the exact Go version from go.mod, so the sandbox builds
# with the same toolchain CI uses (CI reads the version from go.mod through
# go-version-file). A sandbox on a different Go version is not the same
# environment, and "works on my machine" is what this image exists to remove.

FROM golang:1.26.10-bookworm

# Build-time ARGs are declared here, AFTER the first FROM. An ARG declared
# before FROM is in scope only for the FROM lines themselves — every
# instruction after FROM sees it empty, so a download URL built from an unset
# version 404s at build time in a way no linter catches.
ARG GH_CLI_VERSION=v2.102.0
ARG TARGETARCH
# The host uid/gid, forwarded as build args by docker-compose.yml so the image
# can create a passwd entry + home matching the host user (kills "I have no
# name!"). Defaults keep a standalone `docker build` working.
ARG HOST_UID=1000
ARG HOST_GID=1000

# --- AI coding agents, installed IN the image ---
# The agents run inside the sandbox, from the image, so they behave the same on
# every host OS (a host binary mounted in only works when the host is Linux) and
# never touch the host's own installs or logins.
#
# Pinned on purpose: an upgrade is a reviewed one-line bump here plus
# `make dev-build`, and every developer on the project gets the same version.
# `make agents-version` prints what the running sandbox has.
#
# SANDBOX_AGENTS lists the agents to install. Drop the ones the project does not
# use to save image size; the `make <agent>` target of a dropped agent says so.
ARG SANDBOX_AGENTS="opencode claude codex"
ARG OPENCODE_VERSION=2.2.7
ARG CLAUDE_CODE_VERSION=2.1.325
ARG CODEX_VERSION=0.167.0

RUN apt-get update && apt-get install -y --no-install-recommends \
    build-essential \
    ca-certificates \
    curl \
    git \
    zip \
    zsh \
    && rm -rf /var/lib/apt/lists/*

# `zip` is not in the Go image: `make release` builds the Windows archive with
# `zip -j`, so without it the release target fails inside the sandbox with a
# "command not found" that looks like a Makefile bug.
#
# The font and browser packages the source pattern installs are deliberately
# absent: they exist to render README screenshots captured INSIDE the image,
# and cthrone renders its TUI in the terminal, not in the container.

RUN if [ -z "$TARGETARCH" ]; then \
      case "$(uname -m)" in \
        x86_64|amd64) TARGETARCH=amd64 ;; \
        aarch64|arm64) TARGETARCH=arm64 ;; \
        *) echo "Unsupported architecture: $(uname -m)" >&2; exit 1 ;; \
      esac; \
    fi \
    && curl -fsSL "https://github.com/cli/cli/releases/download/${GH_CLI_VERSION}/gh_${GH_CLI_VERSION#v}_linux_${TARGETARCH}.tar.gz" -o /tmp/gh.tar.gz \
    && tar -xzf /tmp/gh.tar.gz -C /tmp \
    && mv "/tmp/gh_${GH_CLI_VERSION#v}_linux_${TARGETARCH}/bin/gh" /usr/local/bin/gh \
    && rm -rf /tmp/gh.tar.gz "/tmp/gh_${GH_CLI_VERSION#v}_linux_${TARGETARCH}" \
    && gh --version

# --- runtime user (matches the host uid/gid from docker-compose.yml) ---
# The compose service runs as `user: "${HOST_UID}:${HOST_GID}"`. Without a
# passwd entry for that uid (or a home it can write), interactive shells greet
# the user with "I have no name!" and rc files never load. Create devuser if the
# uid is free; if the uid is already claimed by another account, fail the build
# readably instead of silently producing a broken shell.
RUN if ! getent group ${HOST_GID} >/dev/null; then groupadd --gid ${HOST_GID} devuser; fi \
    && if ! getent passwd ${HOST_UID} >/dev/null; then \
         useradd --uid ${HOST_UID} --gid ${HOST_GID} --create-home --shell /usr/bin/zsh devuser; \
       fi \
    && { getent passwd devuser >/dev/null \
         || { echo "ERROR: uid ${HOST_UID} is already used by another account, so 'devuser' cannot be created. Rebuild for a different host uid (make dev-build)." >&2; exit 1; }; } \
    && mkdir -p /home/devuser \
    && chown -R ${HOST_UID}:${HOST_GID} /home/devuser \
    && git config --system --add safe.directory /workspace

# --- the mount points, created in the image ---
#
# Docker creates the DESTINATION of a bind mount or a volume when the image
# does not already contain it, and it creates that path as **root**. The
# runtime user is ${HOST_UID}, not root, so it then cannot write what Docker
# just made for it, and both failures land far from their cause:
#
#   - the agent dies on its first run with
#     `Error: EACCES: permission denied, mkdir '/home/devuser/.cache/opencode'`;
#   - the build fails with `mkdir /go/pkg/mod/cache: permission denied`;
#
# while the container still reports `Up` and the entrypoint still prints its
# "sleeping indefinitely" banner.
#
# Creating them here means Docker finds the path already there and leaves the
# ownership alone. The contents are still the host's, or the volume's.
#
# ADD YOUR OWN MOUNT POINTS HERE: every directory the compose file mounts
# into needs a line in this block, or the runtime user cannot write it.
# The three agent state directories are the named volumes in
# docker-compose.yml: each agent's own login is written there by the agent
# itself, so it survives dev-up, dev-restart and dev-build.
RUN mkdir -p /home/devuser/.cache \
             /home/devuser/.cache/go-build \
             /home/devuser/.config \
             /home/devuser/.config/gh \
             /go/pkg/mod \
             /home/devuser/.local \
             /home/devuser/.local/share \
             /home/devuser/.local/share/opencode \
             /home/devuser/.claude \
             /home/devuser/.codex \
    && chown -R ${HOST_UID}:${HOST_GID} \
         /home/devuser/.cache /home/devuser/.config /go/pkg/mod /home/devuser/.local /home/devuser/.claude /home/devuser/.codex

# Codex is one static binary, installed system-wide as root. Every installer
# below downloads to a FILE first and runs it second: `curl … | bash` runs an
# empty script when the download fails, and the layer "succeeds" with no agent.
RUN case " ${SANDBOX_AGENTS} " in *" codex "*) \
      case "${TARGETARCH:-$(uname -m)}" in \
        amd64|x86_64) CODEX_ARCH=x86_64 ;; \
        arm64|aarch64) CODEX_ARCH=aarch64 ;; \
        *) echo "Unsupported architecture for codex: ${TARGETARCH:-$(uname -m)}" >&2; exit 1 ;; \
      esac \
      && curl -fsSL "https://github.com/openai/codex/releases/download/rust-v${CODEX_VERSION}/codex-${CODEX_ARCH}-unknown-linux-musl.tar.gz" -o /tmp/codex.tar.gz \
      && tar -xzf /tmp/codex.tar.gz -C /tmp \
      && install -m 0755 "/tmp/codex-${CODEX_ARCH}-unknown-linux-musl" /usr/local/bin/codex \
      && rm -f /tmp/codex.tar.gz "/tmp/codex-${CODEX_ARCH}-unknown-linux-musl" \
      && codex --version ;; \
    esac

# --- dev-sandbox rc (badge + terminal title, loaded for BOTH users) ---
# `make dev-shell` opens an interactive *non-login* bash, which reads only
# ~/.bashrc (HOME comes from the compose environment), never /etc/profile.d.
# So the sourcing hook must live in devuser's rc files — and in root's, so
# `docker compose exec -u root bash` debugging keeps the badge too.
COPY scripts/dev-sandbox-rc.sh /etc/profile.d/00-dev-sandbox.sh
RUN chsh -s /usr/bin/zsh root 2>/dev/null || true \
    && printf '%s\n' '[ -f /etc/profile.d/00-dev-sandbox.sh ] && . /etc/profile.d/00-dev-sandbox.sh' >> /root/.zshrc \
    && printf '%s\n' '[ -f /etc/profile.d/00-dev-sandbox.sh ] && . /etc/profile.d/00-dev-sandbox.sh' >> /root/.bashrc \
    && printf '%s\n' '[ -f /etc/profile.d/00-dev-sandbox.sh ] && . /etc/profile.d/00-dev-sandbox.sh' >> /root/.zprofile \
    && printf '%s\n' '[ -f /etc/profile.d/00-dev-sandbox.sh ] && . /etc/profile.d/00-dev-sandbox.sh' >> /home/devuser/.zshrc \
    && printf '%s\n' '[ -f /etc/profile.d/00-dev-sandbox.sh ] && . /etc/profile.d/00-dev-sandbox.sh' >> /home/devuser/.bashrc \
    && printf '%s\n' '[ -f /etc/profile.d/00-dev-sandbox.sh ] && . /etc/profile.d/00-dev-sandbox.sh' >> /home/devuser/.zprofile \
    && chown ${HOST_UID}:${HOST_GID} \
        /home/devuser/.bashrc /home/devuser/.zshrc /home/devuser/.zprofile

# The Go toolchain is already in the base image (golang:1.26.10-bookworm), so
# there is nothing to install here. A tool that must be built from source is
# installed as devuser, not as root: `go install` writes to a root-owned
# directory otherwise, and the runtime user could only read it.
# RUN go install <tool>@latest

# Default to the non-root user so a service added to docker-compose.yml without
# a `user:` directive can't silently reintroduce root-owned workspace files.
# Compose still pins the exact UID/GID via `user:`.
USER devuser

# opencode and Claude Code install into the runtime user's home with their own
# official installers, so they run as devuser (the container's user) and keep
# their binaries out of the state volumes. Auto-update is off: the version is the
# pin above, and an in-container update would vanish with the container anyway.
#
# Every agent is also reachable from `~/.local/bin`: a bash LOGIN shell (`bash
# -lc`) on Debian resets PATH in /etc/profile and only `~/.profile` adds
# `~/.local/bin` back, so a binary found through ENV PATH alone is "not
# installed" there.
#
# CLAUDE_CONFIG_DIR moves `~/.claude.json` (the login and onboarding state) INTO
# `~/.claude`, so one volume holds everything Claude Code needs to stay signed in.
ENV PATH=/home/devuser/.opencode/bin:/home/devuser/.local/bin:${PATH} \
    CLAUDE_CONFIG_DIR=/home/devuser/.claude \
    DISABLE_AUTOUPDATER=1 \
    OPENCODE_DISABLE_AUTOUPDATE=1

RUN case " ${SANDBOX_AGENTS} " in *" opencode "*) \
      curl -fsSL https://opencode.ai/v2/install -o /tmp/opencode-install.sh \
      && bash /tmp/opencode-install.sh --version "${OPENCODE_VERSION}" --no-modify-path \
      && rm -f /tmp/opencode-install.sh \
      && mkdir -p /home/devuser/.local/bin \
      && ln -sf /home/devuser/.opencode/bin/opencode /home/devuser/.local/bin/opencode \
      && opencode --version ;; \
    esac

RUN case " ${SANDBOX_AGENTS} " in *" claude "*) \
      curl -fsSL https://claude.ai/install.sh -o /tmp/claude-install.sh \
      && bash /tmp/claude-install.sh "${CLAUDE_CODE_VERSION}" \
      && rm -rf /tmp/claude-install.sh /home/devuser/.claude/downloads \
      && claude --version ;; \
    esac

WORKDIR /workspace

# --- what this image was built from ---
# `make dev-build` passes a hash of the files the image is built from
# (IMAGE_INPUTS in the Makefile: this Dockerfile and every file it COPYs).
# `dev-up` does not rebuild, so without this an edited Dockerfile — an agent
# pin bump, a toolchain bump — keeps running the OLD image, and the failure
# surfaces later as something else (eznit #322: `go.mod requires go >= X`
# inside the log stream). The entry points compare this label with the files
# on disk and refuse a stale image by name. Declared last, so a new hash
# re-runs only this metadata step, not the layers above.
ARG DEV_IMAGE_INPUTS=unknown
LABEL dev.image-inputs="${DEV_IMAGE_INPUTS}"

CMD ["sleep", "infinity"]
