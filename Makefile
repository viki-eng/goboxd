.PHONY: build run test integration lint clean

COMPOSE ?= docker compose
TOOLS   := $(COMPOSE) --profile tools run --rm tools

# Build the Docker image
build:
	$(COMPOSE) build goboxd

# Run the service locally
run:
	$(COMPOSE) up goboxd

# Run unit tests
test:
	$(TOOLS) go test -v ./...

# Run integration tests (requires running service)
integration:
	$(TOOLS) go test -tags=integration -v ./tests/...

# Run linter
lint:
	$(TOOLS) golangci-lint run ./...

# Clean up containers and volumes
clean:
	$(COMPOSE) down
	$(COMPOSE) down -v

# Format code
fmt:
	$(TOOLS) go fmt ./...
