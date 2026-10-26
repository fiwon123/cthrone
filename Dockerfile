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
RUN mkdir -p /home/devuser/.cache /home/devuser/.cache/go-build \
             /home/devuser/.config /home/devuser/.config/gh \
             /home/devuser/.config/opencode \
             /go/pkg/mod \
    && chown -R ${HOST_UID}:${HOST_GID} /home/devuser/.cache /home/devuser/.config /go/pkg/mod

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

WORKDIR /workspace

CMD ["sleep", "infinity"]
