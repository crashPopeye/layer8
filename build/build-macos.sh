#!/usr/bin/env bash
# Builds layer8.app (universal: Apple Silicon + Intel), signs it, and zips it.
#
#   build/build-macos.sh 0.2.0
#
# Signing: with MACOS_SIGNING_IDENTITY set (a "Developer ID Application: …"
# identity in the keychain) the app is signed with the hardened runtime, and
# with APPLE_ID, APPLE_TEAM_ID and APPLE_APP_PASSWORD also notarized. Without
# them it is ad-hoc signed, which runs on Apple Silicon but must be allowed in
# Gatekeeper once (see INSTALL.md).
set -euo pipefail

VERSION="${1:?usage: build-macos.sh VERSION}"
VERSION="${VERSION#v}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUT="$ROOT/build/out"
APP="$OUT/layer8.app"
ZIP="$OUT/layer8-$VERSION-macos-universal.zip"

[[ "$(uname)" == "Darwin" ]] || { echo "build-macos.sh must run on macOS" >&2; exit 1; }
rm -rf "$OUT"
mkdir -p "$OUT"

export CGO_ENABLED=1 GOOS=darwin
export MACOSX_DEPLOYMENT_TARGET=12.0
export CGO_CFLAGS="-mmacosx-version-min=12.0" CGO_LDFLAGS="-mmacosx-version-min=12.0"
LDFLAGS="-s -w -X main.version=$VERSION"

cd "$ROOT"
for arch in arm64 amd64; do
  echo "==> go build ($arch)"
  GOARCH=$arch go build -tags production -trimpath -buildvcs=false -ldflags "$LDFLAGS" -o "$OUT/layer8-$arch" .
done
lipo -create -output "$OUT/layer8" "$OUT/layer8-arm64" "$OUT/layer8-amd64"
lipo -info "$OUT/layer8"

echo "==> bundle"
mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources"
cp "$OUT/layer8" "$APP/Contents/MacOS/layer8"
sed "s/__VERSION__/$VERSION/g" build/darwin/Info.plist > "$APP/Contents/Info.plist"
plutil -lint "$APP/Contents/Info.plist"

ICONSET="$OUT/icons.iconset"
mkdir -p "$ICONSET"
for size in 16 32 128 256 512; do
  sips -z $size $size build/darwin/icon-1024.png --out "$ICONSET/icon_${size}x${size}.png" >/dev/null
  sips -z $((size * 2)) $((size * 2)) build/darwin/icon-1024.png --out "$ICONSET/icon_${size}x${size}@2x.png" >/dev/null
done
iconutil -c icns "$ICONSET" -o "$APP/Contents/Resources/icons.icns"

echo "==> sign"
xattr -cr "$APP"
if [[ -n "${MACOS_SIGNING_IDENTITY:-}" ]]; then
  codesign --force --options runtime --timestamp --sign "$MACOS_SIGNING_IDENTITY" "$APP"
else
  codesign --force --deep --sign - "$APP"
fi
codesign --verify --deep --strict --verbose=2 "$APP"

ditto -c -k --sequesterRsrc --keepParent "$APP" "$ZIP"

if [[ -n "${MACOS_SIGNING_IDENTITY:-}" && -n "${APPLE_ID:-}" && -n "${APPLE_TEAM_ID:-}" && -n "${APPLE_APP_PASSWORD:-}" ]]; then
  echo "==> notarize"
  xcrun notarytool submit "$ZIP" --apple-id "$APPLE_ID" --team-id "$APPLE_TEAM_ID" --password "$APPLE_APP_PASSWORD" --wait
  xcrun stapler staple "$APP"
  rm "$ZIP"
  ditto -c -k --sequesterRsrc --keepParent "$APP" "$ZIP"
fi

(cd "$OUT" && shasum -a 256 "$(basename "$ZIP")" > checksums.txt)
cat "$OUT/checksums.txt"
echo "==> built $ZIP"
