# GT7 Race Coach - frontend dev/build helper.
#
#   make up                          # start the dev server, proxy /v1 -> localhost:8080
#   make up RTPS=192.168.1.50:8080   # ...or point it at a remote RTPS (host:port or full URL)
#   make install                     # npm install
#   make build                       # production build (tsc && vite build)
#   make preview                     # serve the production build (same proxy)
#
# RTPS is the backend address, passed as a make variable (Make can't take a bare
# host:port positional goal - the ':' is Make's rule separator). It flows into the
# Vite dev proxy via GT7_API_ADDR (see vite.config.ts). Defaults to localhost:8080.

RTPS ?= localhost:8080

.PHONY: up install build preview help

up: ## Start the Vite dev server (proxies /v1 to RTPS)
	@echo "-> frontend dev - proxying /v1 to $(RTPS)"
	GT7_API_ADDR="$(RTPS)" npm run dev

install: ## Install dependencies
	npm install

build: ## Production build (tsc && vite build)
	npm run build

preview: ## Serve the production build (proxies /v1 to RTPS)
	GT7_API_ADDR="$(RTPS)" npm run preview

help: ## List targets
	@grep -hE '^[a-zA-Z][a-zA-Z0-9_-]*:.*##' $(MAKEFILE_LIST) | sed -E 's/:.*## / - /'
