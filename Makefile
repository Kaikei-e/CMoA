GO ?= go
GOLANGCI ?= golangci-lint

.PHONY: build test lint vet docdag e2e clean

build:
	$(GO) build -o bin/cmoa ./cmd/cmoa

test:
	$(GO) test ./...

vet:
	$(GO) vet ./...

lint: vet
	$(GOLANGCI) run ./...

docdag:
	docdag validate
	docdag lint

e2e: build
	CMOA_E2E=1 $(GO) test -run TestE2E -v ./...

clean:
	rm -rf bin
