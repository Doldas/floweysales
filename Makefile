# FloweySales build & run commands.
# The only hard requirement: Go 1.24+ (Node/npm needed for frontend changes).

GO      ?= go
BINARY  := floweysales_backend
MCP     := floweysales_mcp

.DEFAULT_GOAL := help

.PHONY: help
help: ## List available targets
	@echo "Available targets:"
	@grep -E '^[a-zA-Z_-]+:.*## ' $(MAKEFILE_LIST) | awk -F'## ' '{printf "  %-16s %s\n", $$1, $$2}'
	@echo ""
	@echo "  Frontend dev server: cd frontend && npm start   (HTTP :4200, proxies /api to :8080)"

.PHONY: deps
deps: ## Install frontend dependencies (npm) and Go modules
	cd frontend && npm install
	$(GO) mod download

.PHONY: frontend
frontend: ## Build the Angular app into www/browser
	cd frontend && npx ng build

.PHONY: backend
backend: ## Build the Go backend binary
	$(GO) build -o $(BINARY) .

.PHONY: mcp
mcp: ## Build the MCP server binary (floweysales_mcp)
	$(GO) build -o $(MCP) ./cmd/mcp

.PHONY: build
build: frontend backend mcp ## Build everything (Angular -> www/, Go backend, MCP server)

.PHONY: test
test: ## Run Go unit tests
	$(GO) test ./...

.PHONY: run
run: build ## Build everything, then launch the backend on :8080
	./$(BINARY)

.PHONY: run-mcp
run-mcp: mcp ## Build and launch the MCP server (stdio) against the local backend
	./$(MCP) -backend http://localhost:8080

.PHONY: dev
dev: ## Recompile & run backend without rebuilding Angular
	$(GO) build -o $(BINARY) .
	@if [ ! -f www/browser/index.html ]; then \
		echo "NOTE: www/browser is empty — run 'make frontend' if you want the web UI."; \
	fi
	./$(BINARY)

.PHONY: clean
clean: ## Remove binary and Angular build output
	rm -f $(BINARY) $(MCP)
	rm -rf www/browser www/3rdpartylicenses.txt
