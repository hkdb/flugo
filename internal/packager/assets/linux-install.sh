#!/bin/sh
#
# Installer for a flugo app distributed as a tarball. Generic — all per-app
# values come from the sibling app.env. Run this from inside the extracted
# tarball directory:
#
#   ./install.sh
#
set -eu

HERE="$(cd "$(dirname "$0")" && pwd)"

if [ ! -f "$HERE/app.env" ]; then
  echo "app.env not found next to install.sh — extract the full tarball and run it from there." >&2
  exit 1
fi
# shellcheck disable=SC1091
. "$HERE/app.env"     # provides APP_NAME, APP_ID, BIN_NAME, SLUG

# --- decoration ------------------------------------------------------------
if [ -t 1 ]; then
  B="$(printf '\033[1m')"; D="$(printf '\033[2m')"; R="$(printf '\033[0m')"
else
  B=''; D=''; R=''
fi
say()  { printf '%s\n' "$*"; }
info() { printf '%s\n' "${D}$*${R}"; }
err()  { printf '%s\n' "$*" >&2; }

# Read an answer from the controlling terminal even when stdin is a pipe
# (curl | sh); fall back to the default when there's no tty.
ask() {
  _ans=''
  if { true >/dev/tty; } 2>/dev/null; then
    printf '%s' "$1" >/dev/tty
    IFS= read -r _ans </dev/tty 2>/dev/null || _ans=''
  fi
  [ -n "$_ans" ] && printf '%s' "$_ans" || printf '%s' "$2"
}

PATH_MODIFIED=0

# ensure_on_path DIR SUDO — add DIR to the user's PATH if it isn't already there,
# setting PATH_MODIFIED=1 when it changes anything.
ensure_on_path() {
  _dir="$1"; _s="$2"
  case ":$PATH:" in
    *":$_dir:"*) return 0 ;;     # already on PATH — nothing to do
  esac

  if [ "$_dir" = "$HOME/.local/bin" ]; then
    # Per-user: append to the matching shell profile with a removable marker.
    _shell="$(basename "${SHELL:-sh}")"
    case "$_shell" in
      zsh)  _rc="$HOME/.zshrc" ;;
      bash) _rc="$HOME/.bashrc" ;;
      fish) _rc="$HOME/.config/fish/config.fish" ;;
      *)    _rc="$HOME/.profile" ;;
    esac
    mkdir -p "$(dirname "$_rc")"
    if [ "$_shell" = "fish" ]; then
      printf '\n# Added by %s installer\nfish_add_path "%s"\n' "$SLUG" "$_dir" >> "$_rc"
    else
      printf '\n# Added by %s installer\nexport PATH="%s:$PATH"\n' "$SLUG" "$_dir" >> "$_rc"
    fi
    info "Added ${_dir} to your PATH in ${_rc}."
    PATH_MODIFIED=1
  else
    # System-wide: a profile.d snippet (only reached when it's genuinely missing).
    _pd="/etc/profile.d/${SLUG}.sh"
    if printf 'export PATH="%s:$PATH"\n' "$_dir" | $_s tee "$_pd" >/dev/null 2>&1; then
      info "Added ${_dir} to the system PATH via ${_pd}."
      PATH_MODIFIED=1
    else
      info "Note: ${_dir} isn't on your PATH — add it to run '${SLUG}' from a terminal."
    fi
  fi
}

say ""
say "${B}Install ${APP_NAME}${R}"
say ""
say "  ${B}1${R}) Per-user     (~/.local — no admin required)"
say "  ${B}2${R}) System-wide  (/opt + /usr/local — may prompt for sudo)"
choice="$(ask "Choose [1/2] (default 1): " "1")"
say ""

