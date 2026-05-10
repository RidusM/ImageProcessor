PROJECT_NAME := img-processor
MAIN_PACKAGE := ./cmd/img-processor
BINARY_NAME := $(PROJECT_NAME)
BINARY_PATH := ./bin/$(BINARY_NAME)

BASE_STACK := docker compose -f docker-compose.yml
LOCAL_ENV_FILE := .env

.DEFAULT_GOAL := help

.PHONY: help
help: ## Display this help screen
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_-]+:.*?##/ { printf "  \033[36m%-25s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n%[1]s\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

.PHONY: deps
deps: ## Tidy and verify Go modules
	go mod tidy && go mod verify

.PHONY: run
run: deps swagger ## Run the application locally (requires Kafka/MinIO)
	@echo "Running application..."
	go run $(MAIN_PACKAGE)

.PHONY: build
build: deps ## Build binary for linux/amd64
	@echo "Building $(BINARY_NAME)..."
	@mkdir -p $(BINARY_PATH)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o $(BINARY_PATH)/$(BINARY_NAME) $(MAIN_PACKAGE)
	@echo "Binary built: $(BINARY_PATH)/$(BINARY_NAME)"

.PHONY: build-local
build-local: deps ## Build binary for local OS
	@echo "Building for local OS..."
	go build -o $(BINARY_NAME) $(MAIN_PACKAGE)
	@echo "Binary built: $(BINARY_NAME)"

.PHONY: build-docker
build-docker: ## Build Docker image
	@echo "Building Docker image..."
	docker build -t $(PROJECT_NAME):latest .
	@echo "Image built: $(PROJECT_NAME):latest"

.PHONY: infra-up
infra-up: ## Start infrastructure (Kafka + MinIO)
	@echo "Starting Infrastructure..."
	$(BASE_STACK) up -d
	@echo "Infrastructure started."

.PHONY: infra-down
infra-down: ## Stop infrastructure
	@echo "Stopping infrastructure..."
	$(BASE_STACK) down
	@echo "Infrastructure stopped"

.PHONY: infra-logs
infra-logs: ## Show logs for all infrastructure
	@$(BASE_STACK) logs -f

.PHONY: compose-up
compose-up: ## Run all services (App + Infra)
	@echo "Starting all services..."
	$(BASE_STACK) up --build -d app
	@echo "All services started"
	@echo "Logs:"
	$(BASE_STACK) logs -f --tail=50

.PHONY: compose-down
compose-down: ## Stop and remove all containers and volumes
	@echo "Stopping and cleaning..."
	$(BASE_STACK) down --remove-orphans --volumes
	@echo "Cleanup completed"

.PHONY: test
test: ## Run unit tests with race detector and coverage
	@echo "Running unit tests..."
	go test -race -covermode=atomic -coverprofile=coverage.txt ./internal/... -v
	@echo "Test results written to coverage.txt"
	go tool cover -func=coverage.txt | tail -1

.PHONY: format
format: ## Format code (gofmt, imports, line length)
	@echo "Formatting..."
	gofumpt -l -w .
	gci write .
	goimports -w .
	golines -w --max-len=120 .
	@echo "Code formatted"

.PHONY: lint
lint: ## Run linter
	@echo "Running linter..."
	golangci-lint run ./...
	@echo "Lint passed"

.PHONY: swagger
swagger: ## Generate Swagger documentation
	@echo "Generating Swagger docs..."
	swag init -g cmd/img-processor/main.go --output docs
	@echo "Swagger docs generated in docs/"

.PHONY: pre-commit
pre-commit: format lint swagger ## Pre-commit hook
	@echo "Pre-commit checks passed!"

.PHONY: clean
clean: ## Clean build artifacts and docs
	@echo "Cleaning..."
	rm -rf $(BINARY_PATH) ./docs/* ./coverage.txt
	@echo "Cleanup done"