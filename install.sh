#!/bin/sh
# Installs Elsewhere (the terminal player) and Melisma (`dhunn`, the engine that chooses what
# plays next), then sets Melisma up: who's listening, and your music analyzed.
#
#   curl -fsSL https://raw.githubusercontent.com/KrishBnsl/elsewhere/main/install.sh | sh
#
# Options (pass after `sh -s --` when piping, e.g. `| sh -s -- --source`):
#   --source           build from source instead of downloading (needs Go 1.25+ and Rust)
#   --bin-dir DIR      where to put the programs (default: ~/.local/bin)
#   --music DIR        the music folder to analyze (default: ~/Music)
#   --name NAME        who's listening (default: asks, or your user name)
#   --no-setup         only install the programs
#   --no-modify-path   don't add the bin folder to your shell's PATH
#   --yes              don't ask; use the defaults
#   --uninstall        remove the two programs (your library and history stay)
#
# Environment: ELSEWHERE_RELEASE_URL overrides where prebuilt programs come from.

set -eu

REPO_ELSEWHERE="https://github.com/KrishBnsl/elsewhere.git"
REPO_MELISMA="https://github.com/KrishBnsl/Melisma.git"
RELEASE_URL="${ELSEWHERE_RELEASE_URL:-https://github.com/KrishBnsl/elsewhere/releases/latest/download}"

BIN_DIR="$HOME/.local/bin"
MUSIC="$HOME/Music"
NAME=""
SOURCE=0
SETUP=1
MODIFY_PATH=1
YES=0
UNINSTALL=0

say() { printf '\033[38;5;214m▸\033[0m %s\n' "$*"; }
note() { printf '  %s\n' "$*"; }
fail() { printf '\033[31m✗\033[0m %s\n' "$*" >&2; exit 1; }

while [ $# -gt 0 ]; do
    case "$1" in
        --source) SOURCE=1 ;;
        --bin-dir) BIN_DIR="${2:?--bin-dir needs a folder}"; shift ;;
        --music) MUSIC="${2:?--music needs a folder}"; shift ;;
        --name) NAME="${2:?--name needs a name}"; shift ;;
        --no-setup) SETUP=0 ;;
        --no-modify-path) MODIFY_PATH=0 ;;
        --yes|-y) YES=1 ;;
        --uninstall) UNINSTALL=1 ;;
        -h|--help) sed -n '2,20p' "$0" 2>/dev/null | sed 's/^# \{0,1\}//'; exit 0 ;;
        *) fail "unknown option: $1 (try --help)" ;;
    esac
    shift
done

# Questions go to the terminal even when this script arrives through a pipe.
ask() { # ask "question" default → answer
    if [ "$YES" -eq 1 ] || ! { [ -r /dev/tty ] && [ -w /dev/tty ]; } || ! (: </dev/tty) 2>/dev/null; then
        printf '%s' "$2"
        return
    fi
    printf '%s ' "$1" >/dev/tty
    read -r answer </dev/tty || answer=""
    printf '%s' "${answer:-$2}"
}
yes_to() { case "$(ask "$1 [Y/n]" y)" in [nN]*) return 1 ;; *) return 0 ;; esac; }

[ "$(uname -s)" = "Darwin" ] || fail "Elsewhere plays through macOS audio, so it needs a Mac."

if [ "$UNINSTALL" -eq 1 ]; then
    for program in elsewhere dhunn; do
        if [ -e "$BIN_DIR/$program" ]; then
            rm -f "$BIN_DIR/$program"
            say "removed $BIN_DIR/$program"
        fi
    done
    note "Your Melisma library and listening history are still in ~/Library/Application Support/dhunn,"
    note "and Elsewhere's likes, playlists and theme in ~/Library/Application Support/Elsewhere."
    exit 0
fi

mkdir -p "$BIN_DIR"
WORK="$(mktemp -d "${TMPDIR:-/tmp}/elsewhere-install.XXXXXX")"
trap 'rm -rf "$WORK"' EXIT

# ---- 1. The programs ----

