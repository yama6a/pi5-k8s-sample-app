MAIN_FILE=cmd/app/main.go
BINARY=cluster-sampleapp

.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

.PHONY: generate
generate: ## Generate the HTTP server from the OpenAPI spec.
	go generate ./...

.PHONY: build
build: ## Build the binary.
	go build -o $(BINARY) $(MAIN_FILE)

.PHONY: run
run: ## Run the app locally (expects DATABASE_URL or PG_PASSWORD).
	go run $(MAIN_FILE)

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
