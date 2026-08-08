.PHONY: help dev build test test-agent test-all format lint dev-fe build-frontend release-check

.DEFAULT_GOAL := help

help: ## Show available commands
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'

dev: ## Start the setup guide
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
