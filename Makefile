# StallionUSSY Makefile
# Premium Equine Genetics Exchange Build System
# "The yogurt is patient. The yogurt remembers."

BINARY_NAME=stallionussy
BUILD_DIR=./cmd/stallionussy
PORT?=8080

.PHONY: all build run serve cli test vet clean docker docker-run fmt smoke

# Default: build the binary
all: build

# Build the single binary
build:
	@echo "🐴 Compiling stallion..."
	go build -ldflags="-w -s" -o $(BINARY_NAME) $(BUILD_DIR)
	@echo "✅ Built: ./$(BINARY_NAME)"

# Run the web server
serve: build
	@echo "🐴 Starting StallionUSSY on port $(PORT)..."
	./$(BINARY_NAME) serve --port $(PORT)

# Run interactive CLI mode
cli: build
	@echo "🐴 Entering the stable..."
	./$(BINARY_NAME) cli

# Run in dev mode (no build cache)
dev:
	go run $(BUILD_DIR)/main.go serve --port $(PORT)

# Run tests
test:
	go test -v -race ./...

# Run vet
vet:
	go vet ./...

# Format all Go files
fmt:
	gofmt -w -s .

# Clean build artifacts
clean:
	rm -f $(BINARY_NAME)
	go clean -cache

# Build Docker image
docker:
	docker build -t stallionussy:latest .

# Run via Docker
docker-run: docker
	docker run -p $(PORT):8080 --name stallionussy --rm stallionussy:latest

# Docker Compose up
up:
	docker-compose up --build

# Smoke test the local compose stack
smoke:
	curl -fsS http://localhost:$(PORT)/api/capabilities | grep 'starter_recovery'

# Docker Compose down
down:
	docker-compose down

# Quick demo: build, seed, and race
demo: build
	@echo "🐴 Quick demo mode..."
	@echo -e "seed\nquick-race\nquick-race\nquick-race\nleaderboard\nexit" | ./$(BINARY_NAME) cli

# Show project structure
tree:
	@echo "StallionUSSY Project Structure:"
	@echo "================================"
	@find . -type f -name "*.go" -o -name "*.html" -o -name "Makefile" -o -name "Dockerfile" -o -name "*.yml" -o -name "*.mod" -o -name "*.sum" | sort | head -30

# Help
help:
	@echo "StallionUSSY Build Commands:"
	@echo "  make build      - Compile the binary"
	@echo "  make serve      - Start the web server (PORT=8080)"
	@echo "  make cli        - Interactive CLI mode"
	@echo "  make dev        - Dev mode (go run)"
	@echo "  make test       - Run tests"
	@echo "  make vet        - Run go vet"
	@echo "  make fmt        - Format code"
	@echo "  make docker     - Build Docker image"
	@echo "  make docker-run - Run via Docker"
	@echo "  make up         - Docker Compose up"
	@echo "  make demo       - Quick CLI demo"
	@echo "  make clean      - Clean artifacts"
