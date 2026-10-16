# GARAGE backend — build & test tasks (collector, rtps, tui, and the embedded web UI in web/).
# Run `make hooks` once after cloning to enable the pre-commit gate.

GO      ?= go
PKG     ?= ./...

# `make deploy-stack` knobs. The default DSN matches the compose `timescaledb` service.
IP       ?=
STORAGE  ?= postgres
DB_DSN   ?= postgres://gt7:gt7@localhost:5432/gt7?sslmode=disable
API_ADDR ?= localhost:8080
# The host has the standalone docker-compose binary, not the `docker compose` plugin.
COMPOSE  ?= docker-compose

# `make web-up` / `make web-preview`: backend the Vite dev proxy forwards /v1 to (host:port or URL)
RTPS     ?= localhost:8080

.PHONY: all build test test-race vet fmt fmt-check lint vuln tidy cover hooks tools clean deploy-stack stop-stack web-install web-up web-build web-preview web-test

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

## deploy-stack: the one command for a race session. Starts timescaledb (compose, waits for healthy),
## builds the web UI + binaries, and runs rtps (API + embedded dashboard) and the collector in the
## background against a PS5, logging to bin/. Re-run to restart; `make stop-stack` to stop.
## Bind the API wide (API_ADDR=0.0.0.0:8080) to open the dashboard from another device.
##   make deploy-stack IP=192.168.1.99 [STORAGE=memory | DB_DSN='postgres://...'] [API_ADDR=host:port]
deploy-stack: web-build
	@test -n "$(IP)" || { echo "usage: make deploy-stack IP=<ps5-ip> [STORAGE=memory | DB_DSN=...] [API_ADDR=host:port]"; exit 2; }
	@if [ "$(STORAGE)" = postgres ]; then $(COMPOSE) up -d --wait timescaledb; fi
	@mkdir -p bin
	$(GO) build -o bin/rtps ./cmd/rtps
	$(GO) build -o bin/collector ./cmd/collector
	$(GO) build -o bin/tui ./cmd/tui
	@bash -c 'pkill -x rtps 2>/dev/null; pkill -x collector 2>/dev/null; sleep 2; \
		nohup ./bin/rtps --storage=$(STORAGE) --db.dsn="$(DB_DSN)" --api.addr=$(API_ADDR) --ingest.addr=localhost:50051 --log.level=info >bin/rtps.log 2>&1 & disown; \
		sleep 2; \
		nohup ./bin/collector --ps5.ip=$(IP) --rtps.addr=localhost:50051 --log.level=info >bin/collector.log 2>&1 & disown'
	@sleep 2; \
		curl -sf -o /dev/null http://$(API_ADDR)/v1/live && echo "rtps       ok    http://$(API_ADDR)/v1/live" || echo "rtps       DOWN  (bin/rtps.log)"; \
		curl -sf -o /dev/null http://$(API_ADDR)/ && echo "dashboard  ok    http://$(API_ADDR)/" || echo "dashboard  DOWN  (bin/rtps.log)"; \
		pgrep -x collector >/dev/null && echo "collector  ok    PS5 $(IP)" || echo "collector  DOWN  (bin/collector.log)"; \
		echo "pit wall: ./bin/tui --api http://$(API_ADDR)    |    stop: make stop-stack"

## stop-stack: stop a running deploy-stack (rtps + collector, and timescaledb when STORAGE=postgres;
## the DB volume is kept).
stop-stack:
	@bash -c 'pkill -x rtps && echo "stopped rtps" || echo "rtps not running"; \
		pkill -x collector && echo "stopped collector" || echo "collector not running"'
	@if [ "$(STORAGE)" = postgres ]; then $(COMPOSE) stop timescaledb; fi

## web-install: install the web UI dependencies (npm ci)
web-install:
	npm --prefix web ci

# Fresh checkout (or lockfile change): install deps before any web target needs them.
web/node_modules: web/package-lock.json
	npm --prefix web ci
	@touch $@

## web-up: run the web UI dev server, proxying /v1 to RTPS. Usage: make web-up [RTPS=host:port]
web-up: web/node_modules
	@echo "-> web dev - proxying /v1 to $(RTPS)"
	GT7_API_ADDR="$(RTPS)" npm --prefix web run dev

## web-build: build the web UI into web/dist (embedded into rtps by go build)
web-build: web/node_modules
	npm --prefix web run build
	touch web/dist/.gitkeep

## web-preview: serve the production web build locally (proxies /v1 to RTPS)
web-preview: web/node_modules
	GT7_API_ADDR="$(RTPS)" npm --prefix web run preview

## web-test: run the web UI tests
web-test: web/node_modules
	npm --prefix web test

## clean: remove build and coverage artifacts
clean:
	rm -f coverage.out cmd/collector/collector
	rm -rf bin
