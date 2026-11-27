VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
APP_VERSION ?= $(shell sed -n 's/^  "version": "\(.*\)",$$/\1/p' app/src-tauri/tauri.conf.json)

.PHONY: test daemon dev app release e2e

test:
	go test ./...

daemon:
	CGO_ENABLED=0 go build -ldflags "-X main.version=$(VERSION)" -o bin/layer8d ./cmd/layer8d
	ln -sf layer8d bin/layer8

dev:
	LAYER8_HOME=$(CURDIR)/.dev-home go run ./cmd/layer8d serve

app:
	scripts/app.sh $(APP_VERSION)

release:
	scripts/release.sh $(VERSION)

e2e:
	go test -tags e2e -count=1 -timeout 30m -v ./e2e
