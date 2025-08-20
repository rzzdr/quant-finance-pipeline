.PHONY: build clean docker-build docker-run test proto help

# Binary output names
API_BIN=bin/api
PROCESSOR_BIN=bin/processor
RISK_ENGINE_BIN=bin/risk-engine

# Build all binaries
build:
	@echo "Building API service..."
	go build -o $(API_BIN) ./cmd/api
	@echo "Building Processor service..."
	go build -o $(PROCESSOR_BIN) ./cmd/processor
	@echo "Building Risk Engine service..."
	go build -o $(RISK_ENGINE_BIN) ./cmd/risk-engine
	@echo "All binaries built successfully!"

# Clean built binaries
clean:
	@echo "Cleaning build artifacts..."
	rm -rf bin/
	@echo "Clean complete!"

# Run tests
test:
	@echo "Running tests..."
	go test ./...
	@echo "Tests complete!"

# Generate code from protobuf files
proto:
	@echo "Generating code from Protocol Buffers..."
	protoc --go_out=. --go_opt=paths=source_relative proto/market.proto
	protoc --go_out=. --go_opt=paths=source_relative proto/order.proto
	protoc --go_out=. --go_opt=paths=source_relative proto/risk.proto
	@echo "Proto code generation complete!"

# Build Docker images
docker-build:
	@echo "Building Docker images..."
	docker build --build-arg SERVICE_NAME=api -t quant-finance/api .
	docker build --build-arg SERVICE_NAME=processor -t quant-finance/processor .
	docker build --build-arg SERVICE_NAME=risk-engine -t quant-finance/risk-engine .
	@echo "Docker images built successfully!"

# Run services using Docker Compose
docker-run:
	@echo "Starting services with Docker Compose..."
	docker-compose up -d
	@echo "Services started! API is accessible at http://localhost:8080"

# Stop Docker Compose services
docker-stop:
	@echo "Stopping services..."
	docker-compose down
	@echo "Services stopped!"

# Format code
fmt:
	@echo "Formatting code..."
	go fmt ./...
	@echo "Formatting complete!"

# Run linter
lint:
	@echo "Running linter..."
	golangci-lint run
	@echo "Linting complete!"

# Show help
help:
	@echo "Available commands:"
	@echo "  make build        - Build all services"
	@echo "  make clean        - Remove build artifacts"
	@echo "  make test         - Run tests"
	@echo "  make proto        - Generate code from protocol buffers"
	@echo "  make docker-build - Build Docker images"
	@echo "  make docker-run   - Start services with Docker Compose"
	@echo "  make docker-stop  - Stop Docker Compose services"
	@echo "  make fmt          - Format code"
	@echo "  make lint         - Run linter"
	@echo "  make help         - Show this help message"
