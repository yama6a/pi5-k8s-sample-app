.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

.PHONY: generate
generate: ## Generate the HTTP server from the OpenAPI spec.
	go generate ./...

.PHONY: build
build: ## Build all three binaries (manager + signup + auditor) into bin/.
	go build -o bin/manager ./cmd/manager
	go build -o bin/signup ./cmd/signup
	go build -o bin/auditor ./cmd/auditor

.PHONY: run
run: ## Run the manager locally (expects PG_* + RABBITMQ_* + WORKLOAD_NAME).
	go run ./cmd/manager

.PHONY: run-signup
run-signup: ## Run the signup service locally (expects RABBITMQ_* + WORKLOAD_NAME).
	go run ./cmd/signup

.PHONY: run-auditor
run-auditor: ## Run the auditor service locally (expects RABBITMQ_* + WORKLOAD_NAME).
	go run ./cmd/auditor

.PHONY: test
test: ## Run tests (starts a Postgres container via Docker).
	go test ./... -count=1

.PHONY: vet
vet: ## Run go vet.
	go vet ./...

.PHONY: lint
lint: ## Run golangci-lint.
	golangci-lint run ./... -c .golangci.yaml

.PHONY: tidy
tidy: ## Tidy go modules.
	go mod tidy

.PHONY: ci
ci: generate vet lint test ## Run all CI checks.
