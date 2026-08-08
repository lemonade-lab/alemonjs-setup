.PHONY: help dev build test test-agent test-all format lint dev-fe build-frontend release-check

.DEFAULT_GOAL := help

help: ## Show available commands
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'

dev: ## Start the setup guide (replaces an older local ALemonX backend)
	@set -e; \
	port=17390; \
	pids="$$(lsof -tiTCP:$$port -sTCP:LISTEN 2>/dev/null || true)"; \
	for pid in $$pids; do \
		command="$$(ps -p "$$pid" -o comm= | xargs basename)"; \
		if [ "$$command" != "alemonx" ] && [ "$$command" != "app" ]; then \
			echo "端口 $$port 已被 $$command (PID $$pid) 占用；为避免误杀，未停止该进程。"; \
			exit 1; \
		fi; \
		echo "停止旧 ALemonX 后端（PID $$pid，端口 $$port）…"; \
		kill "$$pid"; \
	done; \
	for attempt in 1 2 3 4 5; do \
		[ -z "$$(lsof -tiTCP:$$port -sTCP:LISTEN 2>/dev/null || true)" ] && break; \
		sleep 1; \
	done; \
	if [ -n "$$(lsof -tiTCP:$$port -sTCP:LISTEN 2>/dev/null || true)" ]; then \
		echo "旧 ALemonX 后端未能在预期时间内退出，取消启动。"; \
		exit 1; \
	fi; \
	go run .

build: ## Build the production binary
	$(MAKE) build-fe
	go build -o app .

test: ## Run Go tests
	go test ./internal/...

test-agent: ## Run Agent package tests
	GOCACHE="$${GOCACHE:-$$(mktemp -d)}" go test ./internal/agent ./internal/web

test-all: ## Run the complete Go test suite with injectable test storage
	ALX_TEST_CACHE_DIR="$${ALX_TEST_CACHE_DIR:-$$(mktemp -d)}" GOCACHE="$${GOCACHE:-$$(mktemp -d)}" go test ./...

format: ## Format Go files
	go fmt ./...

lint: ## Run Go vet and frontend lint
	GOCACHE="$${GOCACHE:-$$(mktemp -d)}" go vet ./internal/...
	cd frontend && yarn lint

dev-fe: ## Start the Vite development server
	cd frontend && yarn dev

build-fe: ## Build the frontend into dist/
	cd frontend && yarn build

build-frontend: build-fe ## Alias for the release gate

release-check: ## Run the publishability gate
	$(MAKE) test-agent
	$(MAKE) test-all
	cd frontend && yarn lint
	$(MAKE) build-frontend
	git diff --check
