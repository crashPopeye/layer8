.PHONY: check build dev

LAYER8_HOME ?= $(HOME)/.layer8-dev
LAYER8_ADDR ?= 127.0.0.1:7879
export LAYER8_HOME LAYER8_ADDR

# The only CI: formatting, vet and the race-enabled test suite.
check:
	@out=$$(gofmt -l .); if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi
	go vet ./...
	go test -race ./...

build:
	go build -o build/out/layer8 ./cmd/layer8
	go build -o build/out/l8 ./cmd/l8

# The daemon from source on LAYER8_ADDR with state in LAYER8_HOME, and the UI opened signed in. Ctrl+C stops it.
dev: build
	@build/out/layer8 daemon & pid=$$!; trap 'kill $$pid 2>/dev/null' INT TERM EXIT; \
	until curl -so /dev/null "http://$(LAYER8_ADDR)/api/version"; do kill -0 $$pid 2>/dev/null || exit 1; sleep 0.2; done; \
	url="http://$(LAYER8_ADDR)/auth?t=$$(cat "$(LAYER8_HOME)/token")"; \
	echo "layer8 UI: $$url"; \
	echo "CLI:       LAYER8_HOME=$(LAYER8_HOME) LAYER8_ADDR=$(LAYER8_ADDR) build/out/l8 task list"; \
	(open "$$url" 2>/dev/null || xdg-open "$$url" 2>/dev/null || true); \
	wait $$pid
