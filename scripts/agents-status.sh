#!/bin/bash
# Each AI agent's version and whether it is signed in — run INSIDE the sandbox
# by `make agents-version` and `make doctor` (and their dev-up.py equivalents).
#
# A login is detected by the agent's own status command, from its EXIT CODE
# only: nothing an agent prints about its account, and no credential file, is
# ever shown. opencode has no status command, so its login is the presence of
# a non-empty credential store, tested without reading it out.
#
# Exits non-zero only when the image is broken (an agent that should be
# installed fails to run). Not being signed in is not a failure: you sign in
# the first time you start the agent, and some agents run free models without
# a login.
set -uo pipefail

status=0
for agent in opencode claude codex; do
    if ! command -v "$agent" >/dev/null 2>&1; then
        printf '  --    %-9s not installed (add it to SANDBOX_AGENTS in the Dockerfile)\n' "$agent"
        continue
    fi
    if ! version="$("$agent" --version 2>/dev/null | head -n 1)" || [ -z "$version" ]; then
        printf '  FAIL  %-9s installed but does not run — make dev-build\n' "$agent"
        status=1
        continue
    fi
    case "$agent" in
        opencode)
            store="$HOME/.local/share/opencode/auth.json"
            # More than "{}" means at least one stored credential.
            if [ -f "$store" ] && [ "$(wc -c < "$store")" -gt 2 ]; then signed=yes; else signed=no; fi ;;
        claude)
            if claude auth status >/dev/null 2>&1; then signed=yes; else signed=no; fi ;;
        codex)
            if codex login status >/dev/null 2>&1; then signed=yes; else signed=no; fi ;;
    esac
    if [ "$signed" = yes ]; then
        printf '  ok    %-9s %s, signed in\n' "$agent" "$version"
    else
        printf '  --    %-9s %s, not signed in (it asks the first time you run make %s)\n' "$agent" "$version" "$agent"
    fi
done
exit "$status"
