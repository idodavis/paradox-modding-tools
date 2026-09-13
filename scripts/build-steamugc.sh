#!/usr/bin/env bash
# Build pmt-steamugc into steamugc/binembed/bin and copy the GOOS Steam API lib.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DEST="$ROOT/services/internal/steamugc/binembed/bin"
SRC="$ROOT/services/internal/steamugc/sdk"
GOOS="${GOOS:-$(go env GOOS)}"
GOARCH="${GOARCH:-$(go env GOARCH)}"

mkdir -p "$DEST"
# Steamworks ships win64 (amd64) only; ARM Windows runs the amd64 helper.
if [[ "$GOOS" == "windows" ]]; then
  GOARCH=amd64
fi

rm -f "$DEST/pmt-steamugc" "$DEST/pmt-steamugc.exe" \
  "$DEST/steam_api64.dll" "$DEST/libsteam_api.so" "$DEST/libsteam_api.dylib"

OUT="pmt-steamugc"
LDFLAGS=""
if [[ "$GOOS" == "windows" ]]; then
  OUT="pmt-steamugc.exe"
  LDFLAGS="-H windowsgui"
fi

CGO_ENABLED=0 GOOS="$GOOS" GOARCH="$GOARCH" \
  go build -ldflags "$LDFLAGS" -o "$DEST/$OUT" \
    "$ROOT/services/internal/steamugc/cmd/pmt-steamugc"

case "$GOOS" in
  windows)
    cp "$SRC/win64/steam_api64.dll" "$DEST/"
    ;;
  *)
    cp "$SRC/linux64/libsteam_api.so" "$DEST/"
    ;;
esac

echo "wrote $DEST/$OUT + Steam API ($GOOS/$GOARCH)"
