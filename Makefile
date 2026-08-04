.PHONY: help dev build test format lint dev-fe

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

format: ## Format Go files
	go fmt ./...

lint: ## Run Go vet
	go vet ./internal/...

dev-fe: ## Start the Vite development server
	cd frontend && yarn dev

build-fe: ## Build the frontend into dist/
	cd frontend && yarn build
