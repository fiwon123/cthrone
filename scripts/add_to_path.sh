#!/bin/sh
# add_to_path.sh — put the folder that holds cthrone and ct on PATH.
#
# The shell that matters is the one you will type `cthrone` into, so this reads
# your login shell from $SHELL and edits the file that shell actually sources.
# Writing to ~/.bashrc on a zsh machine — the macOS default since Catalina —
# puts the line where nothing will ever read it, and the script used to report
# success anyway.
#
# POSIX sh on purpose: bash, zsh, dash and Termux's sh can all run it.

set -eu

SCRIPT_DIR=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)

# The GUI archive ships these next to the binaries. A launcher, a menu entry or
# a desktop shortcut reads Icon=cthrone from the desktop file and resolves it in
# the XDG icon directories — never from the binary — so they have to be
# installed for the app to show its logo instead of a blank square.
APP_PNG=cthrone.png
APP_DESKTOP=cthrone.desktop

say() { printf '%s\n' "$*"; }
warn() { printf 'warning: %s\n' "$*" >&2; }
die() {
    printf 'error: %s\n' "$*" >&2
    exit 1
}

[ -n "${HOME:-}" ] || die "HOME is not set; nothing to edit."

case $SCRIPT_DIR in
*'"'*)
    die "the folder path contains a double quote, which would break the line this script writes.
Add it by hand instead: export PATH=\"\$PATH:$SCRIPT_DIR\""
    ;;
esac

# Which shell the user actually types into.
#
# $SHELL is the login shell and survives `su` and `sudo -s`, but it is empty
# under `env -i`, in a cron job and in some containers, so fall back to the
# password database — which is where $SHELL is set from in the first place.
detect_shell() {
    _shell=${SHELL:-}

    if [ -z "$_shell" ] && command -v getent >/dev/null 2>&1; then
        _shell=$(getent passwd "$(id -un 2>/dev/null || id -u)" 2>/dev/null | cut -d: -f7) || _shell=
    fi

    # macOS has no getent.
    if [ -z "$_shell" ] && command -v dscl >/dev/null 2>&1; then
        _shell=$(dscl . -read "/Users/$(id -un 2>/dev/null || true)" UserShell 2>/dev/null |
            sed -n 's/^UserShell: //p') || _shell=
    fi

    [ -n "$_shell" ] || _shell=/bin/sh
    printf '%s\n' "$_shell"
}

SHELL_PATH=$(detect_shell)
SHELL_NAME=${SHELL_PATH##*/}

RC_FILE=
RC_KIND=posix

case $SHELL_NAME in
zsh)
    # zsh relocates its dotfiles when ZDOTDIR is set; respect it, or the line
    # lands somewhere zsh will never look.
    RC_FILE=${ZDOTDIR:-$HOME}/.zshrc
    ;;
fish)
    RC_FILE=${XDG_CONFIG_HOME:-$HOME/.config}/fish/config.fish
    RC_KIND=fish
    ;;
bash)
    # Interactive bash reads ~/.bashrc, but a macOS login bash reads
    # ~/.bash_profile instead. Prefer the file that already exists there.
    RC_FILE=$HOME/.bashrc
    if [ "$(uname -s 2>/dev/null || true)" = Darwin ] && [ ! -f "$HOME/.bashrc" ] && [ -f "$HOME/.bash_profile" ]; then
        RC_FILE=$HOME/.bash_profile
    fi
    ;;
sh | dash | ash | ksh | mksh | pdksh | oksh)
    # The POSIX shells read $ENV, or ~/.profile for a login shell.
    RC_FILE=$HOME/.profile
    ;;
*)
    RC_FILE=$HOME/.profile
    warn "$SHELL_NAME is not a shell this script knows; writing to $RC_FILE."
    warn "If $SHELL_NAME never reads it, add the line printed below by hand."
    ;;
esac

if [ "$RC_KIND" = fish ]; then
    # fish has no `export`; fish_add_path is idempotent and keeps its ordering.
    RC_LINE="fish_add_path -- \"$SCRIPT_DIR\""
else
    RC_LINE="export PATH=\"$SCRIPT_DIR:\$PATH\""
fi

# The binary may have lost its executable bit in transit: a .zip on Windows, or
# an unzip that does not restore modes.
for bin in cthrone ct; do
    if [ -f "$SCRIPT_DIR/$bin" ]; then
        chmod +x "$SCRIPT_DIR/$bin" 2>/dev/null || true
    fi
done

mkdir -p "$(dirname -- "$RC_FILE")"

say "cthrone folder: $SCRIPT_DIR"
say "login shell:    $SHELL_NAME ($SHELL_PATH)"
say "editing:        $RC_FILE"
say ""

if [ -f "$RC_FILE" ] && grep -Fq -- "$SCRIPT_DIR" "$RC_FILE" 2>/dev/null; then
    # Matched on the folder rather than the whole line, so a copy added earlier
    # in another form is not duplicated.
    say "$RC_FILE already refers to this folder — nothing to add."
else
    {
        printf '\n# cthrone: added by add_to_path.sh\n'
        printf '%s\n' "$RC_LINE"
    } >>"$RC_FILE"
    say "Added:"
    say "  $RC_LINE"
fi

# Install the desktop entry and its icon so a launcher, a menu entry or a
# desktop shortcut shows the logo. Both files come from the GUI archive; the
# CLI-only archive carries the script without them, and then this is skipped
# silently.
install_launcher() {
    DATA_HOME=${XDG_DATA_HOME:-$HOME/.local/share}
    icon_dir=$DATA_HOME/icons/hicolor/512x512/apps
    apps_dir=$DATA_HOME/applications

    mkdir -p "$icon_dir" "$apps_dir"
    cp "$SCRIPT_DIR/$APP_PNG" "$icon_dir/$APP_PNG"
    cp "$SCRIPT_DIR/$APP_DESKTOP" "$apps_dir/$APP_DESKTOP"

    # Refreshing the caches is a nicety, not a requirement: the icon still
    # resolves on the next login when neither tool is installed.
    if command -v gtk-update-icon-cache >/dev/null 2>&1; then
        gtk-update-icon-cache -q -t -f "$DATA_HOME/icons/hicolor" 2>/dev/null || true
    fi
    if command -v update-desktop-database >/dev/null 2>&1; then
        update-desktop-database -q "$apps_dir" 2>/dev/null || true
    fi

    say "launcher icon:  $icon_dir/$APP_PNG"
    say "menu entry:     $apps_dir/$APP_DESKTOP"
}

if [ -f "$SCRIPT_DIR/$APP_PNG" ] && [ -f "$SCRIPT_DIR/$APP_DESKTOP" ] &&
    [ "$(uname -s 2>/dev/null || true)" = Linux ]; then
    install_launcher
fi

say ""
say "Apply it now:   . \"$RC_FILE\""
say "or open a new terminal."


