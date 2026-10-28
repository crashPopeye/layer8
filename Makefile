# Day-to-day commands. The app itself only builds on macOS.

.PHONY: test dev app release

test:
	go vet ./... && go test -race ./...

# The UI and API in your browser at http://127.0.0.1:7999 (no desktop shell).
dev:
	go run ./cmd/devserver

# A local layer8.app in build/out/.
app:
	build/build-macos.sh 0.0.0-dev

# Tag and push a release; GitHub Actions builds and publishes it.
release:
	@test -n "$(VERSION)" || (echo "usage: make release VERSION=0.2.0" && exit 1)
	git tag v$(VERSION) && git push origin v$(VERSION)
