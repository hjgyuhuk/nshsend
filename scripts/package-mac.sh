#!/usr/bin/env bash
# Build a self-contained NshSend.app: NSH runtime + the Go sidecar.
#
# nsh package bundles the TSX app and NSH runtime but not arbitrary executables,
# so after packaging we drop nshsend-core into Contents/MacOS and replace the
# main executable with a wrapper that prepends that directory to PATH before
# exec'ing the real runner (renamed NshSend-bin). process.spawn("nshsend-core")
# then resolves the bundled binary: bare names resolve through the host PATH
# only, and a Finder-launched app inherits launchd's minimal PATH. The runner
# still finds Contents/Resources/nsh through its own current_exe().
#
# The bundle is re-signed ad hoc: it runs on this Mac; Gatekeeper stops it
# elsewhere. Developer ID signing/notarization is not handled here.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

if [[ "$(uname -s)" != "Darwin" ]]; then
  echo "package-mac.sh must run on macOS" >&2
  exit 1
fi

APP_NAME="NshSend"
OUT_DIR="$ROOT_DIR/.nsh/out"
APP="$OUT_DIR/$APP_NAME.app"
MACOS_DIR="$APP/Contents/MacOS"

"$ROOT_DIR/scripts/build-core.sh"

echo "Packaging NSH app (macos)..."
nsh package --target macos

echo "Bundling nshsend-core into the app bundle..."
cp "$ROOT_DIR/bin/nshsend-core" "$MACOS_DIR/nshsend-core"
chmod +x "$MACOS_DIR/nshsend-core"

# Put the bundle's MacOS dir on PATH so the spawned sidecar is found.
mv "$MACOS_DIR/$APP_NAME" "$MACOS_DIR/$APP_NAME-bin"
cat > "$MACOS_DIR/$APP_NAME" <<'EOF'
#!/bin/bash
HERE="$(cd "$(dirname "$0")" && pwd)"
export PATH="$HERE:$PATH"
exec "$HERE/NshSend-bin" "$@"
EOF
chmod +x "$MACOS_DIR/$APP_NAME"

echo "Re-signing the bundle (ad hoc)..."
codesign --force --deep --sign - "$APP"
codesign --verify --deep --strict "$APP"

echo "Done: $APP"