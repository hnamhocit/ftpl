#!/bin/sh
# install.sh — one-command installer for ftpl.
#
# Remote (the one-liner):
#   curl -sSf https://raw.githubusercontent.com/hnamhocit/ftpl/main/install.sh | sh
#
# Inside a local checkout (skips clone, builds in place):
#   ./install.sh
#
# Overrides:
#   FTPL_REPO         git repo to clone   (default: https://github.com/hnamhocit/ftpl.git)
#   FTPL_BRANCH       branch to clone     (default: main)
#   FTPL_INSTALL_DIR  install location    (default: ~/.local/bin)
set -eu

REPO="${FTPL_REPO:-https://github.com/hnamhocit/ftpl.git}"
BRANCH="${FTPL_BRANCH:-main}"
BIN=ftpl
DIR="${FTPL_INSTALL_DIR:-$HOME/.local/bin}"

# --- prerequisites (both are runtime deps of ftpl anyway) ---
if ! command -v git >/dev/null 2>&1; then
	echo "error: git not found (ftpl needs it at runtime too). https://git-scm.com/downloads" >&2
	exit 1
fi
if ! command -v go >/dev/null 2>&1; then
	echo "error: Go not found. https://go.dev/doc/install" >&2
	exit 1
fi

# --- temp dir, always cleaned up (even on failure / Ctrl-C) ---
TMP=""
cleanup() {
	if [ -n "$TMP" ]; then
		rm -rf "$TMP"
	fi
	return 0
}
trap cleanup EXIT

# --- get source: local checkout if we're inside one, else clone ---
if [ -f main.go ] && [ -d templates ]; then
	SRC="$PWD"
	echo "==> Detected ftpl checkout in $SRC, building in place..."
else
	TMP="$(mktemp -d)"
	echo "==> Cloning $REPO ($BRANCH)..."
	git clone --depth 1 --branch "$BRANCH" "$REPO" "$TMP/repo"
	SRC="$TMP/repo"
fi

cd "$SRC"
echo "==> Building $BIN..."
go build -o "$BIN" .

# --- install (no sudo by default) ---
mkdir -p "$DIR"
if [ -w "$DIR" ]; then
	install -m 0755 "$BIN" "$DIR/$BIN"
else
	echo "==> $DIR not writable, using sudo..."
	sudo install -m 0755 "$BIN" "$DIR/$BIN"
fi
echo "==> Installed: $DIR/$BIN"

# --- make sure DIR is in PATH ---
case ":$PATH:" in
*":$DIR:"*) ;;
*)
	LINE="export PATH=\"$DIR:\$PATH\""
	RC=""
	if [ -f "$HOME/.zshrc" ]; then
		RC="$HOME/.zshrc"
	elif [ -f "$HOME/.bashrc" ]; then
		RC="$HOME/.bashrc"
	elif [ -f "$HOME/.profile" ]; then
		RC="$HOME/.profile"
	fi
	if [ -n "$RC" ] && ! grep -qF "$DIR" "$RC"; then
		printf '\n# added by ftpl install.sh\n%s\n' "$LINE" >>"$RC"
		echo "==> Added $DIR to PATH in $RC (open a new terminal, or: source $RC)"
	else
		echo "!! Could not detect shell rc. Add this yourself: $LINE"
	fi
	;;
esac

echo
echo "Done. Try: ftpl doctor"
# trap cleanup EXIT tự xóa $TMP ở đây
