# High-Performance Quantitative Finance Data Pipeline

## Project Overview

This project is a high-performance, low-latency quantitative finance data pipeline built in Go. It features:

- Publisher-subscriber model using Kafka
- Multiple service endpoints
- Risk model for derivative hedging
- Limit order book integration
- Real-time expected shortfall calculations
- Efficient concurrency patterns in Go

## System Architecture

The system consists of several key components:

1. **API Service** - External interface for data access and management
2. **Processor Service** - Processes market data and updates order books
3. **Risk Engine** - Calculates risk metrics and hedging strategies
4. **Kafka Cluster** - Message broker for high-throughput data transfer
5. **PostgreSQL** - Persistent storage for historical data
6. **Prometheus/Grafana** - Monitoring and visualization

## Getting Started

### Prerequisites

- Go 1.24 or later
- Docker and Docker Compose
- Make (optional, for using Makefile commands)

### Setup

1. Clone the repository:
   ```bash
   git clone https://github.com/rzzdr/quant-finance-pipeline.git
   cd quant-finance-pipeline
   ```

2. Set up environment variables (or use the provided `.env` file):
   ```bash
   cp .env.example .env
   # Edit .env file with your configuration
   ```

3. Start the services using Docker Compose:
   ```bash
   docker-compose up -d
   ```

   This will start all the required services, including:
   - API service on port 8080
   - Kafka cluster (3 brokers)
   - PostgreSQL database
   - Prometheus on port 9090
   - Grafana on port 3000

### Building Locally

If you prefer to build and run the services locally:

1. Build all services:
   ```bash
   go build -o ./bin/api ./cmd/api
   go build -o ./bin/processor ./cmd/processor
   go build -o ./bin/risk-engine ./cmd/risk-engine
   ```

2. Run a service (example for API service):
   ```bash
   ./bin/api --config ./config/config.yaml
   ```

## Project Structure

```
├── cmd/                  # Service entry points
│   ├── api/              # API service
│   ├── processor/        # Market data processor
│   └── risk-engine/      # Risk calculation engine
├── config/               # Configuration files
├── docs/                 # Documentation
├── internal/             # Internal packages
│   ├── kafka/            # Kafka client
│   ├── market/           # Market data processing
│   ├── orderbook/        # Limit order book
│   ├── risk/             # Risk calculations
│   └── store/            # Data storage
├── pkg/                  # Public packages
│   ├── api/              # API server
│   ├── metrics/          # Metrics recording
│   ├── models/           # Data models
│   └── utils/            # Utilities
├── proto/                # Protocol buffer definitions
├── Dockerfile            # Docker build file
├── docker-compose.yml    # Docker Compose file
├── .env                  # Environment variables
└── README.md             # This file
```

## API Endpoints

The API service provides the following endpoints:

- `GET /api/v1/health` - Health check
- `GET /api/v1/market/data/{symbol}` - Get market data for a symbol
- `GET /api/v1/orderbook/{symbol}` - Get order book for a symbol
- `POST /api/v1/orders` - Submit a new order
- `GET /api/v1/orders/{id}` - Get order details
- `GET /api/v1/risk/metrics/{portfolioId}` - Get risk metrics for a portfolio

Detailed API documentation is available in `/docs/api.yaml` (OpenAPI/Swagger format).

## Development

### Testing

Run the tests with:

```bash
go test ./...
```

For specific package tests:

```bash
go test ./internal/orderbook/...
```

### Monitoring

- Prometheus: http://localhost:9090
- Grafana: http://localhost:3000 (default login: admin/admin)

Pre-configured dashboards are available for:
- System metrics
- Order book statistics
- Risk calculations
- Kafka throughput

## Contributing

1. Fork the repository
2. Create your feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add some amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

## License

This project is licensed under the MIT License - see the LICENSE file for details.
