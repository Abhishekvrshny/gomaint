FROM golang:1.23-alpine AS builder

# Install protobuf compiler and plugins
RUN apk add --no-cache protobuf-dev git

# Install Go protobuf plugins
RUN go install google.golang.org/protobuf/cmd/protoc-gen-go@latest && \
    go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest

# Set working directory
WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Generate protobuf code
WORKDIR /app/examples/grpc-service/proto
RUN go generate .

# Build the application
WORKDIR /app
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o grpc-service ./examples/grpc-service/main.go

# Build health check utility
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o grpc-healthcheck ./examples/grpc-service/cmd/healthcheck/main.go

# Final stage
FROM alpine:latest

# Install ca-certificates for SSL/TLS
RUN apk --no-cache add ca-certificates

# Create non-root user
RUN addgroup -g 1000 appgroup && \
    adduser -u 1000 -G appgroup -s /bin/sh -D appuser

# Set working directory
WORKDIR /app/

# Copy binary from builder
COPY --from=builder /app/grpc-service .
COPY --from=builder /app/grpc-healthcheck .

# Change ownership to non-root user
RUN chown -R appuser:appgroup /app

# Switch to non-root user
USER appuser

# Expose gRPC port
EXPOSE 50051

# Health check
HEALTHCHECK --interval=10s --timeout=5s --start-period=10s --retries=3 \
    CMD ["/app/grpc-healthcheck"]

# Run the binary
CMD ["./grpc-service"]