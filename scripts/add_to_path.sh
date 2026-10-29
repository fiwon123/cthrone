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

# The menu entry and its icon. The GUI archive ships the desktop entry next to
# the binaries, but no icon file: the window icon is not a file at all (the GUI
# embeds the PNG and hands it to the toolkit), and only a launcher needs one on
# disk. This script writes that one from the data further down, so the release
# folder carries no icon files and the menu entry still shows the logo.
APP_PNG=cthrone.png
APP_DESKTOP=cthrone.desktop
# The desktop entry's Exec= target. The archive's `cthrone` is the command-line
# tool: without the `gui` build tag it only prints "GUI not built", which is why
# the entry names this binary instead.
APP_GUI=cthrone_gui

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
for bin in "$APP_GUI" cthrone ct; do
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

# The launcher icon, as a 512x512 PNG in base64. It is the only copy that
# ships, and it ships inside this script rather than beside the binaries.
#
# It is generated from assets/logo-512.png. A test in cmd/assets decodes this
# line and fails when the two disagree, so the blob cannot drift from the file
# the rest of the build uses; the checksum is what the installer itself trusts,
# because a mangled script must not install a blank or wrong logo.
CTHRONE_ICON_PNG_B64='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'
CTHRONE_ICON_PNG_SHA256='3324c806a7124f20cbedfc5957eaf72635661c5ee7a59db7c96e52f459177664'

# checksum prints the sha256 of a file. A machine with no such tool is not an
# error: the install then trusts the decoder alone.
checksum() {
    if command -v sha256sum >/dev/null 2>&1; then
        sha256sum "$1" 2>/dev/null | cut -d' ' -f1

        return 0
    fi

    if command -v shasum >/dev/null 2>&1; then
        shasum -a 256 "$1" 2>/dev/null | cut -d' ' -f1

        return 0
    fi

    if command -v openssl >/dev/null 2>&1; then
        openssl dgst -sha256 -r "$1" 2>/dev/null | cut -d' ' -f1

        return 0
    fi

    return 1
}

# decode_icon writes the embedded PNG to $1. coreutils, BusyBox, BSD and
# OpenSSL spell the decoder differently, so each spelling is tried in turn and
# the first one that produces a file wins — [ -s ] rejects the empty file a
# failed attempt can leave behind.
decode_icon() {
    _out=$1

    if command -v base64 >/dev/null 2>&1; then
        for _flag in -d --decode -D; do
            # shellcheck disable=SC2086 # $_flag is one flag, deliberately unquoted
            if printf '%s' "$CTHRONE_ICON_PNG_B64" | base64 $_flag >"$_out" 2>/dev/null && [ -s "$_out" ]; then
                return 0
            fi
        done
    fi

    if command -v openssl >/dev/null 2>&1; then
        if printf '%s' "$CTHRONE_ICON_PNG_B64" | openssl enc -base64 -d -A >"$_out" 2>/dev/null && [ -s "$_out" ]; then
            return 0
        fi
    fi

    return 1
}

# Install the desktop entry, and write the icon it names, so a launcher, a menu
# entry or a desktop shortcut shows the logo instead of a blank square. The
# entry comes from the GUI archive; a CLI-only archive carries no GUI binary, so
# it installs neither (see the guard below).
install_launcher() {
    DATA_HOME=${XDG_DATA_HOME:-$HOME/.local/share}
    icon_dir=$DATA_HOME/icons/hicolor/512x512/apps
    apps_dir=$DATA_HOME/applications
    icon=$icon_dir/$APP_PNG

    mkdir -p "$icon_dir" "$apps_dir"

    # Written and verified before it is moved into place: a damaged script is a
    # packaging bug, and half-installing a launcher hides it.
    tmp_icon=$icon.tmp.$$
    if ! decode_icon "$tmp_icon"; then
        rm -f "$tmp_icon"
        die "the icon this script carries could not be decoded — re-download the archive."
    fi

    if got=$(checksum "$tmp_icon") && [ -n "$got" ] && [ "$got" != "$CTHRONE_ICON_PNG_SHA256" ]; then
        rm -f "$tmp_icon"
        die "the icon this script decoded does not match its checksum — the script is damaged."
    fi

    mv "$tmp_icon" "$icon"
    cp "$SCRIPT_DIR/$APP_DESKTOP" "$apps_dir/$APP_DESKTOP"

    # Refreshing the caches is a nicety, not a requirement: the icon still
    # resolves on the next login when neither tool is installed.
    if command -v gtk-update-icon-cache >/dev/null 2>&1; then
        gtk-update-icon-cache -q -t -f "$DATA_HOME/icons/hicolor" 2>/dev/null || true
    fi
    if command -v update-desktop-database >/dev/null 2>&1; then
        update-desktop-database -q "$apps_dir" 2>/dev/null || true
    fi

    say "launcher icon:  $icon"
    say "menu entry:     $apps_dir/$APP_DESKTOP"
}

# Only the GUI archive has an entry worth installing: the desktop file names
# cthrone_gui, and a CLI-only archive does not carry it.
if [ -f "$SCRIPT_DIR/$APP_DESKTOP" ] && [ -f "$SCRIPT_DIR/$APP_GUI" ] &&
    [ "$(uname -s 2>/dev/null || true)" = Linux ]; then
    install_launcher
fi

say ""
say "Apply it now:   . \"$RC_FILE\""
say "or open a new terminal."


