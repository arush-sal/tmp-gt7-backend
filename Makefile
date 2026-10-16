# GT7 Collector — build & test tasks.
# Run `make hooks` once after cloning to enable the pre-commit gate.

GO      ?= go
PKG     ?= ./...

# `make live` / `make deploy-stack` knobs
IP       ?=
STORAGE  ?= memory
DB_DSN   ?=
API_ADDR ?= localhost:8080

# `make web-up` / `make web-preview`: backend the Vite dev proxy forwards /v1 to (host:port or URL)
RTPS     ?= localhost:8080

.PHONY: all build test test-race vet fmt fmt-check lint vuln tidy cover hooks tools clean live deploy-stack stop-stack web-install web-up web-build web-preview web-test

## all: format check, lint, and test (the default gate)
all: fmt-check lint test

## build: compile all packages
build:
	$(GO) build -o ./bin/ $(PKG)

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
	@unformatted=$$(gofmt -l $$(git ls-files '*.go')); \
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
live: web-build
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

## deploy-stack: build and start the rtps + collector stack in the background against a PS5, logging
## to bin/. Local (native Linux) — no ssh/cross-compile. Re-run to restart; `make stop-stack` to stop.
## Bind the API wide (API_ADDR=0.0.0.0:8080) if another host (e.g. the web dashboard) must reach it.
##   make deploy-stack IP=192.168.1.99 [STORAGE=postgres DB_DSN='postgres://...' API_ADDR=host:port]
deploy-stack: web-build
	@test -n "$(IP)" || { echo "usage: make deploy-stack IP=<ps5-ip> [STORAGE=postgres DB_DSN=... API_ADDR=host:port]"; exit 2; }
	@mkdir -p bin
	$(GO) build -o bin/rtps ./cmd/rtps
	$(GO) build -o bin/collector ./cmd/collector
	@bash -c 'pkill -x rtps 2>/dev/null; pkill -x collector 2>/dev/null; sleep 2; \
		nohup ./bin/rtps --storage=$(STORAGE) --db.dsn="$(DB_DSN)" --api.addr=$(API_ADDR) --ingest.addr=localhost:50051 --log.level=info >bin/rtps.log 2>&1 & disown; \
		sleep 2; \
		nohup ./bin/collector --ps5.ip=$(IP) --rtps.addr=localhost:50051 --log.level=info >bin/collector.log 2>&1 & disown; \
		echo ">> started rtps ($(API_ADDR)) + collector (PS5 $(IP)) in background (logs: bin/rtps.log bin/collector.log)"'
	@echo ">> curl http://$(API_ADDR)/v1/live    |    stop: make stop-stack"

## stop-stack: stop a running deploy-stack (rtps + collector).
stop-stack:
	@bash -c 'pkill -x rtps && echo "stopped rtps" || echo "rtps not running"; \
		pkill -x collector && echo "stopped collector" || echo "collector not running"'

## web-install: install the web UI dependencies (npm ci)
web-install:
	npm --prefix web ci

## web-up: run the web UI dev server, proxying /v1 to RTPS. Usage: make web-up [RTPS=host:port]
web-up:
	@echo "-> web dev - proxying /v1 to $(RTPS)"
	GT7_API_ADDR="$(RTPS)" npm --prefix web run dev

## web-build: build the web UI into web/dist (embedded into rtps by go build)
web-build:
	npm --prefix web run build
	touch web/dist/.gitkeep

## web-preview: serve the production web build locally (proxies /v1 to RTPS)
web-preview:
	GT7_API_ADDR="$(RTPS)" npm --prefix web run preview

## web-test: run the web UI tests
web-test:
	npm --prefix web test

## clean: remove build and coverage artifacts
clean:
	rm -f coverage.out cmd/collector/collector
	rm -rf bin
