#!/usr/bin/env bash
# Fill services/internal/steamugc/sdk from the SDK zip or steamworks-165.tar.gpg.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DEST="$ROOT/services/internal/steamugc/sdk"
STAMP="$ROOT/services/internal/steamugc/steam.stamp"
GPG_BLOB="$ROOT/services/internal/steamugc/steamworks-165.tar.gpg"
VERSION=165
DEFAULT_ZIP="${STEAMWORKS_SDK_ZIP:-$HOME/Downloads/steamworks_sdk_165.zip}"
# Windows Git Bash: also try the documented Downloads path.
if [[ ! -f "$DEFAULT_ZIP" && -f "/c/Users/idhis/Downloads/steamworks_sdk_165.zip" ]]; then
  DEFAULT_ZIP="/c/Users/idhis/Downloads/steamworks_sdk_165.zip"
fi

if [[ -f "$DEST/win64/steam_api64.dll" && -f "$DEST/linux64/libsteam_api.so" &&
      -f "$STAMP" &&
      "$(cat "$STAMP")" == "$VERSION" ]]; then
  echo "steamworks redistributable already $VERSION"
  exit 0
fi

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
mkdir -p "$tmp/win64" "$tmp/linux64"

copy_from_sdk() {
  local root="$1"
  cp "$root/sdk/redistributable_bin/win64/steam_api64.dll" "$tmp/win64/" || \
    cp "$root/redistributable_bin/win64/steam_api64.dll" "$tmp/win64/"
  cp "$root/sdk/redistributable_bin/linux64/libsteam_api.so" "$tmp/linux64/" || \
    cp "$root/redistributable_bin/linux64/libsteam_api.so" "$tmp/linux64/"
}

if [[ -n "${STEAMWORKS_SDK:-}" && -d "$STEAMWORKS_SDK" ]]; then
  copy_from_sdk "$STEAMWORKS_SDK"
elif [[ -f "${STEAMWORKS_SDK_ZIP:-$DEFAULT_ZIP}" ]]; then
  zip="${STEAMWORKS_SDK_ZIP:-$DEFAULT_ZIP}"
  unzip -j -q "$zip" "sdk/redistributable_bin/win64/steam_api64.dll" -d "$tmp/win64"
  unzip -j -q "$zip" "sdk/redistributable_bin/linux64/libsteam_api.so" -d "$tmp/linux64"
elif [[ -f "$GPG_BLOB" ]]; then
  if [[ -z "${STEAMWORKS_GPG_PASSPHRASE:-}" ]]; then
    echo "steamworks-165.tar.gpg present; set STEAMWORKS_GPG_PASSPHRASE to decrypt" >&2
    exit 1
  fi
  printf '%s' "$STEAMWORKS_GPG_PASSPHRASE" | gpg --batch --yes --passphrase-fd 0 \
    --decrypt -o "$tmp/steamworks-165.tar" "$GPG_BLOB"
  tar -xf "$tmp/steamworks-165.tar" -C "$tmp"
else
  echo "need STEAMWORKS_SDK_ZIP, STEAMWORKS_SDK, or STEAMWORKS_GPG_PASSPHRASE + $GPG_BLOB" >&2
  exit 1
fi

if [[ ! -f "$tmp/win64/steam_api64.dll" || ! -f "$tmp/linux64/libsteam_api.so" ]]; then
  echo "redistributable files missing after fetch" >&2
  exit 1
fi

rm -rf "$DEST"
mkdir -p "$DEST/win64" "$DEST/linux64"
cp "$tmp/win64/steam_api64.dll" "$DEST/win64/"
cp "$tmp/linux64/libsteam_api.so" "$DEST/linux64/"
echo "$VERSION" > "$STAMP"
echo "wrote $DEST ($VERSION)"
