.PHONY: help dev build test format lint frontend-dev

.DEFAULT_GOAL := help

help: ## Show available commands
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'

dev: ## Start the setup guide
	go run .

build: ## Build the production binary
	$(MAKE) frontend-build
	go build -o app .

test: ## Run Go tests
	go test ./internal/...

format: ## Format Go files
	go fmt ./...

lint: ## Run Go vet
	go vet ./internal/...

frontend-dev: ## Start the Vite development server
	cd frontend && yarn dev

frontend-build: ## Build the frontend into dist/
	cd frontend && yarn build
