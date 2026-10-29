# Day-to-day commands. The app itself only builds on macOS.

.PHONY: test daemon dev app release

test:
	go vet ./... && go test -race ./...

# The daemon in the foreground; the UI is at http://127.0.0.1:7878 (paste ~/.layer8/token).
daemon:
	go run ./cmd/layer8d daemon

# The daemon from source, with the UI opened in your browser and signed in. Ctrl+C stops it.
dev:
	@go build -o build/out/layer8d-dev ./cmd/layer8d
	@build/out/layer8d-dev daemon & pid=$$!; trap 'kill $$pid 2>/dev/null' INT TERM EXIT; \
	until curl -so /dev/null http://127.0.0.1:7878; do kill -0 $$pid 2>/dev/null || exit 1; sleep 0.2; done; \
	url="http://127.0.0.1:7878/#token=$$(cat "$${LAYER8_HOME:-$$HOME/.layer8}/token")"; \
	(open "$$url" 2>/dev/null || xdg-open "$$url" 2>/dev/null || echo "Open $$url"); \
	wait $$pid

# A local build of layer8.app (no update signing) in src-tauri/target/.
app:
	LAYER8_NO_UPDATER=1 build/build-macos.sh 0.0.0-dev

# Tag and push a release; GitHub Actions builds and publishes it.
release:
	@test -n "$(VERSION)" || (echo "usage: make release VERSION=0.2.0" && exit 1)
	git tag v$(VERSION) && git push origin v$(VERSION)
