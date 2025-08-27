# Use Go 1.23 with Debian (glibc) as base image for Confluent Kafka Go client
FROM golang:1.23-bullseye AS builder

# Set working directory
WORKDIR /app

# Install dependencies for CGO (required by Confluent Kafka Go client)
RUN apt-get update && apt-get install -y \
    git \
    ca-certificates \
    curl \
    gcc \
    libc6-dev \
    pkg-config \
    && rm -rf /var/lib/apt/lists/*

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the application with CGO enabled for Confluent Kafka
RUN CGO_ENABLED=1 GOOS=linux go build -a -o main ./examples/kafka-confluent-service/

# Use Debian slim for runtime (glibc compatible)
FROM debian:bullseye-slim

# Install runtime dependencies
RUN apt-get update && apt-get install -y \
    ca-certificates \
    curl \
    tzdata \
    && rm -rf /var/lib/apt/lists/* \
    && adduser --disabled-password --gecos '' appuser

# Set working directory
WORKDIR /root/

# Copy binary from builder
COPY --from=builder /app/main .

# Switch to non-root user
USER appuser
WORKDIR /home/appuser

# Copy binary to user directory
COPY --from=builder --chown=appuser:appuser /app/main .

# Expose port
EXPOSE 8085

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=45s --retries=3 \
    CMD curl -f http://localhost:8085/health || exit 1

# Run the application
CMD ["./main"]