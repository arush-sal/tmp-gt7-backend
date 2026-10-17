# GARAGE backend — build & test tasks (rtps, with the in-process collector and the embedded web UI in web/).
# Run `make hooks` once after cloning to enable the pre-commit gate.

GO      ?= go
PKG     ?= ./...

# `make deploy-stack` knobs. The default DSN matches the compose `timescaledb` service.
IP       ?=
STORAGE  ?= postgres
DB_DSN   ?= postgres://gt7:gt7@localhost:5432/gt7?sslmode=disable
# On-disk Postgres data dir (bind-mounted by docker-compose; survives `docker system prune --volumes`).
DB_DIR   ?= $(HOME)/.garage/timescaledb
export GARAGE_DB_DIR := $(DB_DIR)
API_ADDR ?= localhost:8080
# `make replay-stack` knobs: capture to replay in-process, and the pacing multiplier.
FILE     ?=
SPEED    ?= 1
# The host has the standalone docker-compose binary, not the `docker compose` plugin.
COMPOSE  ?= docker-compose

# `make web-up` / `make web-preview`: backend the Vite dev proxy forwards /v1 to (host:port or URL)
RTPS     ?= localhost:8080

.PHONY: all build test test-race vet fmt fmt-check lint vuln tidy cover hooks tools clean deploy-stack replay-stack stop-stack web-install web-up web-build web-preview web-test

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
## builds the web UI + rtps, and runs rtps (in-process GT7 collector + API + embedded dashboard) in the
## background against a PS5, logging to bin/rtps.log. Re-run to restart; `make stop-stack` to stop.
## Bind the API wide (API_ADDR=0.0.0.0:8080) to open the dashboard from another device.
##   make deploy-stack IP=192.168.1.99 [STORAGE=memory | DB_DSN='postgres://...'] [API_ADDR=host:port]
## replay-stack: same, but rtps replays a recorded capture instead of a live PS5 (keeps serving at EOF).
##   make replay-stack FILE=capture.pcapng [SPEED=1] [STORAGE=memory | DB_DSN=...] [API_ADDR=host:port]
## Both default to STORAGE=postgres. Re-running a capture is safe: a session whose laps are already
## stored is dropped as a duplicate (lap fingerprints).
deploy-stack: SOURCE = --ps5.ip=$(IP)
deploy-stack: NEED = $(IP)
replay-stack: SOURCE = --replay-file="$(FILE)" --replay.speed=$(SPEED)
replay-stack: NEED = $(FILE)
deploy-stack replay-stack: web-build
	@test -n "$(NEED)" || { echo "usage: make deploy-stack IP=<ps5-ip> | make replay-stack FILE=<capture> [SPEED=1]  [STORAGE=memory | DB_DSN=...] [API_ADDR=host:port]"; exit 2; }
	@if [ "$(STORAGE)" = postgres ]; then mkdir -p "$(DB_DIR)" && $(COMPOSE) up -d --wait timescaledb; fi
	@mkdir -p bin
	$(GO) build -o bin/rtps ./cmd/rtps
	@bash -c 'pkill -x rtps 2>/dev/null; sleep 2; \
		nohup ./bin/rtps $(SOURCE) --storage=$(STORAGE) --db.dsn="$(DB_DSN)" --api.addr=$(API_ADDR) --ingest.addr=localhost:50051 --log.level=info >bin/rtps.log 2>&1 & disown'
	@sleep 2; \
		curl -sf -o /dev/null http://$(API_ADDR)/v1/live && echo "rtps       ok    http://$(API_ADDR)/v1/live" || echo "rtps       DOWN  (bin/rtps.log)"; \
		curl -sf -o /dev/null http://$(API_ADDR)/ && echo "dashboard  ok    http://$(API_ADDR)/" || echo "dashboard  DOWN  (bin/rtps.log)"; \
		echo "stop: make stop-stack"

## stop-stack: stop a running deploy-stack/replay-stack (rtps, and timescaledb when STORAGE=postgres;
## the DB volume is kept).
stop-stack:
	@bash -c 'pkill -x rtps && echo "stopped rtps" || echo "rtps not running"'
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

## web-build: build the web UI into web/dist (embedded into rtps by go build). Incremental: only rebuilds
## when a web source/config file is newer than web/dist/index.html, so deploy-stack/replay-stack skip
## the ~10-20 s npm build when nothing in web/ changed.
WEB_SRC := $(shell find web/src -type f 2>/dev/null) web/index.html web/package.json web/package-lock.json \
	web/vite.config.ts web/tsconfig.json
web-build: web/dist/index.html

web/dist/index.html: web/node_modules $(WEB_SRC)
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
	rm -f coverage.out
	rm -rf bin