SUDO=''
case "$choice" in
  2)
    PREFIX="/opt/$SLUG"
    BINDIR="/usr/local/bin"
    APPDIR="/usr/share/applications"
    ICONROOT="/usr/share/icons/hicolor"
    if [ "$(id -u)" -ne 0 ]; then
      if command -v sudo >/dev/null 2>&1; then
        SUDO="sudo"
        info "(elevated permissions required — you may be prompted for your password)"
      else
        err "System-wide install needs root and 'sudo' is unavailable. Re-run as root, or choose per-user."
        exit 1
      fi
    fi
    ;;
  *)
    PREFIX="$HOME/.local/share/$SLUG"
    BINDIR="$HOME/.local/bin"
    APPDIR="$HOME/.local/share/applications"
    ICONROOT="$HOME/.local/share/icons/hicolor"
    ;;
esac

info "Installing ${APP_NAME} to ${PREFIX}"

# --- 1. app payload --------------------------------------------------------
$SUDO rm -rf "$PREFIX"
$SUDO mkdir -p "$PREFIX"
$SUDO cp -a "$HERE/app/." "$PREFIX/"

# --- 2. launcher wrapper (sets LD_LIBRARY_PATH like the AppImage AppRun) ----
LAUNCHER="$BINDIR/$SLUG"
tmpL="$(mktemp)"
cat > "$tmpL" <<EOF
#!/bin/sh
export LD_LIBRARY_PATH="$PREFIX/lib:\${LD_LIBRARY_PATH:-}"
exec "$PREFIX/$BIN_NAME" "\$@"
EOF
chmod 0755 "$tmpL"
$SUDO mkdir -p "$BINDIR"
$SUDO cp "$tmpL" "$LAUNCHER"
rm -f "$tmpL"

# --- 3. desktop entry (Exec/TryExec rewritten to the installed launcher) ----
DESKTOP_SRC="$HERE/$APP_ID.desktop"
if [ -f "$DESKTOP_SRC" ]; then
  $SUDO mkdir -p "$APPDIR"
  tmpD="$(mktemp)"
  # Replace the command in Exec= (preserving trailing field codes like %u) and
  # point TryExec= at the launcher, adding it if the source lacked one.
  sed -e "s|^Exec=[^ ]*|Exec=$LAUNCHER|" \
      -e "s|^TryExec=.*|TryExec=$LAUNCHER|" "$DESKTOP_SRC" > "$tmpD"
  grep -q '^TryExec=' "$tmpD" || printf 'TryExec=%s\n' "$LAUNCHER" >> "$tmpD"
  $SUDO cp "$tmpD" "$APPDIR/$APP_ID.desktop"
  rm -f "$tmpD"
fi

# --- 4. icons --------------------------------------------------------------
if [ -d "$HERE/icons" ]; then
  for png in "$HERE"/icons/icon-*.png; do
    [ -f "$png" ] || continue
    sz="$(basename "$png" | sed -n 's/^icon-\([0-9]*\)x[0-9]*\.png$/\1/p')"
    [ -n "$sz" ] || continue
    $SUDO mkdir -p "$ICONROOT/${sz}x${sz}/apps"
    $SUDO cp "$png" "$ICONROOT/${sz}x${sz}/apps/$APP_ID.png"
  done
  if [ -f "$HERE/icons/icon.svg" ]; then
    $SUDO mkdir -p "$ICONROOT/scalable/apps"
    $SUDO cp "$HERE/icons/icon.svg" "$ICONROOT/scalable/apps/$APP_ID.svg"
  fi
fi

# --- 5. refresh desktop/icon caches (best effort) --------------------------
if command -v update-desktop-database >/dev/null 2>&1; then
  $SUDO update-desktop-database "$APPDIR" 2>/dev/null || true
fi
if command -v gtk-update-icon-cache >/dev/null 2>&1; then
  $SUDO gtk-update-icon-cache -f "$ICONROOT" 2>/dev/null || true
fi

# --- 6. make sure the launcher's dir is on PATH ----------------------------
ensure_on_path "$BINDIR" "$SUDO"

say ""
say "${B}${APP_NAME} installed.${R}"
say "  Launch it from your application menu, or run: ${B}${SLUG}${R}"
if [ "$PATH_MODIFIED" -eq 1 ]; then
  say ""
  say "${B}One more step:${R} log out and log back in before launching ${APP_NAME},"
  say "so your session picks up the updated PATH."
fi
say ""
