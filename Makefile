# Claude Mnemonic Makefile

VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
# Pass version to both main package and hooks package
LDFLAGS := -ldflags "-X main.Version=$(VERSION) -X github.com/lukaszraczylo/claude-mnemonic/pkg/hooks.Version=$(VERSION) -s -w" -buildvcs=false
BUILD_DIR := bin
PLUGIN_DIR := plugin

# Go settings
GOOS ?= $(shell go env GOOS)
GOARCH ?= $(shell go env GOARCH)

# CGO settings for SQLite FTS5 support
export CGO_ENABLED=1
BUILD_TAGS := -tags "fts5"

.PHONY: all build clean test install lint hooks worker mcp stop-worker start-worker restart-worker dashboard website dev-website

all: build

# Build all binaries
build: dashboard worker hooks mcp

# Build Vue dashboard
dashboard:
	@echo "Building Vue dashboard..."
	@cd ui && npm install --silent && npm run build
	@rm -rf internal/worker/static
	@mkdir -p internal/worker/static
	@touch internal/worker/static/placeholder.html
	@cp -r ui/dist/* internal/worker/static/

# Build worker service
worker:
	@echo "Building worker..."
	@mkdir -p $(BUILD_DIR)
	go build $(BUILD_TAGS) $(LDFLAGS) -o $(BUILD_DIR)/worker ./cmd/worker

# Build all hooks
hooks:
	@echo "Building hooks..."
	@mkdir -p $(BUILD_DIR)/hooks
	go build $(BUILD_TAGS) $(LDFLAGS) -o $(BUILD_DIR)/hooks/session-start ./cmd/hooks/session-start
	go build $(BUILD_TAGS) $(LDFLAGS) -o $(BUILD_DIR)/hooks/user-prompt ./cmd/hooks/user-prompt
	go build $(BUILD_TAGS) $(LDFLAGS) -o $(BUILD_DIR)/hooks/post-tool-use ./cmd/hooks/post-tool-use
	go build $(BUILD_TAGS) $(LDFLAGS) -o $(BUILD_DIR)/hooks/subagent-stop ./cmd/hooks/subagent-stop
	go build $(BUILD_TAGS) $(LDFLAGS) -o $(BUILD_DIR)/hooks/stop ./cmd/hooks/stop

# Build MCP server
mcp:
	@echo "Building MCP server..."
	@mkdir -p $(BUILD_DIR)
	go build $(BUILD_TAGS) $(LDFLAGS) -o $(BUILD_DIR)/mcp-server ./cmd/mcp

# Build for all platforms
build-all: build-linux build-darwin build-windows

build-linux:
	@echo "Building for Linux..."
	@mkdir -p $(BUILD_DIR)/linux-amd64/hooks
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o $(BUILD_DIR)/linux-amd64/worker ./cmd/worker
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o $(BUILD_DIR)/linux-amd64/mcp-server ./cmd/mcp
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o $(BUILD_DIR)/linux-amd64/hooks/session-start ./cmd/hooks/session-start
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o $(BUILD_DIR)/linux-amd64/hooks/user-prompt ./cmd/hooks/user-prompt
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o $(BUILD_DIR)/linux-amd64/hooks/post-tool-use ./cmd/hooks/post-tool-use
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o $(BUILD_DIR)/linux-amd64/hooks/subagent-stop ./cmd/hooks/subagent-stop
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o $(BUILD_DIR)/linux-amd64/hooks/stop ./cmd/hooks/stop

build-darwin:
	@echo "Building for macOS..."
	@mkdir -p $(BUILD_DIR)/darwin-amd64/hooks $(BUILD_DIR)/darwin-arm64/hooks
	GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o $(BUILD_DIR)/darwin-amd64/worker ./cmd/worker
	GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o $(BUILD_DIR)/darwin-arm64/worker ./cmd/worker
	GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o $(BUILD_DIR)/darwin-amd64/mcp-server ./cmd/mcp
	GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o $(BUILD_DIR)/darwin-arm64/mcp-server ./cmd/mcp
	GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o $(BUILD_DIR)/darwin-amd64/hooks/session-start ./cmd/hooks/session-start
	GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o $(BUILD_DIR)/darwin-arm64/hooks/session-start ./cmd/hooks/session-start
	GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o $(BUILD_DIR)/darwin-amd64/hooks/user-prompt ./cmd/hooks/user-prompt
	GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o $(BUILD_DIR)/darwin-arm64/hooks/user-prompt ./cmd/hooks/user-prompt
	GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o $(BUILD_DIR)/darwin-amd64/hooks/post-tool-use ./cmd/hooks/post-tool-use
	GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o $(BUILD_DIR)/darwin-arm64/hooks/post-tool-use ./cmd/hooks/post-tool-use
	GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o $(BUILD_DIR)/darwin-amd64/hooks/subagent-stop ./cmd/hooks/subagent-stop
	GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o $(BUILD_DIR)/darwin-arm64/hooks/subagent-stop ./cmd/hooks/subagent-stop
	GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o $(BUILD_DIR)/darwin-amd64/hooks/stop ./cmd/hooks/stop
	GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o $(BUILD_DIR)/darwin-arm64/hooks/stop ./cmd/hooks/stop

build-windows:
	@echo "Building for Windows..."
	@mkdir -p $(BUILD_DIR)/windows-amd64/hooks
	GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o $(BUILD_DIR)/windows-amd64/worker.exe ./cmd/worker
	GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o $(BUILD_DIR)/windows-amd64/mcp-server.exe ./cmd/mcp
	GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o $(BUILD_DIR)/windows-amd64/hooks/session-start.exe ./cmd/hooks/session-start
	GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o $(BUILD_DIR)/windows-amd64/hooks/user-prompt.exe ./cmd/hooks/user-prompt
	GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o $(BUILD_DIR)/windows-amd64/hooks/post-tool-use.exe ./cmd/hooks/post-tool-use
	GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o $(BUILD_DIR)/windows-amd64/hooks/subagent-stop.exe ./cmd/hooks/subagent-stop
	GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o $(BUILD_DIR)/windows-amd64/hooks/stop.exe ./cmd/hooks/stop

# Stop any running worker
stop-worker:
	@echo "Stopping worker..."
	@-pkill -9 -f 'claude-mnemonic.*worker' 2>/dev/null || true
	@-pkill -9 -f '\.claude/plugins/.*/worker' 2>/dev/null || true
	@-lsof -ti :37777 | xargs kill -9 2>/dev/null || true
	@sleep 1

