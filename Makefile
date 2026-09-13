GO_LINT_CONFIG     ?= .build/golangci.yaml
CANONICAL_LINT_URL := https://raw.githubusercontent.com/yama6a/gha/v2/.golangci.yaml
IMAGE              ?= ghcr.io/yama6a/pi5-k8s-sample-app

.PHONY: lint-config generate fmt fmt-check lint vet test cover vuln tidy tidy-check \
	generate-check mod image ci build run run-signup run-auditor

lint-config:
	mkdir -p .build
	curl -fsSL $(CANONICAL_LINT_URL) -o .build/canonical-golangci.yaml
	if [ -f .golangci.local.yaml ]; then \
		yq eval-all '. as $$item ireduce ({}; . *+ $$item)' \
			.build/canonical-golangci.yaml .golangci.local.yaml > $(GO_LINT_CONFIG); \
	else \
		cp .build/canonical-golangci.yaml $(GO_LINT_CONFIG); \
	fi

generate:
	go generate ./...

fmt: lint-config
	golangci-lint fmt -c $(GO_LINT_CONFIG)

fmt-check: lint-config
	golangci-lint fmt --diff -c $(GO_LINT_CONFIG)

lint: lint-config
	golangci-lint run ./... -c $(GO_LINT_CONFIG)

vet:
	go vet ./...

test:
	go test ./... -race -count=1

cover:
	go test ./... -coverprofile=cover.out -covermode=atomic
	go tool cover -func=cover.out | tail -1

vuln:
	go run golang.org/x/vuln/cmd/govulncheck@latest ./...

tidy:
	go mod tidy

tidy-check:
	go mod tidy
	git diff --exit-code -- go.mod go.sum

generate-check: generate
	git diff --exit-code

mod:
	go get -u -t ./...
	go mod tidy

image:
	docker buildx build -f .build/Dockerfile -t $(IMAGE) --load .

ci: tidy-check generate-check fmt-check lint vet test vuln

build:
	go build -o bin/manager ./cmd/manager
	go build -o bin/signup ./cmd/signup
	go build -o bin/auditor ./cmd/auditor

run:
	go run ./cmd/manager

run-signup:
	go run ./cmd/signup

run-auditor:
	go run ./cmd/auditor
