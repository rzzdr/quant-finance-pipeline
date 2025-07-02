FROM golang:1.24.2-alpine AS builder

WORKDIR /app

# Install build dependencies
RUN apk add --no-cache gcc musl-dev

# Copy go mod and sum files
COPY go.mod go.sum ./

# Download all dependencies
RUN go mod download

# Copy the source code
COPY . .

# Build the application
ARG SERVICE_NAME
RUN CGO_ENABLED=1 GOOS=linux go build -a -installsuffix cgo -o /go/bin/app ./cmd/${SERVICE_NAME}

# Create a minimal image for runtime
FROM alpine:3.19

RUN apk --no-cache add ca-certificates tzdata

WORKDIR /app

# Copy the binary from the builder stage
COPY --from=builder /go/bin/app /app/app

# Copy the config file
COPY config/config.yaml /app/config/

# Run the application
ENTRYPOINT ["/app/app"]
CMD ["--config", "/app/config/config.yaml"]
