#!/usr/bin/env bash
# Build a self-contained NshSend AppImage: NSH runtime + the Go sidecar.
#
# nsh package bundles the TSX app and NSH runtime but not arbitrary executables,
# so after packaging we drop nshsend-core into the AppDir's usr/bin and prepend
# that directory to PATH in AppRun. process.spawn("nshsend-core") then resolves
# the bundled binary (the child inherits the host PATH).
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

APP_NAME="NshSend"
ARCH="$(uname -m)"
OUT_DIR="$ROOT_DIR/.nsh/out"
APPDIR="$OUT_DIR/$APP_NAME.AppDir"
APPIMAGETOOL="$ROOT_DIR/bin/appimagetool"

"$ROOT_DIR/scripts/build-core.sh"

# appimagetool is itself an AppImage; fetch it once into bin/ if absent.
if [[ ! -x "$APPIMAGETOOL" ]]; then
  echo "Downloading appimagetool..."
  curl -sL -o "$APPIMAGETOOL" \
    "https://github.com/AppImage/appimagetool/releases/download/1.9.0/appimagetool-${ARCH}.AppImage"
  chmod +x "$APPIMAGETOOL"
fi

echo "Packaging NSH app (linux/appimage)..."
NSH_APPIMAGETOOL="$APPIMAGETOOL" APPIMAGE_EXTRACT_AND_RUN=1 \
  nsh package --target linux --format appimage

echo "Bundling nshsend-core into the AppDir..."
cp "$ROOT_DIR/bin/nshsend-core" "$APPDIR/usr/bin/nshsend-core"
chmod +x "$APPDIR/usr/bin/nshsend-core"

# Put usr/bin on PATH so the spawned sidecar is found; keep NSH's own exec line.
if ! grep -q 'usr/bin:\$PATH' "$APPDIR/AppRun"; then
  sed -i '/^exec /i export PATH="$HERE/usr/bin:$PATH"' "$APPDIR/AppRun"
fi

echo "Rebuilding AppImage with the sidecar..."
ARCH="$ARCH" APPIMAGE_EXTRACT_AND_RUN=1 \
  "$APPIMAGETOOL" "$APPDIR" "$OUT_DIR/$APP_NAME-$ARCH.AppImage"

echo "Done: $OUT_DIR/$APP_NAME-$ARCH.AppImage"