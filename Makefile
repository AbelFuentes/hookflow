.PHONY: test lint run
test:
	go test -race -cover ./...
lint:
	golangci-lint run
run:
	go run ./cmd/hookflow