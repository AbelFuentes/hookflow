ifneq (,$(wildcard .env))
include .env
export
endif

.PHONY: test lint run
test:
	go test -race -cover -timeout 30s ./...
lint:
	golangci-lint run
run:
	go run ./cmd/hookflow