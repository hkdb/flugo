#!/bin/sh
#
# Uninstaller for a flugo app installed from its tarball. Removes whichever
# scope(s) it finds — per-user (~/.local) and/or system-wide (/opt + /usr/local).
# Run from inside the extracted tarball directory:
#
#   ./uninstall.sh
#
set -eu

HERE="$(cd "$(dirname "$0")" && pwd)"

if [ ! -f "$HERE/app.env" ]; then
  echo "app.env not found next to uninstall.sh — extract the full tarball and run it from there." >&2
  exit 1
fi
# shellcheck disable=SC1091
. "$HERE/app.env"     # provides APP_NAME, APP_ID, BIN_NAME, SLUG

if [ -t 1 ]; then
  B="$(printf '\033[1m')"; D="$(printf '\033[2m')"; R="$(printf '\033[0m')"
else
  B=''; D=''; R=''
fi
say()  { printf '%s\n' "$*"; }
info() { printf '%s\n' "${D}$*${R}"; }

ask() {
  _ans=''
  if { true >/dev/tty; } 2>/dev/null; then
    printf '%s' "$1" >/dev/tty
    IFS= read -r _ans </dev/tty 2>/dev/null || _ans=''
  fi
  [ -n "$_ans" ] && printf '%s' "$_ans" || printf '%s' "$2"
}

# remove_scope PREFIX BINDIR APPDIR ICONROOT SUDO
remove_scope() {
  _p="$1"; _b="$2"; _a="$3"; _i="$4"; _s="$5"
  $_s rm -rf "$_p"
  $_s rm -f "$_b/$SLUG"
  $_s rm -f "$_a/$APP_ID.desktop"
  for d in "$_i"/*/apps; do
    [ -d "$d" ] || continue
    $_s rm -f "$d/$APP_ID.png" "$d/$APP_ID.svg"
  done
  # Undo any PATH entry the installer added.
  if [ "$_b" = "$HOME/.local/bin" ]; then
    for rc in "$HOME/.zshrc" "$HOME/.bashrc" "$HOME/.profile" "$HOME/.config/fish/config.fish"; do
      [ -f "$rc" ] || continue
      if grep -q "# Added by $SLUG installer" "$rc" 2>/dev/null; then
        _t="$(mktemp)"
        sed "/# Added by $SLUG installer/,+1d" "$rc" > "$_t" && cat "$_t" > "$rc"
        rm -f "$_t"
      fi
    done
  else
    $_s rm -f "/etc/profile.d/$SLUG.sh"
  fi
  if command -v update-desktop-database >/dev/null 2>&1; then
    $_s update-desktop-database "$_a" 2>/dev/null || true
  fi
  if command -v gtk-update-icon-cache >/dev/null 2>&1; then
    $_s gtk-update-icon-cache -f "$_i" 2>/dev/null || true
  fi
}

say ""
say "${B}Uninstall ${APP_NAME}${R}"
say ""

found=0

# --- per-user --------------------------------------------------------------
if [ -d "$HOME/.local/share/$SLUG" ] || [ -f "$HOME/.local/bin/$SLUG" ]; then
  found=1
  case "$(ask "Remove the per-user install? [Y/n]: " "Y")" in
    n|N|no|NO) say "  Skipped per-user." ;;
    *)
      remove_scope "$HOME/.local/share/$SLUG" "$HOME/.local/bin" \
        "$HOME/.local/share/applications" "$HOME/.local/share/icons/hicolor" ""
      say "  Removed per-user install."
      ;;
  esac
fi

# --- system-wide -----------------------------------------------------------
if [ -d "/opt/$SLUG" ] || [ -f "/usr/local/bin/$SLUG" ]; then
  found=1
  SUDO=''
  if [ "$(id -u)" -ne 0 ] && command -v sudo >/dev/null 2>&1; then
    SUDO="sudo"
  fi
  case "$(ask "Remove the system-wide install? [Y/n]: " "Y")" in
    n|N|no|NO) say "  Skipped system-wide." ;;
    *)
      remove_scope "/opt/$SLUG" "/usr/local/bin" \
        "/usr/share/applications" "/usr/share/icons/hicolor" "$SUDO"
      say "  Removed system-wide install."
      ;;
  esac
fi

[ "$found" -eq 0 ] && say "  No ${APP_NAME} install found."
say ""
