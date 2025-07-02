#!/bin/bash

# Initialize development environment for the quant finance pipeline

set -e  # Exit on error

echo "Initializing development environment..."

# Check for required tools
echo "Checking for required tools..."

command -v go >/dev/null 2>&1 || { echo "Go is not installed. Please install Go first."; exit 1; }
command -v docker >/dev/null 2>&1 || { echo "Docker is not installed. Please install Docker first."; exit 1; }
command -v docker-compose >/dev/null 2>&1 || { echo "Docker Compose is not installed. Please install Docker Compose first."; exit 1; }
command -v protoc >/dev/null 2>&1 || { echo "Warning: Protocol Buffers compiler is not installed. You may need it for development."; }

# Create necessary directories
echo "Creating directories..."
mkdir -p bin
mkdir -p logs

# Install Go dependencies
echo "Installing Go dependencies..."
go mod tidy
go mod download

# Generate code from Protocol Buffer definitions
if command -v protoc >/dev/null 2>&1; then
    echo "Generating code from Protocol Buffers..."
    protoc --go_out=. --go_opt=paths=source_relative proto/market.proto
    protoc --go_out=. --go_opt=paths=source_relative proto/order.proto
    protoc --go_out=. --go_opt=paths=source_relative proto/risk.proto
else
    echo "Skipping Protocol Buffer code generation..."
fi

# Check for .env file and create if it doesn't exist
if [ ! -f .env ]; then
    echo "Creating default .env file..."
    cp .env.example .env || echo "Warning: .env.example not found. Please create a .env file manually."
fi

echo "Setting up test data..."
mkdir -p testdata
# Add any test data setup here

echo "Setup complete! You can now run 'make build' to build the services or 'make docker-run' to start them in Docker."
