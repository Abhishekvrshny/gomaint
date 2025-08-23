# Use Go 1.23 as base image
FROM golang:1.23-alpine AS builder

# Set working directory
WORKDIR /app

# Install dependencies
RUN apk add --no-cache git ca-certificates curl

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o main ./examples/kafka-service/

# Use minimal base image for runtime
FROM alpine:latest

# Install certificates and curl for health checks
RUN apk --no-cache add ca-certificates curl tzdata && \
    adduser -D -s /bin/sh appuser

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
EXPOSE 8080

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=45s --retries=3 \
    CMD curl -f http://localhost:8080/health || exit 1

# Run the application
CMD ["./main"]