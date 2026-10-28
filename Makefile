# Day-to-day commands. The app itself only builds on macOS.

.PHONY: test daemon app release

test:
	go vet ./... && go test -race ./...

# The daemon in the foreground; the UI is at http://127.0.0.1:7878 (paste ~/.layer8/token).
daemon:
	go run ./cmd/layer8d daemon

# A local build of layer8.app (no update signing) in src-tauri/target/.
app:
	LAYER8_NO_UPDATER=1 build/build-macos.sh 0.0.0-dev

# Tag and push a release; GitHub Actions builds and publishes it.
release:
	@test -n "$(VERSION)" || (echo "usage: make release VERSION=0.2.0" && exit 1)
	git tag v$(VERSION) && git push origin v$(VERSION)