install_prebuilt() {
    arch="$(uname -m)"
    [ "$arch" = "arm64" ] || return 1
    asset="elsewhere-macos-arm64.tar.gz"
    say "Downloading Elsewhere and Melisma for Apple Silicon"
    curl -fsSL "$RELEASE_URL/$asset" -o "$WORK/$asset" 2>/dev/null || return 1
    if curl -fsSL "$RELEASE_URL/$asset.sha256" -o "$WORK/$asset.sha256" 2>/dev/null; then
        want="$(cut -d' ' -f1 <"$WORK/$asset.sha256")"
        got="$(shasum -a 256 "$WORK/$asset" | cut -d' ' -f1)"
        [ "$want" = "$got" ] || fail "the download is damaged (checksum mismatch); try again"
    fi
    tar -xzf "$WORK/$asset" -C "$WORK"
    [ -x "$WORK/elsewhere" ] && [ -x "$WORK/dhunn" ] || fail "the download is missing a program"
    install -m 755 "$WORK/elsewhere" "$WORK/dhunn" "$BIN_DIR/"
}

install_from_source() {
    say "Building from source"
    missing=""
    command -v git >/dev/null 2>&1 || missing="$missing git (xcode-select --install)"
    command -v go >/dev/null 2>&1 || missing="$missing Go-1.25+ (brew install go, or https://go.dev/dl)"
    command -v cargo >/dev/null 2>&1 || missing="$missing Rust (https://rustup.rs)"
    if [ -n "$missing" ]; then
        fail "building needs:$missing"
    fi
    go_minor="$(go env GOVERSION | sed -E 's/^go1\.([0-9]+).*/\1/')"
    [ "${go_minor:-0}" -ge 25 ] 2>/dev/null || fail "Elsewhere needs Go 1.25 or newer (you have $(go env GOVERSION))"

    note "Elsewhere…"
    git clone -q --depth 1 "$REPO_ELSEWHERE" "$WORK/elsewhere-src"
    (cd "$WORK/elsewhere-src" && go build -trimpath -ldflags='-s -w' -o "$BIN_DIR/elsewhere" .)

    note "Melisma (this takes a few minutes the first time)…"
    git clone -q --depth 1 "$REPO_MELISMA" "$WORK/melisma-src"
    (cd "$WORK/melisma-src" && cargo build --release --locked -q -p dhunn-cli)
    install -m 755 "$WORK/melisma-src/target/release/dhunn" "$BIN_DIR/dhunn"
}

if [ "$SOURCE" -eq 1 ]; then
    install_from_source
elif ! install_prebuilt; then
    say "No ready-made download for this Mac; building from source instead"
    install_from_source
fi
say "Installed elsewhere and dhunn in $BIN_DIR"

# ---- 2. PATH ----

case ":$PATH:" in
    *":$BIN_DIR:"*) ;;
    *)
        line="export PATH=\"$BIN_DIR:\$PATH\""
        case "${SHELL:-}" in
            */zsh) rc="$HOME/.zshrc" ;;
            */bash) rc="$HOME/.bash_profile" ;;
            *) rc="" ;;
        esac
        if [ "$MODIFY_PATH" -eq 1 ] && [ -n "$rc" ]; then
            if ! grep -qsF "$line" "$rc"; then
                printf '\n# Elsewhere and Melisma\n%s\n' "$line" >>"$rc"
                say "Added $BIN_DIR to your PATH in $rc (open a new terminal for it to apply)"
            fi
        else
            say "Add $BIN_DIR to your PATH:  $line"
        fi
        ;;
esac
DHUNN="$BIN_DIR/dhunn"

# ---- 3. Melisma's setup ----

if [ "$SETUP" -eq 1 ]; then
    if "$DHUNN" user list 2>/dev/null | grep -q '^No users yet'; then
        [ -n "$NAME" ] || NAME="$(ask "Who's listening? (a name for your taste profile) [$(id -un)]" "$(id -un)")"
        "$DHUNN" user add "$NAME" >/dev/null
        say "Melisma will learn $NAME's taste"
    fi
    if [ -d "$MUSIC" ]; then
        if yes_to "Let Melisma listen to the music in $MUSIC now? (about a second a song, once)"; then
            "$DHUNN" analyze "$MUSIC" -q
        else
            note "Later: dhunn analyze \"$MUSIC\" (or /analyze inside Elsewhere)"
        fi
    else
        note "No folder at $MUSIC. Later: dhunn analyze <your music folder>"
    fi
fi

# ---- 4. Done ----

printf '\n'
say "Ready. Run:  elsewhere"
note "Type /melisma to let Melisma choose what plays next; /q shows what's coming."
if [ "$MUSIC" != "$HOME/Music" ]; then
    note "Your music is elsewhere: elsewhere \"$MUSIC\""
fi
if [ ! -e /opt/homebrew/opt/game-music-emu/lib/libgme.dylib ] && [ ! -e /usr/local/opt/game-music-emu/lib/libgme.dylib ]; then
    note "Optional: brew install game-music-emu  (for Chiptune Radio)"
fi
