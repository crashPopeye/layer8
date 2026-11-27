#!/usr/bin/env bash
# Builds layer8 <x.y.z> and publishes it as the GitHub release v<x.y.z> with the latest.json update manifest.
set -euo pipefail
version=${1:?usage: scripts/release.sh <x.y.z>}
cd "$(dirname "$0")/.."
[[ $version =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || { echo "VERSION must be x.y.z, got $version" >&2; exit 1; }
[ -z "$(git status --porcelain)" ] || { echo "commit or stash your changes first" >&2; exit 1; }
git fetch -q origin
[ -n "$(git branch -r --contains HEAD)" ] || { echo "push $(git rev-parse --short HEAD) first" >&2; exit 1; }
if gh release view "v$version" >/dev/null 2>&1; then echo "release v$version already exists" >&2; exit 1; fi

scripts/app.sh "$version"

bundle=app/src-tauri/target/universal-apple-darwin/release/bundle
repo=$(gh repo view --json nameWithOwner -q .nameWithOwner)
url="https://github.com/$repo/releases/download/v$version/layer8.app.tar.gz"
sig=$(tr -d '\n' <"$bundle/macos/layer8.app.tar.gz.sig")
platform="{\"signature\":\"$sig\",\"url\":\"$url\"}"
cat >"$bundle/latest.json" <<JSON
{"version":"$version","notes":"https://github.com/$repo/releases/tag/v$version","pub_date":"$(date -u +%Y-%m-%dT%H:%M:%SZ)",
 "platforms":{"darwin-aarch64":$platform,"darwin-x86_64":$platform}}
JSON

gh release create "v$version" --title "layer8 $version" --generate-notes --target "$(git rev-parse HEAD)" \
  "$bundle"/dmg/*.dmg "$bundle/macos/layer8.app.tar.gz" "$bundle/macos/layer8.app.tar.gz.sig" "$bundle/latest.json"
