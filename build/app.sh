#!/usr/bin/env bash
# Builds build/out/layer8.app (window, daemon and l8, ad-hoc signed) and, given a
# destination folder, installs it there:  build/app.sh /Applications
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUT="$ROOT/build/out"
APP="$OUT/layer8.app"
DEST="${1:-}"
VERSION="${VERSION:-$(git -C "$ROOT" describe --tags --always --dirty 2>/dev/null || echo dev)}"
VERSION="${VERSION#v}"
export CGO_ENABLED=1 MACOSX_DEPLOYMENT_TARGET="${MACOSX_DEPLOYMENT_TARGET:-11.0}"

rm -rf "$APP"
mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources"
flags="-s -w -X github.com/crashPopeye/layer8/internal/api.Version=$VERSION+$(date +%Y%m%d.%H%M%S)"
(cd "$ROOT" && go build -trimpath -ldflags "$flags" -o "$APP/Contents/MacOS/layer8" ./cmd/layer8)
(cd "$ROOT" && go build -trimpath -ldflags "$flags" -o "$APP/Contents/MacOS/l8" ./cmd/l8)
sed "s/VERSION/$VERSION/g" "$ROOT/build/Info.plist" > "$APP/Contents/Info.plist"
cp "$ROOT/build/icon.icns" "$APP/Contents/Resources/icon.icns"
codesign --force --deep --sign - "$APP"
echo "built $APP ($VERSION)"

if [[ -n "$DEST" ]]; then
  rm -rf "$DEST/layer8.app"
  ditto "$APP" "$DEST/layer8.app"
  echo "installed $DEST/layer8.app; open it to start (or refresh) the daemon"
fi
