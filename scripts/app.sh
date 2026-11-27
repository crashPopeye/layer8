#!/usr/bin/env bash
# Builds the universal macOS app into app/src-tauri/target/universal-apple-darwin/release/bundle:
# layer8.app, the DMG, and the updater bundle layer8.app.tar.gz with its signature.
set -euo pipefail
version=${1:?usage: scripts/app.sh <x.y.z>}
cd "$(dirname "$0")/.."
export PATH="/opt/homebrew/opt/rustup/bin:$HOME/.cargo/bin:$PATH"

if ! command -v cargo >/dev/null; then
  brew install rustup
  rustup-init -y --no-modify-path
  export PATH="/opt/homebrew/opt/rustup/bin:$HOME/.cargo/bin:$PATH"
fi
rustup target add aarch64-apple-darwin x86_64-apple-darwin >/dev/null

key=${LAYER8_RELEASE_KEY:-$HOME/.layer8-release/updater.key}
conf=app/src-tauri/tauri.conf.json
if [ ! -f "$key" ]; then
  mkdir -p "$(dirname "$key")" && chmod 700 "$(dirname "$key")"
  (cd app && npx tauri signer generate --ci -w "$key")
  node -e 'const f=process.argv[1],c=require(require("path").resolve(f));c.plugins.updater.pubkey=require("fs").readFileSync(process.argv[2],"utf8").trim();require("fs").writeFileSync(f,JSON.stringify(c,null,2)+"\n")' "$conf" "$key.pub"
  echo "Generated the updater signing key $key; commit the new pubkey in $conf." >&2
fi
if [ "$(tr -d '\n' <"$key.pub")" != "$(node -p "require('./$conf').plugins.updater.pubkey")" ]; then
  echo "$key.pub does not match plugins.updater.pubkey in $conf" >&2
  exit 1
fi

mkdir -p bin app/src-tauri/binaries
for arch in arm64:aarch64 amd64:x86_64; do
  GOOS=darwin GOARCH=${arch%:*} CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=$version" \
    -o "app/src-tauri/binaries/layer8d-${arch#*:}-apple-darwin" ./cmd/layer8d
done
lipo -create -output app/src-tauri/binaries/layer8d-universal-apple-darwin \
  app/src-tauri/binaries/layer8d-aarch64-apple-darwin app/src-tauri/binaries/layer8d-x86_64-apple-darwin

cd app
npm ci
TAURI_SIGNING_PRIVATE_KEY="$key" TAURI_SIGNING_PRIVATE_KEY_PASSWORD="${TAURI_SIGNING_PRIVATE_KEY_PASSWORD:-}" \
  APPLE_SIGNING_IDENTITY="${APPLE_SIGNING_IDENTITY:--}" \
  npx tauri build --target universal-apple-darwin --config "{\"version\":\"$version\"}"
ls -1d "$PWD"/src-tauri/target/universal-apple-darwin/release/bundle/{macos/layer8.app,macos/layer8.app.tar.gz,macos/layer8.app.tar.gz.sig,dmg/*.dmg}
