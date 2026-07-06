# GT7 Collector — build & test tasks.
# Run `make hooks` once after cloning to enable the pre-commit gate.

GO      ?= go
PKG     ?= ./...

# `make live` / `make dist-joshua` knobs
IP       ?=
STORAGE  ?= memory
DB_DSN   ?=
API_ADDR ?= localhost:8080
JOSHUA   ?= salil@joshua

.PHONY: all build test test-race vet fmt fmt-check lint vuln tidy cover hooks tools clean live dist-joshua

## all: format check, lint, and test (the default gate)
all: fmt-check lint test

## build: compile all packages
build:
	$(GO) build $(PKG)

## test: run all tests
test:
	$(GO) test $(PKG)

## test-race: run all tests with the race detector, uncached
test-race:
	$(GO) test -race -count=1 $(PKG)

## vet: run go vet
vet:
	$(GO) vet $(PKG)

## fmt: format all Go files in place
fmt:
	$(GO) fmt $(PKG)

## fmt-check: fail if any Go file is not gofmt-clean
fmt-check:
	@unformatted=$$(gofmt -l .); \
	if [ -n "$$unformatted" ]; then \
		echo "gofmt needed on:"; echo "$$unformatted"; exit 1; \
	fi

## lint: run golangci-lint (see .golangci.yml)
lint:
	golangci-lint run

## vuln: scan dependencies + code for known vulnerabilities
vuln:
	govulncheck $(PKG)

## tools: install the developer tooling (linter, vuln scanner)
tools:
	$(GO) install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest
	$(GO) install golang.org/x/vuln/cmd/govulncheck@latest

## tidy: prune and sync go.mod/go.sum
tidy:
	$(GO) mod tidy

## cover: run tests with coverage and print a per-function summary
cover:
	$(GO) test -coverprofile=coverage.out $(PKG)
	$(GO) tool cover -func=coverage.out

## hooks: point git at the tracked .githooks directory
hooks:
	git config core.hooksPath .githooks
	@echo "git hooks path set to .githooks"

## live: run backend + collector + TUI together against a PS5. Usage: make live IP=192.168.1.99
##       (opts: STORAGE=postgres DB_DSN='postgres://...' API_ADDR=host:port). Ctrl-C stops all.
live:
	@test -n "$(IP)" || { echo "usage: make live IP=<ps5-ip> [STORAGE=postgres DB_DSN=... API_ADDR=host:port]"; exit 2; }
	@mkdir -p bin
	$(GO) build -o bin/rtps ./cmd/rtps
	$(GO) build -o bin/collector ./cmd/collector
	$(GO) build -o bin/tui ./cmd/tui
	@echo ">> backend ($(API_ADDR)) + collector (PS5 $(IP)); TUI in foreground — Ctrl-C stops all"
	@bash -c "trap 'kill $$RTPS $$COL 2>/dev/null' EXIT INT TERM; \
		./bin/rtps --storage=$(STORAGE) --db.dsn='$(DB_DSN)' --api.addr=$(API_ADDR) --log.level=warn & RTPS=$$!; \
		sleep 2; \
		./bin/collector --ps5.ip=$(IP) --log.level=warn & COL=$$!; \
		./bin/tui --api http://$(API_ADDR)"

## dist-joshua: build linux rtps+collector, copy to the joshua LAN host, and (re)start them there.
## joshua sits on the PS5's LAN and receives the console's UDP natively (WSL2 NAT cannot). rtps binds
## its API wide (0.0.0.0:8080) so a WSL/dev box can curl http://<joshua-lan-ip>:8080. Usage:
##   make dist-joshua IP=192.168.1.99 [JOSHUA=user@host]
dist-joshua:
	@test -n "$(IP)" || { echo "usage: make dist-joshua IP=<ps5-ip> [JOSHUA=user@host]"; exit 2; }
	@mkdir -p bin/linux
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 $(GO) build -o bin/linux/rtps ./cmd/rtps
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 $(GO) build -o bin/linux/collector ./cmd/collector
	ssh $(JOSHUA) 'pkill -x rtps 2>/dev/null; pkill -x collector 2>/dev/null; sleep 2; rm -f ~/rtps ~/collector' || true
	scp bin/linux/rtps bin/linux/collector $(JOSHUA):~/
	ssh $(JOSHUA) 'nohup ~/rtps --storage=$(STORAGE) --db.dsn="$(DB_DSN)" --api.addr=0.0.0.0:8080 --ingest.addr=localhost:50051 --log.level=info >~/rtps.log 2>&1 & disown; \
		sleep 2; \
		nohup ~/collector --ps5.ip=$(IP) --rtps.addr=localhost:50051 --log.level=info >~/collector.log 2>&1 & disown; \
		echo "started rtps + collector on $(JOSHUA) (logs: ~/rtps.log ~/collector.log)"'
	@echo ">> rtps API on $(JOSHUA):8080 (bind 0.0.0.0). From a dev box: curl http://<joshua-lan-ip>:8080/v1/live"

## clean: remove build and coverage artifacts
clean:
	rm -f coverage.out cmd/collector/collector
	rm -rf bin