# Start worker in background
start-worker:
	@echo "Starting worker..."
	@# Prefer cache directory (where Claude Code looks), fall back to marketplaces
	@if [ -f "$(HOME)/.claude/plugins/cache/claude-mnemonic/claude-mnemonic/1.0.0/worker" ]; then \
		nohup $(HOME)/.claude/plugins/cache/claude-mnemonic/claude-mnemonic/1.0.0/worker > /tmp/claude-mnemonic-worker.log 2>&1 & \
	else \
		nohup $(HOME)/.claude/plugins/marketplaces/claude-mnemonic/worker > /tmp/claude-mnemonic-worker.log 2>&1 & \
	fi
	@sleep 1
	@if curl -s http://localhost:37777/health > /dev/null 2>&1; then \
		echo "Worker started successfully (http://localhost:37777)"; \
	else \
		echo "Warning: Worker may not have started. Check /tmp/claude-mnemonic-worker.log"; \
	fi

# Restart worker
restart-worker: stop-worker start-worker

# Install to Claude plugins directory
install: build stop-worker
	@echo "Installing to Claude plugins directory..."
	@# Install to marketplaces directory (for direct installs)
	@mkdir -p $(HOME)/.claude/plugins/marketplaces/claude-mnemonic/hooks
	@mkdir -p $(HOME)/.claude/plugins/marketplaces/claude-mnemonic/.claude-plugin
	cp $(BUILD_DIR)/worker $(HOME)/.claude/plugins/marketplaces/claude-mnemonic/
	cp $(BUILD_DIR)/mcp-server $(HOME)/.claude/plugins/marketplaces/claude-mnemonic/
	cp $(BUILD_DIR)/hooks/* $(HOME)/.claude/plugins/marketplaces/claude-mnemonic/hooks/
	cp $(PLUGIN_DIR)/hooks/hooks.json $(HOME)/.claude/plugins/marketplaces/claude-mnemonic/hooks/
	cp $(PLUGIN_DIR)/.claude-plugin/plugin.json $(HOME)/.claude/plugins/marketplaces/claude-mnemonic/.claude-plugin/
	cp $(PLUGIN_DIR)/.claude-plugin/marketplace.json $(HOME)/.claude/plugins/marketplaces/claude-mnemonic/.claude-plugin/
	@# Also install to cache directory (where Claude Code looks for plugins)
	@if [ -d "$(HOME)/.claude/plugins/cache/claude-mnemonic" ]; then \
		echo "Updating plugin in cache directory..."; \
		CACHE_DIR=$$(find $(HOME)/.claude/plugins/cache/claude-mnemonic -type d -name "hooks" -exec dirname {} \; 2>/dev/null | head -1); \
		if [ -n "$$CACHE_DIR" ]; then \
			cp $(BUILD_DIR)/worker "$$CACHE_DIR/"; \
			cp $(BUILD_DIR)/mcp-server "$$CACHE_DIR/"; \
			cp $(BUILD_DIR)/hooks/* "$$CACHE_DIR/hooks/"; \
			cp $(PLUGIN_DIR)/hooks/hooks.json "$$CACHE_DIR/hooks/"; \
			echo "Cache directory updated: $$CACHE_DIR"; \
		fi; \
	fi
	@echo "Registering plugin with Claude Code..."
	@./scripts/register-plugin.sh
	@$(MAKE) start-worker
	@echo "Installation complete!"

# Uninstall
uninstall:
	@echo "Uninstalling..."
	@./scripts/unregister-plugin.sh
	rm -rf $(HOME)/.claude/plugins/marketplaces/claude-mnemonic
	@echo "Uninstallation complete!"

# Run tests
test:
	go test -v -race ./...

# Run tests with coverage
test-coverage:
	go test -v -race -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

# Run benchmarks
bench:
	go test -bench=. -benchmem ./...

# Run linter
lint:
	golangci-lint run ./...

# Format code
fmt:
	go fmt ./...

# Clean build artifacts
clean:
	rm -rf $(BUILD_DIR)
	rm -f coverage.out coverage.html

# Run worker in development mode
dev: worker
	./$(BUILD_DIR)/worker

# Download dependencies
deps:
	go mod download
	go mod tidy

# Build website for production
website:
	@echo "Building website..."
	@cd docs && npm install --silent && npm run build
	@echo "Website built to docs/dist/"

# Run website in development mode
dev-website:
	@echo "Starting website dev server..."
	@cd docs && npm install --silent && npm run dev

# Show help
help:
	@echo "Claude Mnemonic Build System"
	@echo ""
	@echo "Usage:"
	@echo "  make build          - Build all binaries"
	@echo "  make worker         - Build worker service only"
	@echo "  make mcp            - Build MCP server only"
	@echo "  make hooks          - Build hooks only"
	@echo "  make build-all      - Build for all platforms"
	@echo "  make install        - Install to Claude plugins directory (restarts worker)"
	@echo "  make uninstall      - Remove from Claude plugins directory"
	@echo "  make stop-worker    - Stop the running worker"
	@echo "  make start-worker   - Start the worker in background"
	@echo "  make restart-worker - Restart the worker"
	@echo "  make test           - Run tests"
	@echo "  make bench          - Run benchmarks"
	@echo "  make lint           - Run linter"
	@echo "  make fmt            - Format code"
	@echo "  make clean          - Clean build artifacts"
	@echo "  make dev            - Run worker in development mode"
	@echo "  make deps           - Download dependencies"
	@echo "  make website        - Build website for production"
	@echo "  make dev-website    - Run website dev server"
	@echo ""
	@echo "Variables:"
	@echo "  VERSION=$(VERSION)"
	@echo "  GOOS=$(GOOS)"
	@echo "  GOARCH=$(GOARCH)"
