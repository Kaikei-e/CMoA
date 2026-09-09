GO ?= go
GOLANGCI ?= golangci-lint

.PHONY: build test lint vet docdag e2e clean up down compose-build

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

# CMOA_CONFIG must be a host cmoa.json whose vault, runs_dir and fleet
# endpoints are reachable on this machine. See compose.yaml.
up:
	sh deploy/up.sh

down:
	docker compose down

compose-build:
	docker compose build
