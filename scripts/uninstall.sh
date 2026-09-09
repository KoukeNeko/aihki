#!/usr/bin/env sh
# Remove the Taiga CLI from macOS or Linux.
#
# The binary goes by default. Configuration and stored credentials stay unless
# --purge is given, because uninstalling is often one step of an upgrade and
# silently discarding a login would be a poor trade to make on the user's
# behalf.
#
#   curl -fsSL https://raw.githubusercontent.com/KoukeNeko/taiga-cli/main/scripts/uninstall.sh | sh
#
# Options:
#   --purge      also remove configuration, the completion cache and credentials
#   --dry-run    report what would be removed and change nothing
#
# Environment:
#   TAIGA_INSTALL_DIR  directory to remove the binary from (default: $HOME/.local/bin)
set -eu

BINARY=taiga
LEGACY_KEYRING_SERVICE=taiga-cli

install_dir=${TAIGA_INSTALL_DIR:-$HOME/.local/bin}
purge=no
dry_run=no

log() { printf '%s\n' "$*"; }
warn() { printf 'uninstall: %s\n' "$*" >&2; }
fail() {
    warn "$*"
    exit 1
}

for argument in "$@"; do
    case "$argument" in
        --purge) purge=yes ;;
        --dry-run) dry_run=yes ;;
        -h | --help)
            sed -n '2,18p' "$0" | sed 's/^# \{0,1\}//'
            exit 0
            ;;
        *) fail "unknown option $argument" ;;
    esac
done

remove() {
    target=$1
    [ -e "$target" ] || return 0
    if [ "$dry_run" = yes ]; then
        log "would remove $target"
        return 0
    fi
    rm -rf "$target"
    log "removed $target"
}

# Deleting files Homebrew owns would leave its metadata claiming taiga is still
# installed, so hand the user back to brew rather than corrupting its state.
check_homebrew() {
    command -v brew >/dev/null 2>&1 || return 0
    installed=$(brew list --formula 2>/dev/null | grep -x "$BINARY" || true)
    [ -n "$installed" ] || return 0
    log "$BINARY was installed with Homebrew. Remove it with:"
    log ""
    log "    brew uninstall $BINARY"
    log ""
    log "Then re-run this script with --purge to drop configuration and credentials."
    [ "$purge" = yes ] || exit 0
}

config_directory() {
    case "$(uname -s)" in
        Darwin) printf '%s' "$HOME/Library/Application Support" ;;
        *) printf '%s' "${XDG_CONFIG_HOME:-$HOME/.config}" ;;
    esac
}

# Credentials live in the OS keyring rather than a file, so they need the
# platform's own tool. Report what could not be reached instead of implying a
# clean sweep.
remove_credentials() {
    if [ "$dry_run" = yes ]; then
        log "would remove keyring entries for services $BINARY and $LEGACY_KEYRING_SERVICE"
        return 0
    fi
    case "$(uname -s)" in
        Darwin)
            for service in "$BINARY" "$LEGACY_KEYRING_SERVICE"; do
                while security delete-generic-password -s "$service" >/dev/null 2>&1; do
                    log "removed a keyring entry for $service"
                done
            done
            ;;
        *)
            if command -v secret-tool >/dev/null 2>&1; then
                for service in "$BINARY" "$LEGACY_KEYRING_SERVICE"; do
                    secret-tool clear service "$service" >/dev/null 2>&1 || true
                done
                log "cleared libsecret entries for $BINARY and $LEGACY_KEYRING_SERVICE"
            else
                warn "secret-tool is not installed, so stored credentials were left in the keyring"
            fi
            ;;
    esac
}

# Completions are removed from the same directories the installer writes to,
# and only when the file is one this tool wrote, so a hand-made completion of
# the same name is never destroyed.
remove_completions() {
    zsh_dirs="${HOMEBREW_PREFIX:-/opt/homebrew}/share/zsh/site-functions /usr/local/share/zsh/site-functions $HOME/.zsh/completions"
    bash_dirs="${HOMEBREW_PREFIX:-/opt/homebrew}/etc/bash_completion.d $HOME/.local/share/bash-completion/completions /usr/local/etc/bash_completion.d"
    fish_dirs="${XDG_CONFIG_HOME:-$HOME/.config}/fish/completions"
    for directory in $zsh_dirs; do remove_completion_file "$directory/_$BINARY"; done
    for directory in $bash_dirs; do remove_completion_file "$directory/$BINARY"; done
    for directory in $fish_dirs; do remove_completion_file "$directory/$BINARY.fish"; done
}

remove_completion_file() {
    target=$1
    [ -f "$target" ] || return 0
    # Generated completions name the command they complete; anything else is
    # somebody's own file and is left alone.
    grep -q "$BINARY" "$target" 2>/dev/null || return 0
    remove "$target"
}

main() {
    check_homebrew

    remove "$install_dir/$BINARY"
    remove_completions

    if [ "$purge" = yes ]; then
        base=$(config_directory)
        remove "$base/$BINARY"
        # The pre-rename directory is removed too, since a purge that leaves the
        # old profile behind would silently resurrect it on the next install.
        remove "$base/$LEGACY_KEYRING_SERVICE"
        remove_credentials
        log ""
        log "A repository pinned with \`$BINARY project use --local\` keeps its setting in"
        log "its own .git/config. Clear it there with:"
        log ""
        log "    git config --local --remove-section $BINARY"
    else
        log ""
        log "Configuration and credentials were kept. Pass --purge to remove them too."
    fi
}

main
