PROJECT := github.com/laputalabs/mailbear
CMD     := $(PROJECT)/cmd/mailbear
BIN     := bin/mailbear
IMAGE   := mailbear
# A passed-but-empty VERSION= (e.g. an unset CI tag flowing through Docker --build-arg)
# would otherwise embed an empty main.version. override + $(or) coerces empty/unset to DEV
# so a build is never shipped unversioned.
override VERSION := $(or $(strip $(VERSION)),DEV)

GO      := go
LDFLAGS := -s -w -X main.version=$(VERSION)
GOBUILD := CGO_ENABLED=0 $(GO) build -trimpath -ldflags "$(LDFLAGS)"

# need <tool> — abort with an install hint when a required tool is missing.
need = command -v $(1) >/dev/null 2>&1 || { echo "$(1) not found — run 'make setup'"; exit 1; }

.DEFAULT_GOAL := dev
.PHONY: dev prod clean format lint lint-fix test tidy vuln docker setup help

dev: ## Default: fast local build
	@$(call need,$(GO))
	@echo "Building $(BIN)..."
	@mkdir -p bin
	@$(GOBUILD) -o $(BIN) $(CMD)

prod: ## Production binary: stripped, trimmed, statically linked
	@$(call need,$(GO))
	@echo "Building $(BIN) (VERSION=$(VERSION))..."
	@mkdir -p bin
	@$(GOBUILD) -o $(BIN) $(CMD)

clean: ## Remove build artifacts
	@echo "Cleaning..."
	@rm -rf bin

format: ## Format code (gofumpt)
	@$(call need,gofumpt)
	@echo "Formatting..."
	@gofumpt -w .

lint: ## Run linter (golangci-lint)
	@$(call need,golangci-lint)
	@echo "Linting..."
	@golangci-lint run ./...

lint-fix: ## Run linter with autofix
	@$(call need,golangci-lint)
	@echo "Linting with autofix..."
	@golangci-lint run --fix ./...

test: ## Run tests with the race detector
	@echo "Testing..."
	@$(GO) test -race ./...

tidy: ## Tidy go.mod / go.sum
	@$(GO) mod tidy

vuln: ## Scan dependencies for known vulnerabilities
	@$(call need,govulncheck)
	@echo "Scanning for vulnerabilities..."
	@govulncheck ./...

docker: ## Build the Docker image (VERSION=<tag> to stamp)
	@echo "Building Docker image $(IMAGE):$(VERSION)..."
	@docker build --build-arg VERSION=$(VERSION) -t $(IMAGE):$(VERSION) .

setup: ## Install dev tools (gofumpt, golangci-lint, govulncheck)
	@echo "Installing dev tools..."
	@$(GO) install mvdan.cc/gofumpt@latest
	@$(GO) install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest
	@$(GO) install golang.org/x/vuln/cmd/govulncheck@latest

help: ## Show this help
	@grep -hE '^[a-zA-Z_-]+:.*## ' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  \033[36m%-10s\033[0m %s\n", $$1, $$2}'
