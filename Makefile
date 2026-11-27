VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

.PHONY: test daemon dev

test:
	go test ./...

daemon:
	CGO_ENABLED=0 go build -ldflags "-X main.version=$(VERSION)" -o bin/layer8d ./cmd/layer8d
	ln -sf layer8d bin/layer8

dev:
	LAYER8_HOME=$(CURDIR)/.dev-home go run ./cmd/layer8d serve
