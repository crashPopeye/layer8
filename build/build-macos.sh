#!/usr/bin/env bash
# Builds layer8.app (universal: Apple Silicon + Intel) with Tauri, plus the update
# artifacts the auto-updater reads.
#
#   build/build-macos.sh 0.2.0
#
# Output in build/out/: layer8-<v>-macos-universal.dmg (drag to Applications),
# .zip (same app, for scripted installs), .app.tar.gz + latest.json (updates).
#
# Environment:
#   TAURI_SIGNING_PRIVATE_KEY   signs the update (required unless LAYER8_NO_UPDATER=1)
#   LAYER8_NO_UPDATER=1         build without update artifacts (CI checks, local builds)
#   MACOS_SIGNING_IDENTITY      "Developer ID Application: …" to codesign; ad-hoc otherwise
#   APPLE_ID, APPLE_TEAM_ID, APPLE_APP_PASSWORD   notarize (with a Developer ID only)
#   GITHUB_REPOSITORY           owner/repo for download URLs (default crashPopeye/layer8)
set -euo pipefail

VERSION="${1:?usage: build-macos.sh VERSION}"
VERSION="${VERSION#v}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUT="$ROOT/build/out"
REPO="${GITHUB_REPOSITORY:-crashPopeye/layer8}"
[[ "$(uname)" == "Darwin" ]] || { echo "build-macos.sh must run on macOS" >&2; exit 1; }
cd "$ROOT"
rm -rf "$OUT"
mkdir -p "$OUT" src-tauri/binaries

echo "==> layer8d (Go, universal)"
LDFLAGS="-s -w -X main.version=$VERSION"
for arch in arm64 amd64; do
  CGO_ENABLED=0 GOOS=darwin GOARCH=$arch go build -trimpath -ldflags "$LDFLAGS" -o "$OUT/layer8d-$arch" ./cmd/layer8d
done
# Tauri compiles each architecture separately and then bundles the universal app,
# so it looks for the sidecar under all three target names.
cp "$OUT/layer8d-arm64" src-tauri/binaries/layer8d-aarch64-apple-darwin
cp "$OUT/layer8d-amd64" src-tauri/binaries/layer8d-x86_64-apple-darwin
lipo -create -output src-tauri/binaries/layer8d-universal-apple-darwin "$OUT/layer8d-arm64" "$OUT/layer8d-amd64"
lipo -info src-tauri/binaries/layer8d-universal-apple-darwin

echo "==> layer8.app (Tauri, universal)"
CONFIG="{\"version\":\"$VERSION\"}"
if [[ "${LAYER8_NO_UPDATER:-}" == "1" ]]; then
  CONFIG="{\"version\":\"$VERSION\",\"bundle\":{\"createUpdaterArtifacts\":false}}"
elif [[ -z "${TAURI_SIGNING_PRIVATE_KEY:-}" ]]; then
  echo "TAURI_SIGNING_PRIVATE_KEY is not set: updates can't be signed. Add the repository secret (see INSTALL.md), or set LAYER8_NO_UPDATER=1 for a test build." >&2
  exit 1
fi
# Tauri signs the app and the bundled layer8d; "-" means ad-hoc.
export APPLE_SIGNING_IDENTITY="${MACOS_SIGNING_IDENTITY:--}"
if [[ -n "${MACOS_SIGNING_IDENTITY:-}" && -n "${APPLE_ID:-}" && -n "${APPLE_TEAM_ID:-}" && -n "${APPLE_APP_PASSWORD:-}" ]]; then
  export APPLE_PASSWORD="$APPLE_APP_PASSWORD" # Tauri notarizes when these are set
else
  unset APPLE_ID APPLE_PASSWORD APPLE_TEAM_ID 2>/dev/null || true
fi
export TAURI_SIGNING_PRIVATE_KEY_PASSWORD="${TAURI_SIGNING_PRIVATE_KEY_PASSWORD:-}"
(cd src-tauri && cargo tauri build --ci --target universal-apple-darwin --config "$CONFIG")

BUNDLE="src-tauri/target/universal-apple-darwin/release/bundle/macos"
APP="$BUNDLE/layer8.app"
codesign --verify --deep --strict --verbose=2 "$APP"
lipo -info "$APP/Contents/MacOS/layer8" "$APP/Contents/MacOS/layer8d"

ZIP="layer8-$VERSION-macos-universal.zip"
ditto -c -k --sequesterRsrc --keepParent "$APP" "$OUT/$ZIP"

# The drag-to-Applications disk image Tauri built next to the app.
DMG_SRC=( src-tauri/target/universal-apple-darwin/release/bundle/dmg/*.dmg )
[[ -f "${DMG_SRC[0]}" ]] || { echo "no .dmg was built" >&2; exit 1; }
cp "${DMG_SRC[0]}" "$OUT/layer8-$VERSION-macos-universal.dmg"
hdiutil verify "$OUT/layer8-$VERSION-macos-universal.dmg"

if [[ "${LAYER8_NO_UPDATER:-}" != "1" ]]; then
  TARBALL="layer8-$VERSION-macos-universal.app.tar.gz"
  cp "$BUNDLE/layer8.app.tar.gz" "$OUT/$TARBALL"
  SIG="$(cat "$BUNDLE/layer8.app.tar.gz.sig")"
  URL="https://github.com/$REPO/releases/download/v$VERSION/$TARBALL"
  # The updater looks up darwin-<arch>-app, then darwin-<arch>; there is no
  # "universal" key, so the universal build is listed under both architectures.
  python3 - "$VERSION" "$URL" "$SIG" > "$OUT/latest.json" <<'PY'
import json, sys, datetime
version, url, sig = sys.argv[1:4]
entry = {"url": url, "signature": sig}
print(json.dumps({
    "version": version,
    "notes": f"layer8 {version}",
    "pub_date": datetime.datetime.now(datetime.timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
    "platforms": {k: entry for k in ("darwin-aarch64", "darwin-x86_64", "darwin-aarch64-app", "darwin-x86_64-app")},
}, indent=2))
PY
  cat "$OUT/latest.json"
fi

(cd "$OUT" && rm -f layer8d-arm64 layer8d-amd64 && shasum -a 256 *.dmg *.zip *.tar.gz 2>/dev/null > checksums.txt || true)
cat "$OUT/checksums.txt"
echo "==> built in $OUT"
