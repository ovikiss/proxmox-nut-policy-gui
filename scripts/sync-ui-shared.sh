#!/usr/bin/env sh
set -eu

ROOT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
SHARED_TMP_DIR=""

cleanup() {
  if [ -n "$SHARED_TMP_DIR" ] && [ -d "$SHARED_TMP_DIR" ]; then
    rm -rf "$SHARED_TMP_DIR"
  fi
}
trap cleanup EXIT INT TERM

if [ -n "${UI_SHARED_DIR:-}" ] && [ -d "${UI_SHARED_DIR}" ]; then
  SHARED_DIR="${UI_SHARED_DIR}"
else
  SHARED_TMP_DIR="$(mktemp -d)"
  git clone --depth 1 --branch "${UI_SHARED_REF:-main}" "${UI_SHARED_REPO}" "$SHARED_TMP_DIR" >/dev/null 2>&1
  if [ -n "${UI_SHARED_REV:-}" ]; then
    git -C "$SHARED_TMP_DIR" fetch --depth 1 origin "$UI_SHARED_REV" >/dev/null 2>&1
    git -C "$SHARED_TMP_DIR" checkout --detach "$UI_SHARED_REV" >/dev/null 2>&1
  fi
  SHARED_DIR="$SHARED_TMP_DIR"
fi

mkdir -p "$ROOT_DIR/static"
cp -R "$SHARED_DIR/ui/common" "$ROOT_DIR/static/common"
cp -R "$SHARED_DIR/ui/images" "$ROOT_DIR/static/images"
cp -R "$SHARED_DIR/ui/i18n" "$ROOT_DIR/static/i18n"
cp "$SHARED_DIR/ui/css/style-modern.css" "$ROOT_DIR/static/styles-modern.css"
echo "Synced shared UI from $SHARED_DIR"
